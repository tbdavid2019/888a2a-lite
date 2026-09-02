package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const defaultBusyTimeout = 5000

type DB struct {
	db *sql.DB
}

func Open(ctx context.Context, path string) (*DB, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("sqlite path must not be empty")
	}
	dsn := sqliteDSN(path)
	database, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	database.SetConnMaxLifetime(0)

	opened := &DB{db: database}
	if err := database.PingContext(ctx); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("ping sqlite database: %w", err)
	}
	if err := opened.migrate(ctx); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("migrate sqlite database: %w", err)
	}
	return opened, nil
}

func (database *DB) SQL() *sql.DB { return database.db }

func (database *DB) Close() error {
	if database == nil || database.db == nil {
		return nil
	}
	return database.db.Close()
}

func sqliteDSN(path string) string {
	if path == ":memory:" || strings.HasPrefix(path, "file:") {
		return path + sqliteQuerySeparator(path) + sqliteQuery()
	}
	return "file:" + path + "?" + sqliteQuery()
}

func sqliteQuerySeparator(path string) string {
	if strings.Contains(path, "?") {
		return "&"
	}
	return "?"
}

func sqliteQuery() string {
	return fmt.Sprintf("_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)&_busy_timeout=%d", defaultBusyTimeout)
}

func (database *DB) migrate(ctx context.Context) error {
	_, err := database.db.ExecContext(ctx, schemaSQL)
	return err
}

const schemaSQL = `
CREATE TABLE IF NOT EXISTS hub_policy (
    hub_id TEXT PRIMARY KEY NOT NULL,
    registration_enabled INTEGER NOT NULL CHECK (registration_enabled IN (0, 1)),
    registration_ttl_seconds INTEGER NOT NULL,
    peer_lease_seconds INTEGER NOT NULL,
    max_registered_agents INTEGER NOT NULL,
    max_tasks_per_minute INTEGER NOT NULL,
    max_concurrent_tasks INTEGER NOT NULL,
    max_payload_bytes INTEGER NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS agent (
    hub_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    registration_key_hash TEXT NOT NULL,
    token_hash TEXT NOT NULL,
    display_name TEXT NOT NULL,
    provider_family TEXT NOT NULL,
    transport_id TEXT NOT NULL,
    capabilities_json TEXT NOT NULL,
    agent_card_json TEXT NOT NULL DEFAULT '',
    automatic_execution INTEGER NOT NULL DEFAULT 0 CHECK (automatic_execution IN (0, 1)),
    state TEXT NOT NULL,
    last_seen_at TEXT,
    expires_at TEXT NOT NULL,
    lease_expires_at TEXT,
    created_at TEXT NOT NULL,
    revoked_at TEXT,
    revoke_reason TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (hub_id, agent_id),
    UNIQUE (hub_id, registration_key_hash)
);

CREATE INDEX IF NOT EXISTS idx_agent_hub_state
    ON agent (hub_id, state);

CREATE TABLE IF NOT EXISTS inbox_item (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT,
    hub_id TEXT NOT NULL,
    target_agent_id TEXT NOT NULL,
    requester_agent_id TEXT NOT NULL,
    task_id TEXT NOT NULL,
    context_id TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    message TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('PENDING', 'ACKNOWLEDGED', 'CANCELED')),
    created_at TEXT NOT NULL,
    acknowledged_at TEXT,
    canceled_at TEXT,
    cancel_reason TEXT NOT NULL DEFAULT '',
    UNIQUE (hub_id, target_agent_id, requester_agent_id, idempotency_key),
    FOREIGN KEY (hub_id, target_agent_id) REFERENCES agent (hub_id, agent_id),
    FOREIGN KEY (hub_id, requester_agent_id) REFERENCES agent (hub_id, agent_id)
);

CREATE INDEX IF NOT EXISTS idx_inbox_pending
    ON inbox_item (hub_id, target_agent_id, state, sequence);

CREATE TABLE IF NOT EXISTS event_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    hub_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    actor_agent_id TEXT NOT NULL DEFAULT '',
    target_agent_id TEXT NOT NULL DEFAULT '',
    task_id TEXT NOT NULL DEFAULT '',
    details_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_event_log_hub_id
    ON event_log (hub_id, id);
`

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}
