package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/tbdavid2019/888a2a-lite/internal/hub"
	"github.com/tbdavid2019/888a2a-lite/internal/store"
)

var (
	ErrGroupArchived     = errors.New("group is archived")
	ErrGroupLimit        = errors.New("group limit reached")
	ErrGroupUnavailable  = errors.New("group is unavailable")
	ErrInvitationInvalid = errors.New("invitation is invalid")
)

func (service *Service) CreateGroup(ctx context.Context, agentID, token string, input hub.CreateGroupInput) (hub.Group, error) {
	if _, err := service.AuthenticateAgent(ctx, agentID, token); err != nil {
		return hub.Group{}, err
	}
	if err := hub.ValidateCreateGroup(input); err != nil {
		return hub.Group{}, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	groupID, err := generateGroupID()
	if err != nil {
		return hub.Group{}, err
	}
	now := service.now().UTC()
	group, err := service.store.Groups().CreateGroup(ctx, hub.Group{
		HubID: service.config.HubID, GroupID: groupID, Name: strings.TrimSpace(input.Name),
		State: hub.GroupStateActive, OwnerAgentID: agentID, CreatedAt: now,
	})
	if err == nil {
		service.audit(ctx, hub.Event{Type: hub.EventGroupCreated, ActorAgentID: agentID, Details: map[string]any{"groupId": group.GroupID}})
	}
	return group, err
}

func (service *Service) ListGroups(ctx context.Context, agentID, token string) ([]hub.Group, error) {
	if _, err := service.AuthenticateAgent(ctx, agentID, token); err != nil {
		return nil, err
	}
	return service.store.Groups().ListGroups(ctx, agentID)
}

func (service *Service) GetGroup(ctx context.Context, agentID, token, groupID string) (hub.Group, []hub.GroupMember, error) {
	if _, err := service.requireGroupMember(ctx, agentID, token, groupID); err != nil {
		return hub.Group{}, nil, err
	}
	group, err := service.store.Groups().FindGroup(ctx, groupID)
	if err != nil {
		return hub.Group{}, nil, err
	}
	members, err := service.store.Groups().ListMembers(ctx, groupID)
	return group, members, err
}

func (service *Service) InviteMember(ctx context.Context, agentID, token, groupID, inviteeAgentID string) (hub.GroupInvitation, error) {
	actor, err := service.requireActiveGroupMember(ctx, agentID, token, groupID)
	if err != nil {
		return hub.GroupInvitation{}, err
	}
	if !actor.CanManageMembers() {
		return hub.GroupInvitation{}, ErrForbidden
	}
	group, err := service.store.Groups().FindGroup(ctx, groupID)
	if err != nil {
		return hub.GroupInvitation{}, err
	}
	if !group.IsActive() {
		return hub.GroupInvitation{}, ErrGroupArchived
	}
	invitee, err := service.store.Agents().FindAgent(ctx, strings.TrimSpace(inviteeAgentID))
	if err != nil {
		return hub.GroupInvitation{}, ErrAgentUnavailable
	}
	state := invitee.StateAt(service.now().UTC())
	if state == hub.AgentStateExpired || state == hub.AgentStateRevoked {
		return hub.GroupInvitation{}, ErrAgentUnavailable
	}
	if existing, err := service.store.Groups().FindMember(ctx, groupID, invitee.AgentID); err == nil && existing.IsActive() {
		return hub.GroupInvitation{}, ErrGroupUnavailable
	} else if err != nil && !errors.Is(err, store.ErrNotFound) {
		return hub.GroupInvitation{}, err
	}
	members, err := service.store.Groups().ListMembers(ctx, groupID)
	if err != nil {
		return hub.GroupInvitation{}, err
	}
	if len(members) >= service.maxGroupMembers() {
		return hub.GroupInvitation{}, ErrGroupLimit
	}
	if existing, err := service.store.Groups().FindPendingInvitation(ctx, groupID, invitee.AgentID); err == nil {
		return existing, nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return hub.GroupInvitation{}, err
	}
	now := service.now().UTC()
	invitation, err := service.store.Groups().CreateInvitation(ctx, hub.GroupInvitation{
		HubID: service.config.HubID, GroupID: groupID, InviterAgentID: agentID, InviteeAgentID: invitee.AgentID,
		State: hub.InvitationPending, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour),
	})
	if err == nil {
		service.audit(ctx, hub.Event{Type: hub.EventGroupInvitationCreated, ActorAgentID: agentID, TargetAgentID: invitee.AgentID, Details: map[string]any{"groupId": groupID, "invitationId": invitation.ID}})

		// Push group invitation to invitee's inbox and real-time SSE stream
		inviteMsg := fmt.Sprintf("[群組邀請] Agent %s 邀請你加入群組「%s」(群組ID: %s, 邀請序號: %d)。若要接受邀請，請發送 POST /hub/v1/groups/invitations/%d/accept（或呼叫 POST /hub/v1/groups/%s/accept）確認加入。",
			agentID, group.Name, groupID, invitation.ID, invitation.ID, groupID)
		inboxItem := hub.InboxItem{
			HubID:            service.config.HubID,
			TargetAgentID:    invitee.AgentID,
			RequesterAgentID: agentID,
			TaskID:           fmt.Sprintf("invite-%s-%d", groupID, invitation.ID),
			ContextID:        fmt.Sprintf("group-invite-%s", groupID),
			IdempotencyKey:   fmt.Sprintf("idem-invite-%s-%d", groupID, invitation.ID),
			Message:          inviteMsg,
			GroupID:          groupID,
			Trust:            "UNTRUSTED_DATA",
			State:            hub.DeliveryStatePending,
			CreatedAt:        now,
		}
		if stored, _, enqueueErr := service.store.Inbox().Enqueue(ctx, inboxItem); enqueueErr == nil {
			if service.broker != nil {
				service.broker.Publish(stored)
			}
		}
	}
	return invitation, err
}

func (service *Service) ListInvitations(ctx context.Context, agentID, token string) ([]hub.GroupInvitation, error) {
	if _, err := service.AuthenticateAgent(ctx, agentID, token); err != nil {
		return nil, err
	}
	return service.store.Groups().ListInvitations(ctx, agentID)
}

func (service *Service) AcceptInvitation(ctx context.Context, agentID, token string, invitationID uint64) (hub.GroupMember, error) {
	if _, err := service.AuthenticateAgent(ctx, agentID, token); err != nil {
		return hub.GroupMember{}, err
	}
	member, err := service.store.Groups().AcceptInvitation(ctx, invitationID, agentID, service.now().UTC())
	if err == nil {
		service.audit(ctx, hub.Event{Type: hub.EventGroupInvitationAccepted, ActorAgentID: agentID, Details: map[string]any{"groupId": member.GroupID, "invitationId": invitationID}})

		now := service.now().UTC()
		// Automatically acknowledge the invitation task in the inbox
		taskID := fmt.Sprintf("invite-%s-%d", member.GroupID, invitationID)
		_ = service.store.Inbox().AcknowledgeTask(ctx, agentID, taskID, now)

		// Push member joined notification to group owner via SSE
		if group, gErr := service.store.Groups().FindGroup(ctx, member.GroupID); gErr == nil && group.OwnerAgentID != agentID {
			joinMsg := fmt.Sprintf("[群組動態] Agent %s 已接受邀請，正式加入群組「%s」(群組ID: %s)！", agentID, group.Name, member.GroupID)
			ownerNotice := hub.InboxItem{
				HubID:            service.config.HubID,
				TargetAgentID:    group.OwnerAgentID,
				RequesterAgentID: agentID,
				TaskID:           fmt.Sprintf("member-joined-%s-%s-%d", member.GroupID, agentID, now.Unix()),
				ContextID:        fmt.Sprintf("group-roster-%s", member.GroupID),
				IdempotencyKey:   fmt.Sprintf("idem-joined-%s-%s-%d", member.GroupID, agentID, now.Unix()),
				Message:          joinMsg,
				GroupID:          member.GroupID,
				Trust:            "UNTRUSTED_DATA",
				State:            hub.DeliveryStatePending,
				CreatedAt:        now,
			}
			if stored, _, enqueueErr := service.store.Inbox().Enqueue(ctx, ownerNotice); enqueueErr == nil {
				if service.broker != nil {
					service.broker.Publish(stored)
				}
			}
		}
	}
	return member, err
}

func (service *Service) AcceptInvitationByGroup(ctx context.Context, agentID, token, groupID string) (hub.GroupMember, error) {
	if _, err := service.AuthenticateAgent(ctx, agentID, token); err != nil {
		return hub.GroupMember{}, err
	}
	invitation, err := service.store.Groups().FindPendingInvitation(ctx, groupID, agentID)
	if err != nil {
		return hub.GroupMember{}, err
	}
	return service.AcceptInvitation(ctx, agentID, token, invitation.ID)
}

func (service *Service) LeaveGroup(ctx context.Context, agentID, token, groupID string) error {
	if _, err := service.requireActiveGroupMember(ctx, agentID, token, groupID); err != nil {
		return err
	}
	err := service.store.Groups().LeaveGroup(ctx, groupID, agentID, service.now().UTC())
	if err == nil {
		service.audit(ctx, hub.Event{Type: hub.EventGroupMemberLeft, ActorAgentID: agentID, Details: map[string]any{"groupId": groupID}})
	}
	return err
}

func (service *Service) RemoveMember(ctx context.Context, agentID, token, groupID, targetAgentID string) error {
	actor, err := service.requireActiveGroupMember(ctx, agentID, token, groupID)
	if err != nil {
		return err
	}
	if !actor.CanManageMembers() || actor.AgentID == targetAgentID {
		return ErrForbidden
	}
	err = service.store.Groups().RemoveMember(ctx, groupID, targetAgentID, service.now().UTC())
	if err == nil {
		service.audit(ctx, hub.Event{Type: hub.EventGroupMemberRemoved, ActorAgentID: agentID, TargetAgentID: targetAgentID, Details: map[string]any{"groupId": groupID}})
	}
	return err
}

func (service *Service) TransferOwnership(ctx context.Context, agentID, token, groupID, targetAgentID string) error {
	actor, err := service.requireActiveGroupMember(ctx, agentID, token, groupID)
	if err != nil {
		return err
	}
	if actor.Role != hub.GroupRoleOwner {
		return ErrForbidden
	}
	err = service.store.Groups().TransferOwnership(ctx, groupID, agentID, targetAgentID)
	if err == nil {
		service.audit(ctx, hub.Event{Type: hub.EventGroupOwnershipTransferred, ActorAgentID: agentID, TargetAgentID: targetAgentID, Details: map[string]any{"groupId": groupID}})
	}
	return err
}

func (service *Service) ArchiveGroup(ctx context.Context, agentID, token, groupID string) error {
	actor, err := service.requireActiveGroupMember(ctx, agentID, token, groupID)
	if err != nil {
		return err
	}
	if actor.Role != hub.GroupRoleOwner {
		return ErrForbidden
	}
	err = service.store.Groups().ArchiveGroup(ctx, groupID, service.now().UTC())
	if err == nil {
		service.audit(ctx, hub.Event{Type: hub.EventGroupArchived, ActorAgentID: agentID, Details: map[string]any{"groupId": groupID}})
	}
	return err
}

func (service *Service) GroupRoster(ctx context.Context, agentID, token, groupID, baseURL string) ([]hub.GroupMember, error) {
	if _, err := service.requireGroupMember(ctx, agentID, token, groupID); err != nil {
		return nil, err
	}
	members, err := service.store.Groups().ListMembers(ctx, groupID)
	if err != nil {
		return nil, err
	}
	for index := range members {
		agent, err := service.store.Agents().FindAgent(ctx, members[index].AgentID)
		if err != nil {
			return nil, err
		}
		view := agent.SafeView(baseURL)
		view.State = agent.StateAt(service.now().UTC())
		members[index].Agent = &view
	}
	service.audit(ctx, hub.Event{Type: hub.EventGroupRosterViewed, ActorAgentID: agentID, Details: map[string]any{"groupId": groupID, "count": len(members)}})
	return members, nil
}

func (service *Service) GroupHistory(ctx context.Context, agentID, token, groupID, baseURL string, afterID uint64, limit int) ([]hub.GroupHistoryItem, uint64, error) {
	if _, err := service.requireGroupMember(ctx, agentID, token, groupID); err != nil {
		return nil, afterID, err
	}
	if limit < 1 || limit > service.maxGroupHistoryPage() {
		return nil, afterID, fmt.Errorf("group history limit must be between 1 and %d", service.maxGroupHistoryPage())
	}
	messages, err := service.store.Groups().ListGroupMessages(ctx, groupID, agentID, afterID, limit)
	if err != nil {
		return nil, afterID, err
	}
	items := make([]hub.GroupHistoryItem, 0, len(messages))
	next := afterID
	for _, message := range messages {
		item := hub.GroupHistoryItem{GroupMessage: message}
		if sender, err := service.store.Agents().FindAgent(ctx, message.SenderAgentID); err == nil {
			view := sender.SafeView(baseURL)
			view.State = sender.StateAt(service.now().UTC())
			item.Sender = &view
		}
		items = append(items, item)
		if message.ID > next {
			next = message.ID
		}
	}
	service.audit(ctx, hub.Event{Type: hub.EventGroupHistoryViewed, ActorAgentID: agentID, Details: map[string]any{"groupId": groupID, "count": len(items), "afterId": afterID}})
	return items, next, nil
}

func (service *Service) SendGroupMessage(ctx context.Context, agentID, token, groupID string, input hub.GroupMessageInput) (hub.GroupMessage, bool, error) {
	if _, err := service.requireActiveGroupMember(ctx, agentID, token, groupID); err != nil {
		return hub.GroupMessage{}, false, err
	}
	if err := hub.ValidateGroupMessage(input, service.config.MaxPayloadBytes); err != nil {
		return hub.GroupMessage{}, false, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	message, duplicate, err := service.store.Groups().SendGroupMessage(ctx, hub.GroupMessage{
		HubID: service.config.HubID, GroupID: groupID, SenderAgentID: agentID,
		ContextID: strings.TrimSpace(input.ContextID), IdempotencyKey: input.IdempotencyKey,
		Message: input.Message, Trust: "UNTRUSTED_DATA", CreatedAt: service.now().UTC(),
	}, service.maxGroupFanout())
	if err == nil {
		eventType := hub.EventGroupMessageQueued
		if duplicate {
			eventType = hub.EventGroupMessageDuplicate
		}
		service.audit(ctx, hub.Event{Type: eventType, ActorAgentID: agentID, Details: map[string]any{"groupId": groupID, "groupMessageId": message.ID, "recipientCount": len(message.Deliveries)}})
		if !duplicate && service.broker != nil {
			for _, d := range message.Deliveries {
				service.broker.Publish(hub.InboxItem{
					Sequence:         d.Sequence,
					HubID:            message.HubID,
					TargetAgentID:    d.TargetAgentID,
					RequesterAgentID: message.SenderAgentID,
					TaskID:           fmt.Sprintf("group-%s-%d", message.GroupID, message.ID),
					ContextID:        message.ContextID,
					IdempotencyKey:   message.IdempotencyKey,
					Message:          message.Message,
					GroupID:          message.GroupID,
					GroupMessageID:   message.ID,
					Trust:            message.Trust,
					State:            d.State,
					CreatedAt:        message.CreatedAt,
				})
			}
		}
	}
	return message, duplicate, err
}

func (service *Service) requireGroupMember(ctx context.Context, agentID, token, groupID string) (hub.GroupMember, error) {
	if _, err := service.AuthenticateAgent(ctx, agentID, token); err != nil {
		return hub.GroupMember{}, err
	}
	member, err := service.store.Groups().FindMember(ctx, groupID, agentID)
	if err != nil || !member.IsActive() {
		service.audit(ctx, hub.Event{Type: hub.EventGroupAuthorizationDenied, ActorAgentID: agentID, Details: map[string]any{"groupId": groupID}})
		return hub.GroupMember{}, ErrForbidden
	}
	return member, nil
}

func (service *Service) requireActiveGroupMember(ctx context.Context, agentID, token, groupID string) (hub.GroupMember, error) {
	member, err := service.requireGroupMember(ctx, agentID, token, groupID)
	if err != nil {
		return hub.GroupMember{}, err
	}
	group, err := service.store.Groups().FindGroup(ctx, groupID)
	if err != nil {
		return hub.GroupMember{}, err
	}
	if !group.IsActive() {
		return hub.GroupMember{}, ErrGroupArchived
	}
	return member, nil
}

func (service *Service) maxGroupMembers() int {
	if service.config.MaxGroupMembers > 0 {
		return service.config.MaxGroupMembers
	}
	return hub.MaxGroupMembers
}

func (service *Service) maxGroupFanout() int {
	if service.config.MaxGroupFanout > 0 {
		return service.config.MaxGroupFanout
	}
	return hub.MaxGroupFanout
}

func (service *Service) maxGroupHistoryPage() int {
	if service.config.MaxGroupHistoryPage > 0 {
		return service.config.MaxGroupHistoryPage
	}
	return hub.MaxGroupHistoryPageSize
}

func generateGroupID() (string, error) {
	bytes := make([]byte, 12)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate group id: %w", err)
	}
	return "group-" + hex.EncodeToString(bytes), nil
}
