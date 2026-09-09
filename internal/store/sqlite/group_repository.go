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
	if group.CircleID == "" {
		group.CircleID = "public"
	}
	if group.CreatedAt.IsZero() {
		group.CreatedAt = time.Now().UTC()
	}
	err := repository.withTransaction(ctx, func(tx *Repository) error {
		if _, err := tx.executor().ExecContext(ctx, `
INSERT INTO agent_group (hub_id, group_id, circle_id, name, state, owner_agent_id, created_at, archived_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, group.HubID, group.GroupID, group.CircleID, group.Name, string(group.State),
			group.OwnerAgentID, formatTime(group.CreatedAt), nullTimePtr(group.ArchivedAt)); err != nil {
			return err
		}
		_, err := tx.executor().ExecContext(ctx, `
INSERT INTO group_member (hub_id, group_id, agent_id, circle_id, role, state, joined_at, left_at, removed_at)
VALUES (?, ?, ?, ?, 'OWNER', 'ACTIVE', ?, NULL, NULL)`, group.HubID, group.GroupID, group.OwnerAgentID, group.CircleID, formatTime(group.CreatedAt))
		return err
	})
	if err != nil {
		return hub.Group{}, err
	}
	return group, nil
}

func (repository *Repository) FindGroup(ctx context.Context, groupID string) (hub.Group, error) {
	return scanGroup(repository.executor().QueryRowContext(ctx, `
SELECT hub_id, group_id, circle_id, name, state, owner_agent_id, charter_version, has_charter, charter_content_hash, charter_updated_at, created_at, archived_at
FROM agent_group WHERE group_id = ?`, groupID))
}

func (repository *Repository) ListGroups(ctx context.Context, agentID string) ([]hub.Group, error) {
	rows, err := repository.executor().QueryContext(ctx, `
	SELECT g.hub_id, g.group_id, g.circle_id, g.name, g.state, g.owner_agent_id, g.charter_version, g.has_charter, g.charter_content_hash, g.charter_updated_at, g.created_at, g.archived_at
FROM agent_group g
JOIN group_member m ON m.hub_id = g.hub_id AND m.group_id = g.group_id
WHERE m.agent_id = ? AND m.state = 'ACTIVE' AND m.circle_id = g.circle_id
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

func (repository *Repository) GetGroupCharter(ctx context.Context, hubID, circleID, groupID string) (hub.GroupCharter, error) {
	var charter hub.GroupCharter
	var hasCharter int
	var updatedAt sql.NullString
	err := repository.executor().QueryRowContext(ctx, `
SELECT group_id, charter_version, has_charter, charter_content_hash, charter_updated_at
FROM agent_group WHERE hub_id = ? AND circle_id = ? AND group_id = ?`, hubID, circleID, groupID).Scan(
		&charter.GroupID, &charter.CharterVersion, &hasCharter, &charter.ContentHash, &updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return hub.GroupCharter{}, ErrNotFound
	}
	if err != nil {
		return hub.GroupCharter{}, err
	}
	charter.HubID, charter.CircleID, charter.HasCharter = hubID, circleID, hasCharter != 0
	charter.UpdatedAt = updatedAt.String
	if !charter.HasCharter {
		return charter, nil
	}
	return scanCharter(repository.executor().QueryRowContext(ctx, `
SELECT content, updated_by, created_at, superseded_at
FROM group_charter_revision
WHERE hub_id = ? AND circle_id = ? AND group_id = ? AND version = ?`, hubID, circleID, groupID, charter.CharterVersion), charter)
}

func (repository *Repository) ListGroupCharterRevisions(ctx context.Context, hubID, circleID, groupID string) ([]hub.GroupCharter, error) {
	rows, err := repository.executor().QueryContext(ctx, `
SELECT group_id, version, content, content_hash, updated_by, created_at, superseded_at
FROM group_charter_revision WHERE hub_id = ? AND circle_id = ? AND group_id = ? ORDER BY version`, hubID, circleID, groupID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	charters := make([]hub.GroupCharter, 0)
	for rows.Next() {
		var item hub.GroupCharter
		var superseded sql.NullString
		if err := rows.Scan(&item.GroupID, &item.CharterVersion, &item.Content, &item.ContentHash, &item.UpdatedBy, &item.CreatedAt, &superseded); err != nil {
			return nil, err
		}
		item.HubID, item.CircleID, item.HasCharter = hubID, circleID, true
		if superseded.Valid {
			value := superseded.String
			item.SupersededAt = &value
		}
		charters = append(charters, item)
	}
	return charters, rows.Err()
}

func (repository *Repository) PutGroupCharter(ctx context.Context, charter hub.GroupCharter, expectedVersion int64, idempotencyKey string) (hub.GroupCharter, bool, error) {
	var result hub.GroupCharter
	duplicate := false
	err := repository.withTransaction(ctx, func(tx *Repository) error {
		var existing hub.GroupCharter
		var superseded sql.NullString
		lookupErr := tx.executor().QueryRowContext(ctx, `
SELECT group_id, version, content, content_hash, updated_by, created_at, superseded_at
FROM group_charter_revision WHERE hub_id = ? AND circle_id = ? AND group_id = ? AND idempotency_key = ?`, charter.HubID, charter.CircleID, charter.GroupID, idempotencyKey).Scan(
			&existing.GroupID, &existing.CharterVersion, &existing.Content, &existing.ContentHash, &existing.UpdatedBy, &existing.CreatedAt, &superseded,
		)
		if lookupErr == nil {
			existing.HubID, existing.CircleID, existing.HasCharter = charter.HubID, charter.CircleID, true
			if existing.ContentHash != charter.ContentHash {
				return store.ErrConflict
			}
			result, duplicate = existing, true
			return nil
		}
		if !errors.Is(lookupErr, sql.ErrNoRows) {
			return lookupErr
		}
		var currentVersion int64
		if err := tx.executor().QueryRowContext(ctx, `SELECT charter_version FROM agent_group WHERE hub_id = ? AND circle_id = ? AND group_id = ?`, charter.HubID, charter.CircleID, charter.GroupID).Scan(&currentVersion); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if currentVersion != expectedVersion {
			return store.ErrConflict
		}
		nextVersion := currentVersion + 1
		now := time.Now().UTC()
		createdAt := now.Format(time.RFC3339Nano)
		if _, err := tx.executor().ExecContext(ctx, `
INSERT INTO group_charter_revision (hub_id, circle_id, group_id, version, content, content_hash, idempotency_key, updated_by, created_at, superseded_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NULL)`, charter.HubID, charter.CircleID, charter.GroupID, nextVersion, charter.Content, charter.ContentHash, idempotencyKey, charter.UpdatedBy, createdAt); err != nil {
			return err
		}
		if currentVersion > 0 {
			if _, err := tx.executor().ExecContext(ctx, `UPDATE group_charter_revision SET superseded_at = ? WHERE hub_id = ? AND circle_id = ? AND group_id = ? AND version = ?`, createdAt, charter.HubID, charter.CircleID, charter.GroupID, currentVersion); err != nil {
				return err
			}
		}
		updated, err := tx.executor().ExecContext(ctx, `
UPDATE agent_group SET charter_version = ?, has_charter = 1, charter_content_hash = ?, charter_updated_at = ?
WHERE hub_id = ? AND circle_id = ? AND group_id = ? AND charter_version = ?`, nextVersion, charter.ContentHash, createdAt, charter.HubID, charter.CircleID, charter.GroupID, expectedVersion)
		if err != nil {
			return err
		}
		count, err := updated.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return store.ErrConflict
		}
		result = charter
		result.CharterVersion, result.HasCharter, result.CreatedAt, result.UpdatedAt = nextVersion, true, createdAt, createdAt
		return nil
	})
	return result, duplicate, err
}

func (repository *Repository) FindMember(ctx context.Context, groupID, agentID string) (hub.GroupMember, error) {
	return scanGroupMember(repository.executor().QueryRowContext(ctx, `
SELECT hub_id, group_id, agent_id, circle_id, role, state, joined_at, left_at, removed_at
FROM group_member WHERE group_id = ? AND agent_id = ?`, groupID, agentID))
}

func (repository *Repository) ListMembers(ctx context.Context, groupID string) ([]hub.GroupMember, error) {
	rows, err := repository.executor().QueryContext(ctx, `
SELECT hub_id, group_id, agent_id, circle_id, role, state, joined_at, left_at, removed_at
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
	if invitation.CircleID == "" {
		invitation.CircleID = "public"
	}
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
    hub_id, group_id, circle_id, inviter_agent_id, invitee_agent_id, state, created_at, expires_at, responded_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, invitation.HubID, invitation.GroupID, invitation.CircleID, invitation.InviterAgentID,
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
SELECT id, hub_id, group_id, circle_id, inviter_agent_id, invitee_agent_id, state, created_at, expires_at, responded_at
FROM group_invitation WHERE id = ?`, id))
}

func (repository *Repository) FindPendingInvitation(ctx context.Context, groupID, inviteeAgentID string) (hub.GroupInvitation, error) {
	return scanInvitation(repository.executor().QueryRowContext(ctx, `
SELECT i.id, i.hub_id, i.group_id, i.circle_id, i.inviter_agent_id, i.invitee_agent_id, i.state, i.created_at, i.expires_at, i.responded_at
FROM group_invitation i
JOIN agent a ON a.hub_id = i.hub_id AND a.agent_id = i.invitee_agent_id AND a.circle_id = i.circle_id
WHERE i.group_id = ? AND i.invitee_agent_id = ? AND i.state = 'PENDING' AND i.expires_at > ?
ORDER BY i.id DESC LIMIT 1`, groupID, inviteeAgentID, formatTime(time.Now().UTC())))
}

func (repository *Repository) ListInvitations(ctx context.Context, inviteeAgentID string) ([]hub.GroupInvitation, error) {
	rows, err := repository.executor().QueryContext(ctx, `
SELECT i.id, i.hub_id, i.group_id, i.circle_id, i.inviter_agent_id, i.invitee_agent_id, i.state, i.created_at, i.expires_at, i.responded_at
FROM group_invitation i
JOIN agent a ON a.hub_id = i.hub_id AND a.agent_id = i.invitee_agent_id AND a.circle_id = i.circle_id
WHERE i.invitee_agent_id = ? ORDER BY i.id`, inviteeAgentID)
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
		var state, agentCircleID string
		var expires string
		if err := tx.executor().QueryRowContext(ctx, `SELECT circle_id, state, expires_at FROM agent WHERE agent_id = ?`, agentID).Scan(&agentCircleID, &state, &expires); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return store.ErrNotFound
			}
			return err
		}
		if invitation.CircleID != agentCircleID {
			return store.ErrNotFound
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
INSERT INTO group_member (hub_id, group_id, agent_id, circle_id, role, state, joined_at, left_at, removed_at)
VALUES (?, ?, ?, ?, 'MEMBER', 'ACTIVE', ?, NULL, NULL)
ON CONFLICT (hub_id, group_id, agent_id) DO UPDATE SET
    role = 'MEMBER', state = 'ACTIVE', joined_at = excluded.joined_at,
			left_at = NULL, removed_at = NULL`, invitation.HubID, invitation.GroupID, agentID, invitation.CircleID, formatTime(acceptedAt))
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
		if message.CircleID == "" {
			message.CircleID = group.CircleID
		}
		if message.CircleID != group.CircleID {
			return store.ErrNotFound
		}
		result, err := tx.executor().ExecContext(ctx, `
INSERT INTO group_message (hub_id, group_id, circle_id, sender_agent_id, context_id, idempotency_key, message, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, message.HubID, message.GroupID, message.CircleID, message.SenderAgentID, message.ContextID,
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
    hub_id, circle_id, target_agent_id, requester_agent_id, task_id, context_id, idempotency_key,
    message, state, created_at, acknowledged_at, canceled_at, cancel_reason, group_id, group_message_id
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'PENDING', ?, NULL, NULL, '', ?, ?)`, message.HubID, message.CircleID, recipient.AgentID,
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
INSERT INTO group_delivery (sequence, hub_id, circle_id, group_id, group_message_id, target_agent_id, state)
VALUES (?, ?, ?, ?, ?, ?, 'PENDING')`, sequence, message.HubID, message.CircleID, message.GroupID, message.ID, recipient.AgentID); err != nil {
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
SELECT m.id, m.hub_id, m.group_id, m.circle_id, m.sender_agent_id, m.context_id,
       m.idempotency_key, m.message, m.created_at
FROM group_message m
JOIN group_member member ON member.hub_id = m.hub_id AND member.group_id = m.group_id
WHERE m.group_id = ? AND member.agent_id = ? AND member.state = 'ACTIVE' AND member.circle_id = m.circle_id
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
SELECT id, hub_id, group_id, circle_id, sender_agent_id, context_id, idempotency_key, message, created_at
FROM group_message WHERE group_id = ? AND sender_agent_id = ? AND idempotency_key = ?`, groupID, senderID, key))
}

func (repository *Repository) ListGroupMessagesAdmin(ctx context.Context, beforeID uint64, limit int, groupID, agentID string) ([]hub.GroupMessage, error) {
	return repository.ListGroupMessagesAdminInCircle(ctx, beforeID, limit, groupID, agentID, "")
}

func (repository *Repository) ListGroupMessagesAdminInCircle(ctx context.Context, beforeID uint64, limit int, groupID, agentID, circleID string) ([]hub.GroupMessage, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	groupID = strings.TrimSpace(groupID)
	agentID = strings.TrimSpace(agentID)
	query := `
SELECT id, hub_id, group_id, circle_id, sender_agent_id, context_id, idempotency_key, message, created_at
FROM group_message
WHERE (? = 0 OR id < ?)
  AND (? = '' OR group_id = ?)
  AND (? = '' OR sender_agent_id = ?)
  AND (? = '' OR circle_id = ?)
ORDER BY id DESC LIMIT ?`
	rows, err := repository.executor().QueryContext(ctx, query, beforeID, beforeID, groupID, groupID, agentID, agentID, circleID, circleID, limit)
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
	var state, created, archived, charterUpdated sql.NullString
	var hasCharter int
	if err := row.Scan(&group.HubID, &group.GroupID, &group.CircleID, &group.Name, &state, &group.OwnerAgentID, &group.CharterVersion, &hasCharter, &group.ContentHash, &charterUpdated, &created, &archived); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return hub.Group{}, ErrNotFound
		}
		return hub.Group{}, err
	}
	group.State = hub.GroupState(state.String)
	group.HasCharter = hasCharter != 0
	var err error
	if group.CharterUpdatedAt, err = parseNullableTimePtr(charterUpdated); err != nil {
		return hub.Group{}, err
	}
	if group.CreatedAt, err = parseRequiredTime(created); err != nil {
		return hub.Group{}, err
	}
	if group.ArchivedAt, err = parseNullableTimePtr(archived); err != nil {
		return hub.Group{}, err
	}
	return group, nil
}

func scanCharter(row scanner, charter hub.GroupCharter) (hub.GroupCharter, error) {
	var content, updatedBy, createdAt, superseded sql.NullString
	if err := row.Scan(&content, &updatedBy, &createdAt, &superseded); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return hub.GroupCharter{}, ErrNotFound
		}
		return hub.GroupCharter{}, err
	}
	charter.Content, charter.UpdatedBy, charter.CreatedAt = content.String, updatedBy.String, createdAt.String
	if superseded.Valid {
		value := superseded.String
		charter.SupersededAt = &value
	}
	return charter, nil
}

func scanGroupMember(row scanner) (hub.GroupMember, error) {
	var member hub.GroupMember
	var role, state, joined, left, removed sql.NullString
	if err := row.Scan(&member.HubID, &member.GroupID, &member.AgentID, &member.CircleID, &role, &state, &joined, &left, &removed); err != nil {
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
	if err := row.Scan(&invitation.ID, &invitation.HubID, &invitation.GroupID, &invitation.CircleID, &invitation.InviterAgentID,
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
	if err := row.Scan(&message.ID, &message.HubID, &message.GroupID, &message.CircleID, &message.SenderAgentID,
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
