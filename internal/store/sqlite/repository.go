package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/tbdavid2019/888a2a-lite/internal/hub"
	"github.com/tbdavid2019/888a2a-lite/internal/store"
)

var (
	ErrNotFound        = store.ErrNotFound
	ErrUnauthenticated = errors.New("sqlite authentication failed")
	ErrCanceled        = store.ErrCanceled
)

type Repository struct {
	database *DB
	tx       *sql.Tx
}

var _ store.Store = (*Repository)(nil)

func NewRepository(database *DB) *Repository {
	return &Repository{database: database}
}

func (repository *Repository) Agents() store.AgentStore { return repository }

func (repository *Repository) Policy() store.PolicyStore { return repository }

func (repository *Repository) Inbox() store.InboxStore { return repository }

func (repository *Repository) WithTransaction(ctx context.Context, fn func(store.TxStore) error) error {
	if repository.tx != nil {
		return fn(repository)
	}
	tx, err := repository.database.SQL().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin sqlite transaction: %w", err)
	}
	transactional := &Repository{database: repository.database, tx: tx}
	if err := fn(transactional); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit sqlite transaction: %w", err)
	}
	return nil
}

func (repository *Repository) Close() error { return repository.database.Close() }

func (repository *Repository) executor() interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
} {
	if repository.tx != nil {
		return repository.tx
	}
	return repository.database.SQL()
}

func (repository *Repository) withTransaction(ctx context.Context, fn func(*Repository) error) error {
	if repository.tx != nil {
		return fn(repository)
	}
	return repository.WithTransaction(ctx, func(tx store.TxStore) error {
		return fn(tx.(*Repository))
	})
}

func (repository *Repository) CreateAgent(ctx context.Context, agent hub.RegisteredAgent) error {
	if agent.State == "" {
		agent.State = hub.AgentStatePending
	}
	capabilities, err := json.Marshal(agent.Capabilities)
	if err != nil {
		return fmt.Errorf("marshal capabilities: %w", err)
	}
	return repository.withTransaction(ctx, func(tx *Repository) error {
		_, err := tx.executor().ExecContext(ctx, `
INSERT INTO agent (
    hub_id, agent_id, registration_key_hash, token_hash, display_name,
    provider_family, transport_id, capabilities_json, agent_card_json,
    automatic_execution, state, last_seen_at, expires_at, lease_expires_at,
    created_at, revoked_at, revoke_reason
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			agent.HubID, agent.AgentID, agent.RegistrationKeyHash, agent.TokenHash,
			agent.DisplayName, agent.ProviderFamily, agent.TransportID, string(capabilities),
			agent.AgentCardJSON, boolInt(agent.AutomaticExecution), string(agent.State),
			nullTime(agent.LastSeenAt), formatTime(agent.ExpiresAt), nullTime(agent.LeaseExpiresAt),
			formatTime(agent.CreatedAt), nullTimePtr(agent.RevokedAt), agent.RevokeReason,
		)
		return err
	})
}

func (repository *Repository) FindAgent(ctx context.Context, agentID string) (hub.RegisteredAgent, error) {
	return repository.findAgent(ctx, `
SELECT hub_id, agent_id, registration_key_hash, token_hash, display_name,
       provider_family, transport_id, capabilities_json, agent_card_json,
       automatic_execution, state, last_seen_at, expires_at, lease_expires_at,
       created_at, revoked_at, revoke_reason
FROM agent WHERE agent_id = ?`, agentID)
}

func (repository *Repository) FindAgentByRegistrationKey(ctx context.Context, key string) (hub.RegisteredAgent, error) {
	return repository.findAgent(ctx, `
SELECT hub_id, agent_id, registration_key_hash, token_hash, display_name,
       provider_family, transport_id, capabilities_json, agent_card_json,
       automatic_execution, state, last_seen_at, expires_at, lease_expires_at,
       created_at, revoked_at, revoke_reason
FROM agent WHERE registration_key_hash = ?`, hub.HashToken(key))
}

func (repository *Repository) findAgent(ctx context.Context, query string, args ...any) (hub.RegisteredAgent, error) {
	row := repository.executor().QueryRowContext(ctx, query, args...)
	var (
		agent                    hub.RegisteredAgent
		capabilitiesJSON         string
		automaticExecution       int
		state                    string
		lastSeen, expires, lease sql.NullString
		created, revoked         sql.NullString
	)
	err := row.Scan(
		&agent.HubID, &agent.AgentID, &agent.RegistrationKeyHash, &agent.TokenHash,
		&agent.DisplayName, &agent.ProviderFamily, &agent.TransportID, &capabilitiesJSON,
		&agent.AgentCardJSON, &automaticExecution, &state, &lastSeen, &expires, &lease,
		&created, &revoked, &agent.RevokeReason,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return hub.RegisteredAgent{}, ErrNotFound
	}
	if err != nil {
		return hub.RegisteredAgent{}, err
	}
	if err := json.Unmarshal([]byte(capabilitiesJSON), &agent.Capabilities); err != nil {
		return hub.RegisteredAgent{}, fmt.Errorf("unmarshal capabilities: %w", err)
	}
	agent.AutomaticExecution = automaticExecution != 0
	agent.State = hub.AgentState(state)
	if agent.LastSeenAt, err = parseNullableTime(lastSeen); err != nil {
		return hub.RegisteredAgent{}, err
	}
	if agent.ExpiresAt, err = parseRequiredTime(expires); err != nil {
		return hub.RegisteredAgent{}, err
	}
	if agent.LeaseExpiresAt, err = parseNullableTime(lease); err != nil {
		return hub.RegisteredAgent{}, err
	}
	if agent.CreatedAt, err = parseRequiredTime(created); err != nil {
		return hub.RegisteredAgent{}, err
	}
	if revoked.Valid {
		value, parseErr := time.Parse(time.RFC3339Nano, revoked.String)
		if parseErr != nil {
			return hub.RegisteredAgent{}, fmt.Errorf("parse revoked_at: %w", parseErr)
		}
		agent.RevokedAt = &value
	}
	return agent, nil
}

func (repository *Repository) ListAgents(ctx context.Context) ([]hub.RegisteredAgent, error) {
	rows, err := repository.executor().QueryContext(ctx, `
SELECT agent_id FROM agent ORDER BY created_at, agent_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var agentIDs []string
	for rows.Next() {
		var agentID string
		if err := rows.Scan(&agentID); err != nil {
			return nil, err
		}
		agentIDs = append(agentIDs, agentID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	agents := make([]hub.RegisteredAgent, 0, len(agentIDs))
	for _, agentID := range agentIDs {
		agent, err := repository.FindAgent(ctx, agentID)
		if err != nil {
			return nil, err
		}
		agents = append(agents, agent)
	}
	return agents, nil
}

func (repository *Repository) CountAgents(ctx context.Context) (int, error) {
	var count int
	err := repository.executor().QueryRowContext(ctx, "SELECT COUNT(*) FROM agent").Scan(&count)
	return count, err
}

func (repository *Repository) AuthenticateAgent(ctx context.Context, agentID, token string) (hub.RegisteredAgent, error) {
	agent, err := repository.FindAgent(ctx, agentID)
	if err != nil || !hub.VerifyToken(agent.TokenHash, token) {
		return hub.RegisteredAgent{}, ErrUnauthenticated
	}
	return agent, nil
}

func (repository *Repository) HeartbeatAgent(ctx context.Context, agentID string, seenAt, leaseExpiresAt time.Time) (hub.RegisteredAgent, error) {
	result, err := repository.executor().ExecContext(ctx, `
UPDATE agent
SET last_seen_at = ?, lease_expires_at = ?, state = 'ONLINE'
WHERE agent_id = ? AND revoked_at IS NULL AND expires_at > ?`,
		formatTime(seenAt), formatTime(leaseExpiresAt), agentID, formatTime(seenAt))
	if err != nil {
		return hub.RegisteredAgent{}, err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return hub.RegisteredAgent{}, ErrNotFound
	}
	return repository.FindAgent(ctx, agentID)
}

func (repository *Repository) DisconnectAgent(ctx context.Context, agentID string, disconnectedAt time.Time) error {
	result, err := repository.executor().ExecContext(ctx, `
UPDATE agent SET state = 'OFFLINE', lease_expires_at = ?
WHERE agent_id = ? AND revoked_at IS NULL`, formatTime(disconnectedAt), agentID)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return ErrNotFound
	}
	return nil
}

func (repository *Repository) RevokeAgent(ctx context.Context, agentID, reason string, revokedAt time.Time) error {
	result, err := repository.executor().ExecContext(ctx, `
UPDATE agent SET state = 'REVOKED', revoked_at = ?, revoke_reason = ?
WHERE agent_id = ? AND revoked_at IS NULL`, formatTime(revokedAt), reason, agentID)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return ErrNotFound
	}
	return nil
}

func (repository *Repository) GetPolicy(ctx context.Context) (hub.HubPolicy, error) {
	var (
		policy                 hub.HubPolicy
		registrationEnabled    int
		registrationTTLSeconds int64
		peerLeaseSeconds       int64
	)
	err := repository.executor().QueryRowContext(ctx, `
SELECT hub_id, registration_enabled, registration_ttl_seconds, peer_lease_seconds,
       max_registered_agents, max_tasks_per_minute, max_concurrent_tasks, max_payload_bytes
FROM hub_policy ORDER BY updated_at DESC LIMIT 1`).Scan(
		&policy.HubID, &registrationEnabled, &registrationTTLSeconds, &peerLeaseSeconds,
		&policy.MaxRegisteredAgents, &policy.MaxTasksPerMinute, &policy.MaxConcurrentTasks,
		&policy.MaxPayloadBytes)
	if errors.Is(err, sql.ErrNoRows) {
		return hub.HubPolicy{}, ErrNotFound
	}
	if err != nil {
		return hub.HubPolicy{}, err
	}
	policy.RegistrationEnabled = registrationEnabled != 0
	policy.RegistrationTTL = time.Duration(registrationTTLSeconds) * time.Second
	policy.PeerLease = time.Duration(peerLeaseSeconds) * time.Second
	return policy, nil
}

func (repository *Repository) SavePolicy(ctx context.Context, policy hub.HubPolicy) error {
	if strings.TrimSpace(policy.HubID) == "" {
		return errors.New("hub policy requires a hub id")
	}
	_, err := repository.executor().ExecContext(ctx, `
INSERT INTO hub_policy (
    hub_id, registration_enabled, registration_ttl_seconds, peer_lease_seconds,
    max_registered_agents, max_tasks_per_minute, max_concurrent_tasks,
    max_payload_bytes, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (hub_id) DO UPDATE SET
    registration_enabled = excluded.registration_enabled,
    registration_ttl_seconds = excluded.registration_ttl_seconds,
    peer_lease_seconds = excluded.peer_lease_seconds,
    max_registered_agents = excluded.max_registered_agents,
    max_tasks_per_minute = excluded.max_tasks_per_minute,
    max_concurrent_tasks = excluded.max_concurrent_tasks,
    max_payload_bytes = excluded.max_payload_bytes,
    updated_at = excluded.updated_at`,
		policy.HubID, boolInt(policy.RegistrationEnabled), int64(policy.RegistrationTTL/time.Second),
		int64(policy.PeerLease/time.Second), policy.MaxRegisteredAgents, policy.MaxTasksPerMinute,
		policy.MaxConcurrentTasks, policy.MaxPayloadBytes, formatTime(time.Now().UTC()))
	return err
}

func (repository *Repository) SetRegistrationEnabled(ctx context.Context, enabled bool) error {
	result, err := repository.executor().ExecContext(ctx, `
UPDATE hub_policy SET registration_enabled = ?, updated_at = ?`, boolInt(enabled), formatTime(time.Now()))
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (repository *Repository) Enqueue(ctx context.Context, item hub.InboxItem) (hub.InboxItem, bool, error) {
	if item.State == "" {
		item.State = hub.DeliveryStatePending
	}
	returnItem := hub.InboxItem{}
	duplicate := false
	err := repository.withTransaction(ctx, func(tx *Repository) error {
		var err error
		returnItem, duplicate, err = tx.FindByIdempotencyKey(ctx, hub.IdempotencyKey{
			HubID: item.HubID, TargetAgentID: item.TargetAgentID,
			RequesterAgentID: item.RequesterAgentID, Key: item.IdempotencyKey,
		})
		if err == nil {
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		duplicate = false
		if item.CreatedAt.IsZero() {
			item.CreatedAt = time.Now().UTC()
		}
		result, insertErr := tx.executor().ExecContext(ctx, `
INSERT INTO inbox_item (
    hub_id, target_agent_id, requester_agent_id, task_id, context_id,
    idempotency_key, message, state, created_at, acknowledged_at, canceled_at, cancel_reason
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			item.HubID, item.TargetAgentID, item.RequesterAgentID, item.TaskID, item.ContextID,
			item.IdempotencyKey, item.Message, string(item.State), formatTime(item.CreatedAt),
			nullTimePtr(item.AcknowledgedAt), nullTimePtr(item.CanceledAt), "")
		if insertErr != nil {
			return insertErr
		}
		insertedID, err := result.LastInsertId()
		if err != nil {
			return err
		}
		item.Sequence = uint64(insertedID)
		returnItem = item
		return nil
	})
	return returnItem, duplicate, err
}

func (repository *Repository) FindByIdempotencyKey(ctx context.Context, key hub.IdempotencyKey) (hub.InboxItem, bool, error) {
	item, err := repository.findInbox(ctx, `
SELECT sequence, hub_id, target_agent_id, requester_agent_id, task_id, context_id,
       idempotency_key, message, state, created_at, acknowledged_at, canceled_at
FROM inbox_item
WHERE hub_id = ? AND target_agent_id = ? AND requester_agent_id = ? AND idempotency_key = ?`,
		key.HubID, key.TargetAgentID, key.RequesterAgentID, key.Key)
	if errors.Is(err, ErrNotFound) {
		return hub.InboxItem{}, false, ErrNotFound
	}
	return item, err == nil, err
}

func (repository *Repository) Poll(ctx context.Context, targetAgentID string, afterSequence uint64, limit int) ([]hub.InboxItem, error) {
	rows, err := repository.executor().QueryContext(ctx, `
SELECT sequence, hub_id, target_agent_id, requester_agent_id, task_id, context_id,
       idempotency_key, message, state, created_at, acknowledged_at, canceled_at
FROM inbox_item
WHERE target_agent_id = ? AND sequence > ? AND state = 'PENDING'
ORDER BY sequence LIMIT ?`, targetAgentID, afterSequence, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]hub.InboxItem, 0, limit)
	for rows.Next() {
		item, err := scanInbox(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (repository *Repository) findInbox(ctx context.Context, query string, args ...any) (hub.InboxItem, error) {
	return scanInbox(repository.executor().QueryRowContext(ctx, query, args...))
}

func (repository *Repository) Acknowledge(ctx context.Context, targetAgentID string, sequence uint64, acknowledgedAt time.Time) error {
	var state string
	err := repository.executor().QueryRowContext(ctx,
		"SELECT state FROM inbox_item WHERE target_agent_id = ? AND sequence = ?", targetAgentID, sequence).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if state == string(hub.DeliveryStateAcknowledged) {
		return nil
	}
	if state == string(hub.DeliveryStateCanceled) {
		return ErrCanceled
	}
	result, err := repository.executor().ExecContext(ctx, `
UPDATE inbox_item SET state = 'ACKNOWLEDGED', acknowledged_at = ?
WHERE target_agent_id = ? AND sequence = ? AND state = 'PENDING'`,
		formatTime(acknowledgedAt), targetAgentID, sequence)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return ErrNotFound
	}
	return nil
}

func (repository *Repository) CancelTask(ctx context.Context, taskID, reason string, canceledAt time.Time) error {
	result, err := repository.executor().ExecContext(ctx, `
UPDATE inbox_item SET state = 'CANCELED', canceled_at = ?, cancel_reason = ?
WHERE task_id = ? AND state = 'PENDING'`, formatTime(canceledAt), reason, taskID)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return ErrNotFound
	}
	return nil
}

func (repository *Repository) PendingCount(ctx context.Context, targetAgentID string) (int, error) {
	var count int
	err := repository.executor().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM inbox_item WHERE (? = '' OR target_agent_id = ?) AND state = 'PENDING'", targetAgentID, targetAgentID).Scan(&count)
	return count, err
}

type scanner interface {
	Scan(...any) error
}

func scanInbox(row scanner) (hub.InboxItem, error) {
	var (
		item                                   hub.InboxItem
		state, created, acknowledged, canceled sql.NullString
	)
	err := row.Scan(
		&item.Sequence, &item.HubID, &item.TargetAgentID, &item.RequesterAgentID,
		&item.TaskID, &item.ContextID, &item.IdempotencyKey, &item.Message, &state,
		&created, &acknowledged, &canceled)
	if errors.Is(err, sql.ErrNoRows) {
		return hub.InboxItem{}, ErrNotFound
	}
	if err != nil {
		return hub.InboxItem{}, err
	}
	item.State = hub.DeliveryState(state.String)
	var parseErr error
	if item.CreatedAt, parseErr = parseRequiredTime(created); parseErr != nil {
		return hub.InboxItem{}, parseErr
	}
	if item.AcknowledgedAt, parseErr = parseNullableTimePtr(acknowledged); parseErr != nil {
		return hub.InboxItem{}, parseErr
	}
	if item.CanceledAt, parseErr = parseNullableTimePtr(canceled); parseErr != nil {
		return hub.InboxItem{}, parseErr
	}
	return item, nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func nullTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return formatTime(value)
}

func nullTimePtr(value *time.Time) any {
	if value == nil {
		return nil
	}
	return formatTime(*value)
}

func parseRequiredTime(value sql.NullString) (time.Time, error) {
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return time.Time{}, errors.New("required sqlite timestamp is missing")
	}
	return time.Parse(time.RFC3339Nano, value.String)
}

func parseNullableTime(value sql.NullString) (time.Time, error) {
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339Nano, value.String)
}

func parseNullableTimePtr(value sql.NullString) (*time.Time, error) {
	parsed, err := parseNullableTime(value)
	if err != nil || parsed.IsZero() {
		return nil, err
	}
	return &parsed, nil
}
