package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/tbdavid2019/888a2a-lite/internal/hub"
	"github.com/tbdavid2019/888a2a-lite/internal/store"
)

func (repository *Repository) CreateGroup(ctx context.Context, group hub.Group) (hub.Group, error) {
	if strings.TrimSpace(group.HubID) == "" || strings.TrimSpace(group.GroupID) == "" || strings.TrimSpace(group.Name) == "" || strings.TrimSpace(group.OwnerAgentID) == "" {
		return hub.Group{}, errors.New("group requires hub id, group id, name, and owner")
	}
	if group.State == "" {
		group.State = hub.GroupStateActive
	}
	if group.CreatedAt.IsZero() {
		group.CreatedAt = time.Now().UTC()
	}
	err := repository.withTransaction(ctx, func(tx *Repository) error {
		if _, err := tx.executor().ExecContext(ctx, `
INSERT INTO agent_group (hub_id, group_id, name, state, owner_agent_id, created_at, archived_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`, group.HubID, group.GroupID, group.Name, string(group.State),
			group.OwnerAgentID, formatTime(group.CreatedAt), nullTimePtr(group.ArchivedAt)); err != nil {
			return err
		}
		_, err := tx.executor().ExecContext(ctx, `
INSERT INTO group_member (hub_id, group_id, agent_id, role, state, joined_at, left_at, removed_at)
VALUES (?, ?, ?, 'OWNER', 'ACTIVE', ?, NULL, NULL)`, group.HubID, group.GroupID, group.OwnerAgentID, formatTime(group.CreatedAt))
		return err
	})
	if err != nil {
		return hub.Group{}, err
	}
	return group, nil
}

func (repository *Repository) FindGroup(ctx context.Context, groupID string) (hub.Group, error) {
	return scanGroup(repository.executor().QueryRowContext(ctx, `
SELECT hub_id, group_id, name, state, owner_agent_id, created_at, archived_at
FROM agent_group WHERE group_id = ?`, groupID))
}

func (repository *Repository) ListGroups(ctx context.Context, agentID string) ([]hub.Group, error) {
	rows, err := repository.executor().QueryContext(ctx, `
SELECT g.hub_id, g.group_id, g.name, g.state, g.owner_agent_id, g.created_at, g.archived_at
FROM agent_group g
JOIN group_member m ON m.hub_id = g.hub_id AND m.group_id = g.group_id
WHERE m.agent_id = ? AND m.state = 'ACTIVE'
ORDER BY g.created_at, g.group_id`, agentID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	groups := make([]hub.Group, 0)
	for rows.Next() {
		group, err := scanGroup(rows)
		if err != nil {
			return nil, err
		}
		groups = append(groups, group)
	}
	return groups, rows.Err()
}

func (repository *Repository) FindMember(ctx context.Context, groupID, agentID string) (hub.GroupMember, error) {
	return scanGroupMember(repository.executor().QueryRowContext(ctx, `
SELECT hub_id, group_id, agent_id, role, state, joined_at, left_at, removed_at
FROM group_member WHERE group_id = ? AND agent_id = ?`, groupID, agentID))
}

func (repository *Repository) ListMembers(ctx context.Context, groupID string) ([]hub.GroupMember, error) {
	rows, err := repository.executor().QueryContext(ctx, `
SELECT hub_id, group_id, agent_id, role, state, joined_at, left_at, removed_at
FROM group_member WHERE group_id = ? AND state = 'ACTIVE'
ORDER BY joined_at, agent_id`, groupID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	members := make([]hub.GroupMember, 0)
	for rows.Next() {
		member, err := scanGroupMember(rows)
		if err != nil {
			return nil, err
		}
		members = append(members, member)
	}
	return members, rows.Err()
}

func (repository *Repository) CreateInvitation(ctx context.Context, invitation hub.GroupInvitation) (hub.GroupInvitation, error) {
	if invitation.CreatedAt.IsZero() {
		invitation.CreatedAt = time.Now().UTC()
	}
	if invitation.ExpiresAt.IsZero() {
		invitation.ExpiresAt = invitation.CreatedAt.Add(24 * time.Hour)
	}
	if invitation.State == "" {
		invitation.State = hub.InvitationPending
	}
	var resultInvitation hub.GroupInvitation
	err := repository.withTransaction(ctx, func(tx *Repository) error {
		if _, err := tx.executor().ExecContext(ctx, `
UPDATE group_invitation SET state = 'EXPIRED'
WHERE hub_id = ? AND group_id = ? AND invitee_agent_id = ? AND state = 'PENDING' AND expires_at <= ?`,
			invitation.HubID, invitation.GroupID, invitation.InviteeAgentID, formatTime(invitation.CreatedAt)); err != nil {
			return err
		}
		result, err := tx.executor().ExecContext(ctx, `
INSERT INTO group_invitation (
    hub_id, group_id, inviter_agent_id, invitee_agent_id, state, created_at, expires_at, responded_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, invitation.HubID, invitation.GroupID, invitation.InviterAgentID,
			invitation.InviteeAgentID, string(invitation.State), formatTime(invitation.CreatedAt),
			formatTime(invitation.ExpiresAt), nullTimePtr(invitation.RespondedAt))
		if err != nil {
			return err
		}
		id, err := result.LastInsertId()
		if err != nil {
			return err
		}
		invitation.ID = uint64(id)
		resultInvitation = invitation
		return nil
	})
	if err != nil {
		return hub.GroupInvitation{}, err
	}
	return resultInvitation, nil
}

func (repository *Repository) FindInvitation(ctx context.Context, id uint64) (hub.GroupInvitation, error) {
	return scanInvitation(repository.executor().QueryRowContext(ctx, `
SELECT id, hub_id, group_id, inviter_agent_id, invitee_agent_id, state, created_at, expires_at, responded_at
FROM group_invitation WHERE id = ?`, id))
}

func (repository *Repository) FindPendingInvitation(ctx context.Context, groupID, inviteeAgentID string) (hub.GroupInvitation, error) {
	return scanInvitation(repository.executor().QueryRowContext(ctx, `
SELECT id, hub_id, group_id, inviter_agent_id, invitee_agent_id, state, created_at, expires_at, responded_at
FROM group_invitation WHERE group_id = ? AND invitee_agent_id = ? AND state = 'PENDING' AND expires_at > ?
ORDER BY id DESC LIMIT 1`, groupID, inviteeAgentID, formatTime(time.Now().UTC())))
}

func (repository *Repository) ListInvitations(ctx context.Context, inviteeAgentID string) ([]hub.GroupInvitation, error) {
	rows, err := repository.executor().QueryContext(ctx, `
SELECT id, hub_id, group_id, inviter_agent_id, invitee_agent_id, state, created_at, expires_at, responded_at
FROM group_invitation WHERE invitee_agent_id = ? ORDER BY id`, inviteeAgentID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	invitations := make([]hub.GroupInvitation, 0)
	for rows.Next() {
		invitation, err := scanInvitation(rows)
		if err != nil {
			return nil, err
		}
		invitations = append(invitations, invitation)
	}
	return invitations, rows.Err()
}

func (repository *Repository) AcceptInvitation(ctx context.Context, id uint64, agentID string, acceptedAt time.Time) (hub.GroupMember, error) {
	var member hub.GroupMember
	err := repository.withTransaction(ctx, func(tx *Repository) error {
		invitation, err := tx.FindInvitation(ctx, id)
		if err != nil {
			return err
		}
		if invitation.InviteeAgentID != agentID {
			return store.ErrForbidden
		}
		if invitation.State == hub.InvitationAccepted {
			member, err = tx.FindMember(ctx, invitation.GroupID, agentID)
			return err
		}
		if invitation.State != hub.InvitationPending || !invitation.ExpiresAt.After(acceptedAt) {
			return store.ErrInvalidState
		}
		group, err := tx.FindGroup(ctx, invitation.GroupID)
		if err != nil {
			return err
		}
		if !group.IsActive() {
			return store.ErrInvalidState
		}
		var state string
		var expires string
		if err := tx.executor().QueryRowContext(ctx, `SELECT state, expires_at FROM agent WHERE agent_id = ?`, agentID).Scan(&state, &expires); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return store.ErrNotFound
			}
			return err
		}
		if state == string(hub.AgentStateRevoked) {
			return store.ErrForbidden
		}
		expiresAt, err := time.Parse(time.RFC3339Nano, expires)
		if err != nil {
			return err
		}
		if !expiresAt.After(acceptedAt) {
			return store.ErrForbidden
		}
		var activeCount int
		if err := tx.executor().QueryRowContext(ctx, `SELECT count(*) FROM group_member WHERE group_id = ? AND state = 'ACTIVE'`, invitation.GroupID).Scan(&activeCount); err != nil {
			return err
		}
		if activeCount >= 32 {
			return store.ErrInvalidState
		}
		_, err = tx.executor().ExecContext(ctx, `
INSERT INTO group_member (hub_id, group_id, agent_id, role, state, joined_at, left_at, removed_at)
VALUES (?, ?, ?, 'MEMBER', 'ACTIVE', ?, NULL, NULL)
ON CONFLICT (hub_id, group_id, agent_id) DO UPDATE SET
    role = 'MEMBER', state = 'ACTIVE', joined_at = excluded.joined_at,
    left_at = NULL, removed_at = NULL`, invitation.HubID, invitation.GroupID, agentID, formatTime(acceptedAt))
		if err != nil {
			return err
		}
		_, err = tx.executor().ExecContext(ctx, `
UPDATE group_invitation SET state = 'ACCEPTED', responded_at = ?
WHERE id = ? AND state = 'PENDING'`, formatTime(acceptedAt), id)
		if err != nil {
			return err
		}
		member, err = tx.FindMember(ctx, invitation.GroupID, agentID)
		return err
	})
	return member, err
}

func (repository *Repository) LeaveGroup(ctx context.Context, groupID, agentID string, at time.Time) error {
	return repository.withTransaction(ctx, func(tx *Repository) error {
		member, err := tx.FindMember(ctx, groupID, agentID)
		if err != nil {
			return err
		}
		if member.State != hub.MembershipActive {
			return nil
		}
		if member.Role == hub.GroupRoleOwner {
			return store.ErrInvalidState
		}
		if _, err := tx.executor().ExecContext(ctx, `
UPDATE group_member SET state = 'LEFT', left_at = ? WHERE group_id = ? AND agent_id = ? AND state = 'ACTIVE'`,
			formatTime(at), groupID, agentID); err != nil {
			return err
		}
		return tx.cancelPendingGroupDeliveries(ctx, groupID, agentID, at)
	})
}

func (repository *Repository) RemoveMember(ctx context.Context, groupID, agentID string, at time.Time) error {
	return repository.withTransaction(ctx, func(tx *Repository) error {
		member, err := tx.FindMember(ctx, groupID, agentID)
		if err != nil {
			return err
		}
		if member.Role == hub.GroupRoleOwner {
			return store.ErrInvalidState
		}
		if member.State != hub.MembershipActive {
			return nil
		}
		if _, err := tx.executor().ExecContext(ctx, `
UPDATE group_member SET state = 'REMOVED', removed_at = ? WHERE group_id = ? AND agent_id = ? AND state = 'ACTIVE'`,
			formatTime(at), groupID, agentID); err != nil {
			return err
		}
		return tx.cancelPendingGroupDeliveries(ctx, groupID, agentID, at)
	})
}

func (repository *Repository) TransferOwnership(ctx context.Context, groupID, fromAgentID, toAgentID string) error {
	return repository.withTransaction(ctx, func(tx *Repository) error {
		group, err := tx.FindGroup(ctx, groupID)
		if err != nil {
			return err
		}
		if group.OwnerAgentID != fromAgentID || !group.IsActive() {
			return store.ErrForbidden
		}
		target, err := tx.FindMember(ctx, groupID, toAgentID)
		if err != nil {
			return err
		}
		if !target.IsActive() || toAgentID == fromAgentID {
			return store.ErrInvalidState
		}
		if _, err := tx.executor().ExecContext(ctx, `UPDATE group_member SET role = 'MEMBER' WHERE group_id = ? AND agent_id = ?`, groupID, fromAgentID); err != nil {
			return err
		}
		if _, err := tx.executor().ExecContext(ctx, `UPDATE group_member SET role = 'OWNER' WHERE group_id = ? AND agent_id = ? AND state = 'ACTIVE'`, groupID, toAgentID); err != nil {
			return err
		}
		result, err := tx.executor().ExecContext(ctx, `UPDATE agent_group SET owner_agent_id = ? WHERE group_id = ? AND state = 'ACTIVE'`, toAgentID, groupID)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return store.ErrNotFound
		}
		return nil
	})
}

func (repository *Repository) ArchiveGroup(ctx context.Context, groupID string, at time.Time) error {
	result, err := repository.executor().ExecContext(ctx, `
UPDATE agent_group SET state = 'ARCHIVED', archived_at = ? WHERE group_id = ? AND state = 'ACTIVE'`, formatTime(at), groupID)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return store.ErrNotFound
	}
	return nil
}

func (repository *Repository) SendGroupMessage(ctx context.Context, message hub.GroupMessage, maxFanout int) (hub.GroupMessage, bool, error) {
	var resultMessage hub.GroupMessage
	duplicate := false
	err := repository.withTransaction(ctx, func(tx *Repository) error {
		if existing, err := tx.findGroupMessageByIdempotency(ctx, message.GroupID, message.SenderAgentID, message.IdempotencyKey); err == nil {
			resultMessage = existing
			resultMessage.Deliveries, err = tx.listGroupDeliveries(ctx, existing.ID)
			duplicate = true
			return err
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
		group, err := tx.FindGroup(ctx, message.GroupID)
		if err != nil {
			return err
		}
		if !group.IsActive() {
			return store.ErrInvalidState
		}
		sender, err := tx.FindMember(ctx, message.GroupID, message.SenderAgentID)
		if err != nil {
			return store.ErrForbidden
		}
		if !sender.IsActive() {
			return store.ErrForbidden
		}
		members, err := tx.ListMembers(ctx, message.GroupID)
		if err != nil {
			return err
		}
		recipients := make([]hub.GroupMember, 0, len(members))
		for _, member := range members {
			if member.AgentID != message.SenderAgentID {
				recipients = append(recipients, member)
			}
		}
		if len(recipients) == 0 || (maxFanout > 0 && len(recipients) > maxFanout) {
			return store.ErrInvalidState
		}
		if message.CreatedAt.IsZero() {
			message.CreatedAt = time.Now().UTC()
		}
		result, err := tx.executor().ExecContext(ctx, `
INSERT INTO group_message (hub_id, group_id, sender_agent_id, context_id, idempotency_key, message, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`, message.HubID, message.GroupID, message.SenderAgentID, message.ContextID,
			message.IdempotencyKey, message.Message, formatTime(message.CreatedAt))
		if err != nil {
			return err
		}
		id, err := result.LastInsertId()
		if err != nil {
			return err
		}
		message.ID = uint64(id)
		message.Trust = "UNTRUSTED_DATA"
		message.Deliveries = make([]hub.GroupDeliverySummary, 0, len(recipients))
		for _, recipient := range recipients {
			internalKey := "group:" + message.GroupID + ":" + message.IdempotencyKey
			result, err := tx.executor().ExecContext(ctx, `
INSERT INTO inbox_item (
    hub_id, target_agent_id, requester_agent_id, task_id, context_id, idempotency_key,
    message, state, created_at, acknowledged_at, canceled_at, cancel_reason, group_id, group_message_id
) VALUES (?, ?, ?, ?, ?, ?, ?, 'PENDING', ?, NULL, NULL, '', ?, ?)`, message.HubID, recipient.AgentID,
				message.SenderAgentID, fmt.Sprintf("group-message-%d", message.ID), message.ContextID, internalKey,
				message.Message, formatTime(message.CreatedAt), message.GroupID, message.ID)
			if err != nil {
				return err
			}
			sequence, err := result.LastInsertId()
			if err != nil {
				return err
			}
			if _, err := tx.executor().ExecContext(ctx, `
INSERT INTO group_delivery (sequence, hub_id, group_id, group_message_id, target_agent_id, state)
VALUES (?, ?, ?, ?, ?, 'PENDING')`, sequence, message.HubID, message.GroupID, message.ID, recipient.AgentID); err != nil {
				return err
			}
			message.Deliveries = append(message.Deliveries, hub.GroupDeliverySummary{
				TargetAgentID: recipient.AgentID, Sequence: uint64(sequence), State: hub.DeliveryStatePending,
			})
		}
		resultMessage = message
		return nil
	})
	return resultMessage, duplicate, err
}

func (repository *Repository) ListGroupMessages(ctx context.Context, groupID, agentID string, afterID uint64, limit int) ([]hub.GroupMessage, error) {
	rows, err := repository.executor().QueryContext(ctx, `
SELECT m.id, m.hub_id, m.group_id, m.sender_agent_id, m.context_id,
       m.idempotency_key, m.message, m.created_at
FROM group_message m
JOIN group_member member ON member.hub_id = m.hub_id AND member.group_id = m.group_id
WHERE m.group_id = ? AND member.agent_id = ? AND member.state = 'ACTIVE'
  AND m.id > ? AND m.created_at >= member.joined_at
ORDER BY m.id LIMIT ?`, groupID, agentID, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	messages := make([]hub.GroupMessage, 0, limit)
	for rows.Next() {
		message, err := scanGroupMessage(rows)
		if err != nil {
			return nil, err
		}
		message.Trust = "UNTRUSTED_DATA"
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for index := range messages {
		messages[index].Deliveries, err = repository.listGroupDeliveriesForAgent(ctx, messages[index].ID, agentID)
		if err != nil {
			return nil, err
		}
	}
	return messages, nil
}

func (repository *Repository) listGroupDeliveriesForAgent(ctx context.Context, messageID uint64, agentID string) ([]hub.GroupDeliverySummary, error) {
	rows, err := repository.executor().QueryContext(ctx, `
SELECT target_agent_id, sequence, state FROM group_delivery
WHERE group_message_id = ? AND target_agent_id = ? ORDER BY sequence`, messageID, agentID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	deliveries := make([]hub.GroupDeliverySummary, 0, 1)
	for rows.Next() {
		var delivery hub.GroupDeliverySummary
		var state string
		if err := rows.Scan(&delivery.TargetAgentID, &delivery.Sequence, &state); err != nil {
			return nil, err
		}
		delivery.State = hub.DeliveryState(state)
		deliveries = append(deliveries, delivery)
	}
	return deliveries, rows.Err()
}

func (repository *Repository) CancelPendingGroupDeliveries(ctx context.Context, groupID, agentID string, at time.Time) error {
	return repository.withTransaction(ctx, func(tx *Repository) error {
		return tx.cancelPendingGroupDeliveries(ctx, groupID, agentID, at)
	})
}

func (repository *Repository) cancelPendingGroupDeliveries(ctx context.Context, groupID, agentID string, at time.Time) error {
	if _, err := repository.executor().ExecContext(ctx, `
UPDATE inbox_item SET state = 'CANCELED', canceled_at = ?, cancel_reason = 'membership removed'
WHERE sequence IN (
    SELECT sequence FROM group_delivery
    WHERE group_id = ? AND target_agent_id = ? AND state = 'PENDING' AND polled_at IS NULL
) AND state = 'PENDING'`, formatTime(at), groupID, agentID); err != nil {
		return err
	}
	_, err := repository.executor().ExecContext(ctx, `
UPDATE group_delivery SET state = 'CANCELED', canceled_at = ?
WHERE group_id = ? AND target_agent_id = ? AND state = 'PENDING' AND polled_at IS NULL`, formatTime(at), groupID, agentID)
	return err
}

func (repository *Repository) findGroupMessageByIdempotency(ctx context.Context, groupID, senderID, key string) (hub.GroupMessage, error) {
	return scanGroupMessage(repository.executor().QueryRowContext(ctx, `
SELECT id, hub_id, group_id, sender_agent_id, context_id, idempotency_key, message, created_at
FROM group_message WHERE group_id = ? AND sender_agent_id = ? AND idempotency_key = ?`, groupID, senderID, key))
}

func (repository *Repository) ListGroupMessagesAdmin(ctx context.Context, beforeID uint64, limit int, groupID, agentID string) ([]hub.GroupMessage, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	groupID = strings.TrimSpace(groupID)
	agentID = strings.TrimSpace(agentID)
	query := `
SELECT id, hub_id, group_id, sender_agent_id, context_id, idempotency_key, message, created_at
FROM group_message
WHERE (? = 0 OR id < ?)
  AND (? = '' OR group_id = ?)
  AND (? = '' OR sender_agent_id = ?)
ORDER BY id DESC LIMIT ?`
	rows, err := repository.executor().QueryContext(ctx, query, beforeID, beforeID, groupID, groupID, agentID, agentID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	messages := make([]hub.GroupMessage, 0, limit)
	for rows.Next() {
		message, err := scanGroupMessage(rows)
		if err != nil {
			return nil, err
		}
		message.Trust = "UNTRUSTED_DATA"
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for index := range messages {
		deliveries, err := repository.listGroupDeliveries(ctx, messages[index].ID)
		if err != nil {
			return nil, err
		}
		messages[index].Deliveries = deliveries
	}
	return messages, nil
}

func (repository *Repository) listGroupDeliveries(ctx context.Context, messageID uint64) ([]hub.GroupDeliverySummary, error) {
	rows, err := repository.executor().QueryContext(ctx, `
SELECT target_agent_id, sequence, state FROM group_delivery
WHERE group_message_id = ? ORDER BY sequence`, messageID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	deliveries := make([]hub.GroupDeliverySummary, 0)
	for rows.Next() {
		var delivery hub.GroupDeliverySummary
		var state string
		if err := rows.Scan(&delivery.TargetAgentID, &delivery.Sequence, &state); err != nil {
			return nil, err
		}
		delivery.State = hub.DeliveryState(state)
		deliveries = append(deliveries, delivery)
	}
	return deliveries, rows.Err()
}

func scanGroup(row scanner) (hub.Group, error) {
	var group hub.Group
	var state, created, archived sql.NullString
	if err := row.Scan(&group.HubID, &group.GroupID, &group.Name, &state, &group.OwnerAgentID, &created, &archived); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return hub.Group{}, ErrNotFound
		}
		return hub.Group{}, err
	}
	group.State = hub.GroupState(state.String)
	var err error
	if group.CreatedAt, err = parseRequiredTime(created); err != nil {
		return hub.Group{}, err
	}
	if group.ArchivedAt, err = parseNullableTimePtr(archived); err != nil {
		return hub.Group{}, err
	}
	return group, nil
}

func scanGroupMember(row scanner) (hub.GroupMember, error) {
	var member hub.GroupMember
	var role, state, joined, left, removed sql.NullString
	if err := row.Scan(&member.HubID, &member.GroupID, &member.AgentID, &role, &state, &joined, &left, &removed); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return hub.GroupMember{}, ErrNotFound
		}
		return hub.GroupMember{}, err
	}
	member.Role = hub.GroupRole(role.String)
	member.State = hub.MembershipState(state.String)
	var err error
	if member.JoinedAt, err = parseRequiredTime(joined); err != nil {
		return hub.GroupMember{}, err
	}
	if member.LeftAt, err = parseNullableTimePtr(left); err != nil {
		return hub.GroupMember{}, err
	}
	if member.RemovedAt, err = parseNullableTimePtr(removed); err != nil {
		return hub.GroupMember{}, err
	}
	return member, nil
}

func scanInvitation(row scanner) (hub.GroupInvitation, error) {
	var invitation hub.GroupInvitation
	var state, created, expires, responded sql.NullString
	if err := row.Scan(&invitation.ID, &invitation.HubID, &invitation.GroupID, &invitation.InviterAgentID,
		&invitation.InviteeAgentID, &state, &created, &expires, &responded); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return hub.GroupInvitation{}, ErrNotFound
		}
		return hub.GroupInvitation{}, err
	}
	invitation.State = hub.InvitationState(state.String)
	var err error
	if invitation.CreatedAt, err = parseRequiredTime(created); err != nil {
		return hub.GroupInvitation{}, err
	}
	if invitation.ExpiresAt, err = parseRequiredTime(expires); err != nil {
		return hub.GroupInvitation{}, err
	}
	if invitation.RespondedAt, err = parseNullableTimePtr(responded); err != nil {
		return hub.GroupInvitation{}, err
	}
	return invitation, nil
}

func scanGroupMessage(row scanner) (hub.GroupMessage, error) {
	var message hub.GroupMessage
	var created string
	if err := row.Scan(&message.ID, &message.HubID, &message.GroupID, &message.SenderAgentID,
		&message.ContextID, &message.IdempotencyKey, &message.Message, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return hub.GroupMessage{}, ErrNotFound
		}
		return hub.GroupMessage{}, err
	}
	var err error
	if message.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		return hub.GroupMessage{}, fmt.Errorf("parse group message created_at: %w", err)
	}
	return message, nil
}
