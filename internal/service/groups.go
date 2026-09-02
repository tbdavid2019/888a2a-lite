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
		return hub.Group{}, err
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
	}
	return member, err
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
	return items, next, nil
}

func (service *Service) SendGroupMessage(ctx context.Context, agentID, token, groupID string, input hub.GroupMessageInput) (hub.GroupMessage, bool, error) {
	if _, err := service.requireActiveGroupMember(ctx, agentID, token, groupID); err != nil {
		return hub.GroupMessage{}, false, err
	}
	if err := hub.ValidateGroupMessage(input, service.config.MaxPayloadBytes); err != nil {
		return hub.GroupMessage{}, false, err
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
	}
	return message, duplicate, err
}

func (service *Service) requireGroupMember(ctx context.Context, agentID, token, groupID string) (hub.GroupMember, error) {
	if _, err := service.AuthenticateAgent(ctx, agentID, token); err != nil {
		return hub.GroupMember{}, err
	}
	member, err := service.store.Groups().FindMember(ctx, groupID, agentID)
	if err != nil || !member.IsActive() {
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
