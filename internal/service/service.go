package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/tbdavid2019/888a2a-lite/internal/config"
	"github.com/tbdavid2019/888a2a-lite/internal/hub"
	"github.com/tbdavid2019/888a2a-lite/internal/store"
)

var (
	ErrRegistrationDisabled = errors.New("registration is disabled")
	ErrUnauthenticated      = errors.New("authentication failed")
	ErrForbidden            = errors.New("operation is not permitted")
	ErrAgentUnavailable     = errors.New("agent is unavailable")
	ErrAgentLimit           = errors.New("registered agent limit reached")
	ErrTaskLimit            = errors.New("task limit reached")
	ErrValidation           = errors.New("validation failed")
)

type HubStatus struct {
	HubID               string `json:"hubId"`
	RegistrationEnabled bool   `json:"registrationEnabled"`
	RegisteredAgents    int    `json:"registeredAgents"`
	PendingTasks        int    `json:"pendingTasks"`
}

type Service struct {
	store        store.Store
	config       config.Config
	operatorHash string
	now          func() time.Time
}

func New(database store.Store, cfg config.Config) *Service {
	operatorHash := ""
	if cfg.OperatorToken != "" {
		operatorHash = hub.HashToken(cfg.OperatorToken)
	}
	return &Service{store: database, config: cfg, operatorHash: operatorHash, now: time.Now}
}

func (service *Service) Register(ctx context.Context, declaration hub.AgentDeclaration) (hub.AgentIdentity, bool, error) {
	if err := hub.ValidateDeclaration(declaration); err != nil {
		return hub.AgentIdentity{}, false, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	policy, err := service.store.Policy().GetPolicy(ctx)
	if errors.Is(err, store.ErrNotFound) {
		policy = service.policyFromConfig()
		if err := service.store.Policy().SavePolicy(ctx, policy); err != nil {
			return hub.AgentIdentity{}, false, err
		}
	} else if err != nil {
		return hub.AgentIdentity{}, false, err
	}
	if !policy.RegistrationEnabled {
		return hub.AgentIdentity{}, false, ErrRegistrationDisabled
	}
	if existing, findErr := service.store.Agents().FindAgentByRegistrationKey(ctx, declaration.RegistrationIdempotency); findErr == nil {
		service.audit(ctx, hub.Event{Type: hub.EventAgentRegistrationRetry, ActorAgentID: existing.AgentID})
		return hub.AgentIdentity{HubID: existing.HubID, AgentID: existing.AgentID, ExpiresAt: existing.ExpiresAt}, true, nil
	} else if !errors.Is(findErr, store.ErrNotFound) {
		return hub.AgentIdentity{}, false, findErr
	}
	count, err := service.store.Agents().CountAgents(ctx)
	if err != nil {
		return hub.AgentIdentity{}, false, err
	}
	if count >= policy.MaxRegisteredAgents {
		return hub.AgentIdentity{}, false, ErrAgentLimit
	}
	token, err := hub.GenerateToken()
	if err != nil {
		return hub.AgentIdentity{}, false, err
	}
	agentID, err := generateAgentID()
	if err != nil {
		return hub.AgentIdentity{}, false, err
	}
	now := service.now().UTC()
	agent := hub.RegisteredAgent{
		HubID:               policy.HubID,
		AgentID:             agentID,
		RegistrationKeyHash: hub.HashToken(declaration.RegistrationIdempotency),
		TokenHash:           hub.HashToken(token),
		DisplayName:         strings.TrimSpace(declaration.DisplayName),
		ProviderFamily:      strings.TrimSpace(declaration.ProviderFamily),
		TransportID:         strings.TrimSpace(declaration.TransportID),
		Capabilities:        append([]string(nil), declaration.Capabilities...),
		State:               hub.AgentStatePending,
		ExpiresAt:           now.Add(policy.RegistrationTTL),
		CreatedAt:           now,
	}
	if err := service.store.Agents().CreateAgent(ctx, agent); err != nil {
		return hub.AgentIdentity{}, false, err
	}
	service.audit(ctx, hub.Event{Type: hub.EventAgentRegistered, ActorAgentID: agent.AgentID})
	return hub.AgentIdentity{HubID: agent.HubID, AgentID: agent.AgentID, AgentToken: token, ExpiresAt: agent.ExpiresAt}, false, nil
}

func (service *Service) AuthenticateAgent(ctx context.Context, agentID, token string) (hub.RegisteredAgent, error) {
	agent, err := service.store.Agents().AuthenticateAgent(ctx, agentID, token)
	if err != nil {
		return hub.RegisteredAgent{}, ErrUnauthenticated
	}
	state := agent.StateAt(service.now().UTC())
	if state == hub.AgentStateExpired || state == hub.AgentStateRevoked {
		return hub.RegisteredAgent{}, ErrUnauthenticated
	}
	return agent, nil
}

func (service *Service) ListAgents(ctx context.Context, agentID, token, baseURL string) ([]hub.AgentView, error) {
	if _, err := service.AuthenticateAgent(ctx, agentID, token); err != nil {
		return nil, err
	}
	agents, err := service.store.Agents().ListAgents(ctx)
	if err != nil {
		return nil, err
	}
	views := make([]hub.AgentView, 0, len(agents))
	for _, agent := range agents {
		view := agent.SafeView(baseURL)
		view.State = agent.StateAt(service.now().UTC())
		views = append(views, view)
	}
	return views, nil
}

func (service *Service) GetAgent(ctx context.Context, requesterID, token, targetID, baseURL string) (hub.AgentView, error) {
	if _, err := service.AuthenticateAgent(ctx, requesterID, token); err != nil {
		return hub.AgentView{}, err
	}
	agent, err := service.store.Agents().FindAgent(ctx, targetID)
	if err != nil {
		return hub.AgentView{}, err
	}
	view := agent.SafeView(baseURL)
	view.State = agent.StateAt(service.now().UTC())
	return view, nil
}

func (service *Service) Heartbeat(ctx context.Context, agentID, token, baseURL string) (hub.AgentView, error) {
	if _, err := service.AuthenticateAgent(ctx, agentID, token); err != nil {
		return hub.AgentView{}, err
	}
	now := service.now().UTC()
	agent, err := service.store.Agents().HeartbeatAgent(ctx, agentID, now, now.Add(service.config.PeerLease))
	if err != nil {
		return hub.AgentView{}, ErrAgentUnavailable
	}
	view := agent.SafeView(baseURL)
	view.State = agent.StateAt(now)
	service.audit(ctx, hub.Event{Type: hub.EventAgentHeartbeat, ActorAgentID: agentID})
	return view, nil
}

func (service *Service) Disconnect(ctx context.Context, agentID, token string) error {
	if _, err := service.AuthenticateAgent(ctx, agentID, token); err != nil {
		return err
	}
	if err := service.store.Agents().DisconnectAgent(ctx, agentID, service.now().UTC()); err != nil {
		return ErrAgentUnavailable
	}
	service.audit(ctx, hub.Event{Type: hub.EventAgentDisconnected, ActorAgentID: agentID})
	return nil
}

func (service *Service) SendTask(ctx context.Context, requesterID, token string, task hub.TaskDelivery) (hub.InboxItem, bool, error) {
	if _, err := service.AuthenticateAgent(ctx, requesterID, token); err != nil {
		return hub.InboxItem{}, false, err
	}
	if err := hub.ValidateTaskDelivery(task); err != nil {
		return hub.InboxItem{}, false, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	target, err := service.store.Agents().FindAgent(ctx, task.TargetAgentID)
	if err != nil {
		return hub.InboxItem{}, false, ErrAgentUnavailable
	}
	state := target.StateAt(service.now().UTC())
	if state == hub.AgentStateExpired || state == hub.AgentStateRevoked {
		return hub.InboxItem{}, false, ErrAgentUnavailable
	}
	if existing, found, err := service.store.Inbox().FindByIdempotencyKey(ctx, hub.IdempotencyKey{
		HubID: service.config.HubID, TargetAgentID: task.TargetAgentID,
		RequesterAgentID: requesterID, Key: task.IdempotencyKey,
	}); err == nil && found {
		service.audit(ctx, hub.Event{Type: hub.EventTaskDuplicate, ActorAgentID: requesterID, TargetAgentID: task.TargetAgentID, TaskID: existing.TaskID})
		return existing, true, nil
	} else if err != nil && !errors.Is(err, store.ErrNotFound) {
		return hub.InboxItem{}, false, err
	}
	if pending, err := service.store.Inbox().PendingCount(ctx, task.TargetAgentID); err != nil {
		return hub.InboxItem{}, false, err
	} else if pending >= service.config.MaxConcurrentTasks {
		return hub.InboxItem{}, false, ErrTaskLimit
	}
	item := hub.InboxItem{
		HubID:            service.config.HubID,
		TargetAgentID:    task.TargetAgentID,
		RequesterAgentID: requesterID,
		TaskID:           task.TaskID,
		ContextID:        task.ContextID,
		IdempotencyKey:   task.IdempotencyKey,
		Message:          task.Message,
		State:            hub.DeliveryStatePending,
		CreatedAt:        service.now().UTC(),
	}
	stored, duplicate, err := service.store.Inbox().Enqueue(ctx, item)
	if err == nil {
		eventType := hub.EventTaskQueued
		if duplicate {
			eventType = hub.EventTaskDuplicate
		}
		service.audit(ctx, hub.Event{Type: eventType, ActorAgentID: requesterID, TargetAgentID: task.TargetAgentID, TaskID: stored.TaskID})
	}
	return stored, duplicate, err
}

func (service *Service) Poll(ctx context.Context, agentID, token string, after uint64, limit int) ([]hub.InboxItem, error) {
	if _, err := service.AuthenticateAgent(ctx, agentID, token); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 100 {
		return nil, fmt.Errorf("inbox limit must be between 1 and 100")
	}
	items, err := service.store.Inbox().Poll(ctx, agentID, after, limit)
	if err == nil {
		service.audit(ctx, hub.Event{Type: hub.EventInboxPolled, ActorAgentID: agentID, Details: map[string]any{"count": len(items)}})
		groupCounts := make(map[string]int)
		for _, item := range items {
			if item.GroupID != "" {
				groupCounts[item.GroupID]++
			}
		}
		for groupID, count := range groupCounts {
			service.audit(ctx, hub.Event{Type: hub.EventGroupDeliveryPolled, ActorAgentID: agentID, Details: map[string]any{"groupId": groupID, "count": count}})
		}
	}
	return items, err
}

func (service *Service) Acknowledge(ctx context.Context, agentID, token string, sequence uint64) error {
	if _, err := service.AuthenticateAgent(ctx, agentID, token); err != nil {
		return err
	}
	err := service.store.Inbox().Acknowledge(ctx, agentID, sequence, service.now().UTC())
	if err == nil {
		service.audit(ctx, hub.Event{Type: hub.EventInboxAcknowledged, ActorAgentID: agentID, Details: map[string]any{"sequence": sequence}})
	}
	return err
}

func (service *Service) Status(ctx context.Context) (HubStatus, error) {
	policy, err := service.store.Policy().GetPolicy(ctx)
	if err != nil {
		policy = service.policyFromConfig()
	}
	registered, err := service.store.Agents().CountAgents(ctx)
	if err != nil {
		return HubStatus{}, err
	}
	pending, err := service.store.Inbox().PendingCount(ctx, "")
	if err != nil {
		pending = 0
	}
	return HubStatus{HubID: policy.HubID, RegistrationEnabled: policy.RegistrationEnabled, RegisteredAgents: registered, PendingTasks: pending}, nil
}

func (service *Service) AuthenticateOperator(token string) error {
	if service.operatorHash == "" || !hub.VerifyToken(service.operatorHash, token) {
		return ErrUnauthenticated
	}
	return nil
}

func (service *Service) SetRegistrationEnabled(ctx context.Context, enabled bool) error {
	err := service.store.Policy().SetRegistrationEnabled(ctx, enabled)
	if err == nil {
		service.audit(ctx, hub.Event{Type: hub.EventRegistrationChanged, Details: map[string]any{"enabled": enabled}})
	}
	return err
}

func (service *Service) Revoke(ctx context.Context, agentID, reason string) error {
	err := service.store.Agents().RevokeAgent(ctx, agentID, reason, service.now().UTC())
	if err == nil {
		service.audit(ctx, hub.Event{Type: hub.EventAgentRevoked, TargetAgentID: agentID, Details: map[string]any{"hasReason": strings.TrimSpace(reason) != ""}})
	}
	return err
}

func (service *Service) CancelTask(ctx context.Context, taskID, reason string) error {
	err := service.store.Inbox().CancelTask(ctx, taskID, reason, service.now().UTC())
	if err == nil {
		service.audit(ctx, hub.Event{Type: hub.EventTaskCanceled, TaskID: taskID, Details: map[string]any{"hasReason": strings.TrimSpace(reason) != ""}})
	}
	return err
}

func (service *Service) ListEvents(ctx context.Context, token string, afterID uint64, limit int) ([]hub.Event, error) {
	if err := service.AuthenticateOperator(token); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 1000 {
		return nil, fmt.Errorf("event limit must be between 1 and 1000")
	}
	return service.store.Events().ListEvents(ctx, afterID, limit)
}

func (service *Service) BuildSystemCard(baseURL string) hub.HubSystemCard {
	baseURL = strings.TrimRight(baseURL, "/")
	return hub.HubSystemCard{
		HubID:                service.config.HubID,
		SelfURL:              baseURL + "/hub/v1/system-card.json",
		Mode:                 "PUBLIC",
		Protocol:             "a2a888-hub",
		ProtocolVersion:      "1",
		DeliverySemantics:    "AT_LEAST_ONCE",
		CapabilityTrust:      "SELF_DECLARED",
		IncomingMessageTrust: "UNTRUSTED_DATA",
		SystemMetadataTrust:  "CONTROL_PLANE_METADATA",
		RemoteExecution:      false,
		SystemCardURL:        baseURL + "/hub/v1/system-card.json",
		AnnouncementFeedURL:  baseURL + "/hub/v1/announcements",
		GroupBaseURL:         baseURL + "/hub/v1/groups",
		Limits: map[string]int64{
			"maxRegisteredAgents": int64(service.config.MaxRegisteredAgents),
			"maxTasksPerMinute":   int64(service.config.MaxTasksPerMinute),
			"maxConcurrentTasks":  int64(service.config.MaxConcurrentTasks),
			"maxPayloadBytes":     service.config.MaxPayloadBytes,
			"maxGroupMembers":     int64(service.maxGroupMembers()),
			"maxGroupFanout":      int64(service.maxGroupFanout()),
			"maxGroupHistoryPage": int64(service.maxGroupHistoryPage()),
		},
		Extensions: []hub.SystemCardExtension{
			{URI: hub.AnnouncementExtensionURI, Required: false},
			{URI: hub.GroupExtensionURI, Required: false},
		},
		UpdatedAt: service.now().UTC(),
	}
}

func (service *Service) HubMetadata(ctx context.Context, baseURL string) (hub.HubMetadata, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	announcements, err := service.store.Announcements().ListActiveAnnouncements(ctx, 0, 20, service.now().UTC())
	if err != nil {
		return hub.HubMetadata{}, err
	}
	summaries := make([]hub.AnnouncementSummary, 0, len(announcements))
	var cursor uint64
	for _, announcement := range announcements {
		summaries = append(summaries, announcement.SummaryView())
		if announcement.ID > cursor {
			cursor = announcement.ID
		}
	}
	return hub.HubMetadata{
		SystemCardURL:       baseURL + "/hub/v1/system-card.json",
		AnnouncementFeedURL: baseURL + "/hub/v1/announcements",
		AnnouncementCursor:  cursor,
		Announcements:       summaries,
		ExtensionURI:        hub.AnnouncementExtensionURI,
	}, nil
}

func (service *Service) ListActiveAnnouncements(ctx context.Context, afterID uint64, limit int) ([]hub.AnnouncementSummary, uint64, error) {
	if limit < 1 || limit > 100 {
		return nil, afterID, fmt.Errorf("announcement limit must be between 1 and 100")
	}
	items, err := service.store.Announcements().ListActiveAnnouncements(ctx, afterID, limit, service.now().UTC())
	if err != nil {
		return nil, afterID, err
	}
	summaries := make([]hub.AnnouncementSummary, 0, len(items))
	next := afterID
	for _, item := range items {
		summaries = append(summaries, item.SummaryView())
		if item.ID > next {
			next = item.ID
		}
	}
	return summaries, next, nil
}

func (service *Service) PublishAnnouncement(ctx context.Context, token string, input hub.AnnouncementInput) (hub.Announcement, error) {
	if err := service.AuthenticateOperator(token); err != nil {
		return hub.Announcement{}, err
	}
	now := service.now().UTC()
	if err := input.Validate(now); err != nil {
		return hub.Announcement{}, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	announcement, err := service.store.Announcements().CreateAnnouncement(ctx, hub.Announcement{
		HubID: service.config.HubID, Revision: 1, Status: hub.AnnouncementPublished,
		Severity: input.Severity, Title: strings.TrimSpace(input.Title), Summary: strings.TrimSpace(input.Summary),
		DocumentationURL: strings.TrimSpace(input.DocumentationURL), PublishedAt: &now,
		ExpiresAt: input.ExpiresAt, CreatedAt: now,
	})
	if err == nil {
		service.audit(ctx, hub.Event{Type: hub.EventAnnouncementPublished, Details: map[string]any{"announcementId": announcement.ID, "severity": announcement.Severity}})
	}
	return announcement, err
}

func (service *Service) ListAnnouncementAdmin(ctx context.Context, token string, afterID uint64, limit int) ([]hub.Announcement, uint64, error) {
	if err := service.AuthenticateOperator(token); err != nil {
		return nil, afterID, err
	}
	if limit < 1 || limit > 1000 {
		return nil, afterID, fmt.Errorf("announcement limit must be between 1 and 1000")
	}
	items, err := service.store.Announcements().ListAnnouncements(ctx, afterID, limit)
	if err != nil {
		return nil, afterID, err
	}
	next := afterID
	for _, item := range items {
		if item.ID > next {
			next = item.ID
		}
	}
	return items, next, nil
}

func (service *Service) CreateDraft(ctx context.Context, token string, input hub.AnnouncementInput) (hub.Announcement, error) {
	if err := service.AuthenticateOperator(token); err != nil {
		return hub.Announcement{}, err
	}
	if err := input.Validate(service.now().UTC()); err != nil {
		return hub.Announcement{}, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	announcement, err := service.store.Announcements().CreateAnnouncement(ctx, hub.Announcement{
		HubID: service.config.HubID, Revision: 1, Status: hub.AnnouncementDraft,
		Severity: input.Severity, Title: strings.TrimSpace(input.Title), Summary: strings.TrimSpace(input.Summary),
		DocumentationURL: strings.TrimSpace(input.DocumentationURL), ExpiresAt: input.ExpiresAt,
		CreatedAt: service.now().UTC(),
	})
	if err == nil {
		service.audit(ctx, hub.Event{Type: hub.EventAnnouncementDraftCreated, Details: map[string]any{"announcementId": announcement.ID}})
	}
	return announcement, err
}

func (service *Service) UpdateDraft(ctx context.Context, token string, id uint64, input hub.AnnouncementInput) (hub.Announcement, error) {
	if err := service.AuthenticateOperator(token); err != nil {
		return hub.Announcement{}, err
	}
	if err := input.Validate(service.now().UTC()); err != nil {
		return hub.Announcement{}, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	announcement, err := service.store.Announcements().UpdateDraft(ctx, id, input, service.now().UTC())
	if err == nil {
		service.audit(ctx, hub.Event{Type: hub.EventAnnouncementDraftUpdated, Details: map[string]any{"announcementId": announcement.ID}})
	}
	return announcement, err
}

func (service *Service) PublishDraft(ctx context.Context, token string, id uint64) (hub.Announcement, error) {
	if err := service.AuthenticateOperator(token); err != nil {
		return hub.Announcement{}, err
	}
	announcement, err := service.store.Announcements().PublishAnnouncement(ctx, id, service.now().UTC())
	if err == nil {
		service.audit(ctx, hub.Event{Type: hub.EventAnnouncementPublished, Details: map[string]any{"announcementId": id}})
	}
	return announcement, err
}

func (service *Service) CreateRevision(ctx context.Context, token string, id uint64, input hub.AnnouncementInput) (hub.Announcement, error) {
	if err := service.AuthenticateOperator(token); err != nil {
		return hub.Announcement{}, err
	}
	if err := input.Validate(service.now().UTC()); err != nil {
		return hub.Announcement{}, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	announcement, err := service.store.Announcements().CreateRevision(ctx, id, input, service.now().UTC())
	if err == nil {
		service.audit(ctx, hub.Event{Type: hub.EventAnnouncementRevisionCreated, Details: map[string]any{"announcementId": announcement.ID, "revisionOfId": id}})
	}
	return announcement, err
}

type AdminMessagesResult struct {
	DirectMessages []hub.InboxItem    `json:"directMessages"`
	GroupMessages  []hub.GroupMessage `json:"groupMessages"`
}

func (service *Service) ListMessagesAdmin(ctx context.Context, token string, msgType string, beforeSequence, beforeID uint64, limit int, groupID, agentID string) (AdminMessagesResult, error) {
	if err := service.AuthenticateOperator(token); err != nil {
		return AdminMessagesResult{}, err
	}
	if limit < 1 || limit > 200 {
		limit = 50
	}
	result := AdminMessagesResult{
		DirectMessages: []hub.InboxItem{},
		GroupMessages:  []hub.GroupMessage{},
	}
	var err error
	if msgType == "" || msgType == "all" || msgType == "direct" {
		result.DirectMessages, err = service.store.Inbox().ListDirectMessagesAdmin(ctx, beforeSequence, limit, agentID)
		if err != nil {
			return AdminMessagesResult{}, err
		}
	}
	if msgType == "" || msgType == "all" || msgType == "group" {
		result.GroupMessages, err = service.store.Groups().ListGroupMessagesAdmin(ctx, beforeID, limit, groupID, agentID)
		if err != nil {
			return AdminMessagesResult{}, err
		}
	}
	return result, nil
}

func (service *Service) RecordEvent(ctx context.Context, eventType string) {
	service.audit(ctx, hub.Event{Type: eventType})
}

func (service *Service) audit(ctx context.Context, event hub.Event) {
	if event.HubID == "" {
		event.HubID = service.config.HubID
	}
	if err := service.store.Events().AppendEvent(ctx, event); err != nil {
		log.Printf("audit event failed type=%s", event.Type)
	}
}

func (service *Service) policyFromConfig() hub.HubPolicy {
	return hub.HubPolicy{
		HubID:               service.config.HubID,
		RegistrationEnabled: service.config.RegistrationEnabled,
		RegistrationTTL:     service.config.RegistrationTTL,
		PeerLease:           service.config.PeerLease,
		MaxRegisteredAgents: service.config.MaxRegisteredAgents,
		MaxTasksPerMinute:   service.config.MaxTasksPerMinute,
		MaxConcurrentTasks:  service.config.MaxConcurrentTasks,
		MaxPayloadBytes:     service.config.MaxPayloadBytes,
	}
}

func generateAgentID() (string, error) {
	bytes := make([]byte, 12)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate agent id: %w", err)
	}
	return "agent-" + hex.EncodeToString(bytes), nil
}
