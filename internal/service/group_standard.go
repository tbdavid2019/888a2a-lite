package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/tbdavid2019/888a2a-lite/internal/a2a"
	"github.com/tbdavid2019/888a2a-lite/internal/hub"
	"github.com/tbdavid2019/888a2a-lite/internal/store"
)

const (
	groupTenantPrefix             = "group:"
	groupReplyPolicyAll           = "ALL"
	groupReplyPolicyMentionedOnly = "MENTIONED_ONLY"
	groupReplyPolicyAckOnly       = "ACK_ONLY"
)

type standardGroupReference struct {
	GroupID     string `json:"groupId"`
	Name        string `json:"name"`
	MemberCount int    `json:"memberCount"`
	CardURL     string `json:"cardUrl"`
}

type standardGroupListResponse struct {
	Groups        []standardGroupReference `json:"groups"`
	NextPageToken string                   `json:"nextPageToken"`
}

type groupExtensionMetadata struct {
	ReplyPolicy string   `json:"replyPolicy"`
	Mentions    []string `json:"mentions,omitempty"`
}

func parseGroupTenant(tenant string) (string, bool) {
	if !strings.HasPrefix(tenant, groupTenantPrefix) {
		return "", false
	}
	groupID := strings.TrimSpace(strings.TrimPrefix(tenant, groupTenantPrefix))
	return groupID, groupID != "" && !strings.ContainsAny(groupID, "/?#")
}

func groupExtensionOptedIn(header string) bool {
	for _, value := range strings.Split(header, ",") {
		if strings.TrimSpace(value) == a2a.GroupExtensionURI {
			return true
		}
	}
	return false
}

func (service *Service) groupMemberForStandard(ctx context.Context, requester hub.RegisteredAgent, groupID string) (hub.Group, hub.GroupMember, error) {
	group, err := service.store.Groups().FindGroup(ctx, groupID)
	if err != nil || group.CircleID != requester.CircleID || !group.IsActive() {
		return hub.Group{}, hub.GroupMember{}, standardError(http.StatusNotFound, "GROUP_NOT_FOUND", "group not found")
	}
	member, err := service.store.Groups().FindMember(ctx, groupID, requester.AgentID)
	if err != nil || !member.IsActive() || member.CircleID != requester.CircleID {
		return hub.Group{}, hub.GroupMember{}, standardError(http.StatusNotFound, "GROUP_NOT_FOUND", "group not found")
	}
	return group, member, nil
}

func (service *Service) ListStandardGroups(ctx context.Context, requester hub.RegisteredAgent, baseURL string, pageSize, offset int) ([]standardGroupReference, string, error) {
	groups, err := service.store.Groups().ListGroups(ctx, requester.AgentID)
	if err != nil {
		return nil, "", err
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 50
	}
	if offset < 0 {
		offset = 0
	}
	references := make([]standardGroupReference, 0, len(groups))
	for _, group := range groups {
		if !group.IsActive() || group.CircleID != requester.CircleID {
			continue
		}
		members, memberErr := service.store.Groups().ListMembers(ctx, group.GroupID)
		if memberErr != nil {
			return nil, "", memberErr
		}
		references = append(references, standardGroupReference{GroupID: group.GroupID, Name: group.Name, MemberCount: len(members), CardURL: strings.TrimRight(baseURL, "/") + "/a2a/v1/groups/" + group.GroupID + "/card"})
	}
	if offset >= len(references) {
		return []standardGroupReference{}, "", nil
	}
	end := offset + pageSize
	if end > len(references) {
		end = len(references)
	}
	next := ""
	if end < len(references) {
		nextValue, _ := json.Marshal(struct {
			Offset   int    `json:"offset"`
			AgentID  string `json:"agentId"`
			CircleID string `json:"circleId"`
		}{end, requester.AgentID, requester.CircleID})
		next = base64.RawURLEncoding.EncodeToString(nextValue)
	}
	return references[offset:end], next, nil
}

func (service *Service) StandardGroupCard(ctx context.Context, requester hub.RegisteredAgent, groupID, baseURL string) (a2a.AgentCard, error) {
	group, _, err := service.groupMemberForStandard(ctx, requester, groupID)
	if err != nil {
		return a2a.AgentCard{}, err
	}
	card := service.standardCard(baseURL, group.Name, "A2A Group Coordination Extension for "+group.Name, groupTenantPrefix+groupID)
	for index := range card.Capabilities.Extensions {
		if card.Capabilities.Extensions[index].URI == a2a.GroupExtensionURI {
			params := card.Capabilities.Extensions[index].Params
			if params == nil {
				params = map[string]any{}
			}
			params["hasCharter"] = group.HasCharter
			params["charterVersion"] = group.CharterVersion
			if group.ContentHash != "" {
				params["contentHash"] = group.ContentHash
			}
			if group.CharterUpdatedAt != nil {
				params["updatedAt"] = group.CharterUpdatedAt.UTC().Format(time.RFC3339Nano)
			}
			card.Capabilities.Extensions[index].Params = params
		}
	}
	card.Skills = []a2a.AgentSkill{{ID: "group-coordination", Name: "Group coordination", Description: "Fan out text messages to eligible group members and aggregate their outcomes", Tags: []string{"group", "coordination", "fan-out"}, InputModes: []string{"text/plain"}, OutputModes: []string{"text/plain"}}}
	return card, nil
}

func (service *Service) parseGroupMetadata(request a2a.SendMessageRequest) (groupExtensionMetadata, error) {
	value := any(nil)
	if request.Metadata != nil {
		value = request.Metadata[a2a.GroupExtensionURI]
	}
	if value == nil && request.Message.Metadata != nil {
		value = request.Message.Metadata[a2a.GroupExtensionURI]
	}
	metadata := groupExtensionMetadata{ReplyPolicy: groupReplyPolicyAll}
	if value == nil {
		return metadata, nil
	}
	encoded, err := json.Marshal(value)
	if err != nil || json.Unmarshal(encoded, &metadata) != nil {
		return metadata, standardError(http.StatusBadRequest, "INVALID_ARGUMENT", "group extension metadata is invalid")
	}
	switch metadata.ReplyPolicy {
	case groupReplyPolicyAll, groupReplyPolicyMentionedOnly, groupReplyPolicyAckOnly:
	default:
		return metadata, standardError(http.StatusBadRequest, "INVALID_ARGUMENT", "replyPolicy must be ALL, MENTIONED_ONLY, or ACK_ONLY")
	}
	seen := make(map[string]struct{}, len(metadata.Mentions))
	for _, mention := range metadata.Mentions {
		mention = strings.TrimSpace(mention)
		if mention == "" || strings.ContainsAny(mention, "\r\n") {
			return metadata, standardError(http.StatusBadRequest, "INVALID_ARGUMENT", "mentions contains an invalid Agent ID")
		}
		if _, exists := seen[mention]; exists {
			return metadata, standardError(http.StatusBadRequest, "INVALID_ARGUMENT", "mentions contains a duplicate Agent ID")
		}
		seen[mention] = struct{}{}
	}
	return metadata, nil
}

func (service *Service) CreateStandardGroupTask(ctx context.Context, requester hub.RegisteredAgent, request a2a.SendMessageRequest, groupID string) (a2a.TaskRecord, bool, error) {
	_, _, err := service.groupMemberForStandard(ctx, requester, groupID)
	if err != nil {
		return a2a.TaskRecord{}, false, err
	}
	text, err := validateStandardMessage(request.Message)
	if err != nil {
		return a2a.TaskRecord{}, false, err
	}
	digest, err := messageDigest(request.Message)
	if err != nil {
		return a2a.TaskRecord{}, false, err
	}
	if existing, findErr := service.store.StandardTasks().FindTaskByMessage(ctx, requester.HubID, requester.CircleID, requester.AgentID, groupTenantPrefix+groupID, request.Message.MessageID); findErr == nil {
		if existing.ContentDigest != digest {
			return a2a.TaskRecord{}, false, standardError(http.StatusConflict, "INVALID_ARGUMENT", "messageId conflicts with an existing group task")
		}
		return existing, true, nil
	} else if !errors.Is(findErr, store.ErrNotFound) {
		return a2a.TaskRecord{}, false, findErr
	}
	metadata, err := service.parseGroupMetadata(request)
	if err != nil {
		return a2a.TaskRecord{}, false, err
	}
	members, err := service.store.Groups().ListMembers(ctx, groupID)
	if err != nil {
		return a2a.TaskRecord{}, false, err
	}
	mentionSet := make(map[string]struct{}, len(metadata.Mentions))
	for _, mentioned := range metadata.Mentions {
		mentionSet[mentioned] = struct{}{}
	}
	for mentioned := range mentionSet {
		member, memberErr := service.store.Groups().FindMember(ctx, groupID, mentioned)
		if memberErr != nil || !member.IsActive() || member.CircleID != requester.CircleID {
			return a2a.TaskRecord{}, false, standardError(http.StatusBadRequest, "INVALID_ARGUMENT", "mentions contains a non-member Agent")
		}
	}
	eligible := make([]hub.GroupMember, 0, len(members))
	for _, member := range members {
		if !member.IsActive() || member.AgentID == requester.AgentID || member.CircleID != requester.CircleID {
			continue
		}
		if metadata.ReplyPolicy == groupReplyPolicyMentionedOnly {
			if _, ok := mentionSet[member.AgentID]; !ok {
				continue
			}
		}
		agent, findErr := service.store.Agents().FindAgent(ctx, member.AgentID)
		if findErr != nil || agent.StateAt(service.now().UTC()) == hub.AgentStateExpired || agent.StateAt(service.now().UTC()) == hub.AgentStateRevoked || !supportsStandardExecution(agent) {
			continue
		}
		if pending, countErr := service.store.Inbox().PendingCount(ctx, member.AgentID); countErr != nil {
			return a2a.TaskRecord{}, false, countErr
		} else if pending >= service.config.MaxConcurrentTasks {
			return a2a.TaskRecord{}, false, standardError(http.StatusTooManyRequests, "RESOURCE_EXHAUSTED", "group member capacity is exhausted")
		}
		eligible = append(eligible, member)
	}
	if len(eligible) == 0 {
		return a2a.TaskRecord{}, false, standardError(http.StatusBadRequest, "GROUP_NO_RECIPIENTS", "group has no eligible recipients")
	}
	if len(eligible) > service.maxGroupFanout() {
		return a2a.TaskRecord{}, false, standardError(http.StatusTooManyRequests, "RESOURCE_EXHAUSTED", "group fan-out limit exceeded")
	}
	now := service.now().UTC()
	parentID := fmt.Sprintf("a2a-group-task-%d", now.UnixNano())
	contextID := strings.TrimSpace(request.Message.ContextID)
	if contextID == "" {
		contextID = fmt.Sprintf("a2a-group-context-%d", now.UnixNano())
	}
	parentDigest := digest
	parentMessage := request.Message
	parentMessage.TaskID = parentID
	parentMessage.ContextID = contextID
	parent := a2a.TaskRecord{HubID: requester.HubID, ID: parentID, CircleID: requester.CircleID, RequesterAgentID: requester.AgentID, TargetAgentID: groupTenantPrefix + groupID, ContextID: contextID, MessageID: request.Message.MessageID, TurnID: fmt.Sprintf("a2a-group-turn-%d", now.UnixNano()), Revision: 1, State: a2a.TaskStateSubmitted, Message: parentMessage, History: []a2a.Message{parentMessage}, ContentDigest: parentDigest, ExecutionDeadline: now.Add(5 * time.Minute), RetryBudget: 3, CreatedAt: now, UpdatedAt: now}
	children := make([]a2a.TaskRecord, 0, len(eligible))
	items := make([]hub.InboxItem, 0, len(eligible))
	links := make([]a2a.GroupTaskMember, 0, len(eligible))
	for index, member := range eligible {
		memberTaskID := fmt.Sprintf("%s-member-%s", parentID, member.AgentID)
		childMessage := request.Message
		childMessage.MessageID = request.Message.MessageID + ":" + member.AgentID
		childMessage.TaskID = memberTaskID
		childMessage.ContextID = contextID
		childDigest, digestErr := messageDigest(childMessage)
		if digestErr != nil {
			return a2a.TaskRecord{}, false, digestErr
		}
		turnID := fmt.Sprintf("a2a-turn-%d-%d", now.UnixNano(), index)
		children = append(children, a2a.TaskRecord{HubID: requester.HubID, ID: memberTaskID, CircleID: requester.CircleID, RequesterAgentID: requester.AgentID, TargetAgentID: member.AgentID, ContextID: contextID, MessageID: childMessage.MessageID, TurnID: turnID, Revision: 1, State: a2a.TaskStateSubmitted, Message: childMessage, History: []a2a.Message{childMessage}, ContentDigest: childDigest, ExecutionDeadline: now.Add(5 * time.Minute), RetryBudget: 3, CreatedAt: now, UpdatedAt: now})
		items = append(items, hub.InboxItem{HubID: requester.HubID, CircleID: requester.CircleID, TargetAgentID: member.AgentID, RequesterAgentID: requester.AgentID, TaskID: memberTaskID, ContextID: contextID, IdempotencyKey: "a2a:group:" + parentID + ":" + member.AgentID, Message: text, GroupID: groupID, Protocol: "A2A/1.0", MessageID: childMessage.MessageID, TurnID: turnID, TaskRevision: 1, ParentTaskID: parentID, MemberTaskID: memberTaskID, ReplyPolicy: metadata.ReplyPolicy, Mentions: metadata.Mentions, CreatedAt: now})
		links = append(links, a2a.GroupTaskMember{GroupID: groupID, CircleID: requester.CircleID, TargetAgentID: member.AgentID, ReplyPolicy: metadata.ReplyPolicy, Mentions: metadata.Mentions, Ordinal: index, CreatedAt: now})
	}
	created, duplicate, err := service.store.StandardTasks().CreateGroupTask(ctx, parent, children, items, links, service.config.MaxConcurrentTasks, service.maxGroupFanout())
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			return a2a.TaskRecord{}, false, standardError(http.StatusConflict, "INVALID_ARGUMENT", "messageId conflicts with an existing group task")
		}
		if errors.Is(err, store.ErrInvalidState) {
			return a2a.TaskRecord{}, false, standardError(http.StatusConflict, "GROUP_PRECONDITION_FAILED", "group membership or capacity changed before fan-out")
		}
		return a2a.TaskRecord{}, false, err
	}
	eventType := hub.EventTaskQueued
	if duplicate {
		eventType = hub.EventTaskDuplicate
	}
	service.audit(ctx, hub.Event{Type: eventType, ActorAgentID: requester.AgentID, TargetAgentID: groupTenantPrefix + groupID, TaskID: created.ID, Details: map[string]any{"groupId": groupID, "memberCount": len(eligible)}})
	if !duplicate && service.broker != nil {
		for _, item := range items {
			if stored, _, findErr := service.store.Inbox().FindByIdempotencyKey(ctx, hub.IdempotencyKey{HubID: requester.HubID, TargetAgentID: item.TargetAgentID, RequesterAgentID: requester.AgentID, Key: item.IdempotencyKey}); findErr == nil {
				service.broker.Publish(stored)
			}
		}
	}
	return created, duplicate, nil
}

func (server *HTTPServer) standardSendGroupMessage(w http.ResponseWriter, r *http.Request, requester hub.RegisteredAgent, request a2a.SendMessageRequest, groupID string) {
	task, _, err := server.service.CreateStandardGroupTask(r.Context(), requester, request, groupID)
	if err != nil {
		writeStandardServiceError(w, err)
		return
	}
	if request.Configuration == nil || !request.Configuration.ReturnImmediately {
		if !server.standardWaitSlots.acquire(requester.AgentID) {
			writeStandardError(w, &StandardError{HTTPStatus: http.StatusTooManyRequests, Reason: "RESOURCE_EXHAUSTED", Message: "too many standard waits for this Agent"})
			return
		}
		task, err = server.service.WaitForStandardTask(r.Context(), requester, task.ID)
		server.standardWaitSlots.release(requester.AgentID)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			writeStandardServiceError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, a2a.SendMessageResponse{Task: taskPointer(task.PublicTask(-1, true))})
}

func (server *HTTPServer) standardStreamGroupMessage(w http.ResponseWriter, r *http.Request, requester hub.RegisteredAgent, request a2a.SendMessageRequest, groupID string) {
	task, _, err := server.service.CreateStandardGroupTask(r.Context(), requester, request, groupID)
	if err != nil {
		writeStandardServiceError(w, err)
		return
	}
	if !server.standardStreamSlots.acquire(requester.AgentID) {
		writeStandardError(w, &StandardError{HTTPStatus: http.StatusTooManyRequests, Reason: "RESOURCE_EXHAUSTED", Message: "too many standard streams for this Agent"})
		return
	}
	defer server.standardStreamSlots.release(requester.AgentID)
	server.streamStandardTask(w, r, requester, task)
}

func decodeGroupPageToken(value string, requester hub.RegisteredAgent) (int, error) {
	if value == "" {
		return 0, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return 0, standardError(http.StatusBadRequest, "INVALID_ARGUMENT", "pageToken is invalid")
	}
	var token struct {
		Offset   int    `json:"offset"`
		AgentID  string `json:"agentId"`
		CircleID string `json:"circleId"`
	}
	if err := json.Unmarshal(decoded, &token); err != nil || token.Offset < 0 || token.AgentID != requester.AgentID || token.CircleID != requester.CircleID {
		return 0, standardError(http.StatusBadRequest, "INVALID_ARGUMENT", "pageToken is invalid for this scope")
	}
	return token.Offset, nil
}

func (server *HTTPServer) standardListGroups(w http.ResponseWriter, r *http.Request) {
	if !server.standardRequestChecks(w, r) {
		return
	}
	requester, ok := server.standardAuth(w, r)
	if !ok {
		return
	}
	pageSize, err := parseIntQuery(r, "pageSize", 50)
	if err != nil || pageSize < 1 || pageSize > 100 {
		writeStandardError(w, &StandardError{HTTPStatus: http.StatusBadRequest, Reason: "INVALID_ARGUMENT", Message: "pageSize must be between 1 and 100"})
		return
	}
	offset, err := decodeGroupPageToken(r.URL.Query().Get("pageToken"), requester)
	if err != nil {
		writeStandardServiceError(w, err)
		return
	}
	groups, next, err := server.service.ListStandardGroups(r.Context(), requester, server.baseURLFor(r), pageSize, offset)
	if err != nil {
		writeStandardServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, standardGroupListResponse{Groups: groups, NextPageToken: next})
}

func (server *HTTPServer) standardGroupCard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	token, ok := bearerToken(r.Header.Get("Authorization"))
	if !ok {
		writeStandardError(w, &StandardError{HTTPStatus: http.StatusUnauthorized, Reason: "UNAUTHENTICATED", Message: "authentication failed"})
		return
	}
	requester, err := server.service.AuthenticateStandardAgent(r.Context(), token)
	if err != nil {
		writeStandardServiceError(w, err)
		return
	}
	card, err := server.service.StandardGroupCard(r.Context(), requester, r.PathValue("groupId"), server.baseURLFor(r))
	if err != nil {
		writeStandardServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, card)
}
