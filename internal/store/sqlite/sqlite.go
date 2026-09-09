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

const (
	MaxGroupMembersDefault     = 32
	MaxGroupFanoutDefault      = 32
	MaxGroupHistoryPageDefault = 100
)

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
	if _, err := database.db.ExecContext(ctx, schemaSQL); err != nil {
		return err
	}
	for _, column := range []struct {
		table string
		name  string
		ddl   string
	}{
		{table: "hub_policy", name: "max_group_members", ddl: "INTEGER NOT NULL DEFAULT 32"},
		{table: "hub_policy", name: "max_group_fanout", ddl: "INTEGER NOT NULL DEFAULT 32"},
		{table: "hub_policy", name: "max_group_history_page", ddl: "INTEGER NOT NULL DEFAULT 100"},
		{table: "inbox_item", name: "group_id", ddl: "TEXT NOT NULL DEFAULT ''"},
		{table: "inbox_item", name: "group_message_id", ddl: "INTEGER NOT NULL DEFAULT 0"},
		{table: "agent", name: "circle_id", ddl: "TEXT NOT NULL DEFAULT 'public'"},
		{table: "inbox_item", name: "circle_id", ddl: "TEXT NOT NULL DEFAULT 'public'"},
		{table: "agent_group", name: "circle_id", ddl: "TEXT NOT NULL DEFAULT 'public'"},
		{table: "group_member", name: "circle_id", ddl: "TEXT NOT NULL DEFAULT 'public'"},
		{table: "group_invitation", name: "circle_id", ddl: "TEXT NOT NULL DEFAULT 'public'"},
		{table: "group_message", name: "circle_id", ddl: "TEXT NOT NULL DEFAULT 'public'"},
		{table: "group_delivery", name: "circle_id", ddl: "TEXT NOT NULL DEFAULT 'public'"},
		{table: "event_log", name: "circle_id", ddl: "TEXT NOT NULL DEFAULT 'public'"},
	} {
		if err := ensureColumn(ctx, database.db, column.table, column.name, column.ddl); err != nil {
			return err
		}
	}
	if err := migrateAgentRegistrationScope(ctx, database.db); err != nil {
		return err
	}
	if err := migrateStandardA2A(ctx, database.db); err != nil {
		return err
	}
	if err := migrateGroupA2A(ctx, database.db); err != nil {
		return err
	}
	for _, column := range []struct{ table, name, ddl string }{
		{table: "inbox_item", name: "protocol", ddl: "TEXT NOT NULL DEFAULT ''"},
		{table: "inbox_item", name: "message_id", ddl: "TEXT NOT NULL DEFAULT ''"},
		{table: "inbox_item", name: "turn_id", ddl: "TEXT NOT NULL DEFAULT ''"},
		{table: "inbox_item", name: "task_revision", ddl: "INTEGER NOT NULL DEFAULT 0"},
		{table: "inbox_item", name: "parent_task_id", ddl: "TEXT NOT NULL DEFAULT ''"},
		{table: "inbox_item", name: "member_task_id", ddl: "TEXT NOT NULL DEFAULT ''"},
		{table: "inbox_item", name: "reply_policy", ddl: "TEXT NOT NULL DEFAULT ''"},
		{table: "inbox_item", name: "mentions_json", ddl: "TEXT NOT NULL DEFAULT '[]'"},
	} {
		if err := ensureColumn(ctx, database.db, column.table, column.name, column.ddl); err != nil {
			return err
		}
	}
	if _, err := database.db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_inbox_group_message
ON inbox_item (hub_id, group_id, group_message_id, target_agent_id, state)`); err != nil {
		return err
	}
	for _, statement := range []string{
		`CREATE INDEX IF NOT EXISTS idx_agent_circle_state ON agent (hub_id, circle_id, state)`,
		`CREATE INDEX IF NOT EXISTS idx_inbox_circle_state ON inbox_item (hub_id, circle_id, target_agent_id, state, sequence)`,
		`CREATE INDEX IF NOT EXISTS idx_group_circle_state ON agent_group (hub_id, circle_id, state, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_event_circle_id ON event_log (hub_id, circle_id, id)`,
	} {
		if _, err := database.db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	_, err := database.db.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations (version, applied_at) VALUES (3, CURRENT_TIMESTAMP)`)
	return err
}

func migrateStandardA2A(ctx context.Context, database *sql.DB) error {
	var applied int
	if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version = 5").Scan(&applied); err != nil {
		return err
	}
	if applied > 0 {
		return nil
	}
	statements := []string{
		`CREATE UNIQUE INDEX idx_agent_token_hash ON agent (hub_id, token_hash)`,
		`CREATE TABLE a2a_task (
    hub_id TEXT NOT NULL,
    task_id TEXT NOT NULL,
    circle_id TEXT NOT NULL,
    requester_agent_id TEXT NOT NULL,
    target_agent_id TEXT NOT NULL,
    context_id TEXT NOT NULL,
    message_id TEXT NOT NULL,
    turn_id TEXT NOT NULL,
    revision INTEGER NOT NULL,
    state TEXT NOT NULL,
    message_json TEXT NOT NULL,
    history_json TEXT NOT NULL,
    result_message_json TEXT NOT NULL DEFAULT '',
    artifacts_json TEXT NOT NULL,
    mailbox_sequence INTEGER NOT NULL DEFAULT 0,
    content_digest TEXT NOT NULL,
    execution_deadline TEXT,
    retry_budget INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (hub_id, task_id),
    UNIQUE (hub_id, circle_id, requester_agent_id, target_agent_id, message_id)
)`,
		`CREATE INDEX idx_a2a_task_requester ON a2a_task (hub_id, circle_id, requester_agent_id, updated_at, task_id)`,
		`CREATE INDEX idx_a2a_task_context ON a2a_task (hub_id, circle_id, requester_agent_id, context_id, updated_at)`,
		`CREATE TABLE a2a_task_event (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    hub_id TEXT NOT NULL,
    task_id TEXT NOT NULL,
    circle_id TEXT NOT NULL,
    revision INTEGER NOT NULL,
    event_type TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE (hub_id, task_id, revision)
)`,
		`CREATE INDEX idx_a2a_task_event_task ON a2a_task_event (hub_id, task_id, revision)`,
		`CREATE TABLE a2a_task_update (
    hub_id TEXT NOT NULL,
    task_id TEXT NOT NULL,
    update_id TEXT NOT NULL,
    content_digest TEXT NOT NULL,
    task_json TEXT NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY (hub_id, task_id, update_id)
)`,
		`INSERT INTO schema_migrations (version, applied_at) VALUES (5, CURRENT_TIMESTAMP)`,
	}
	for _, statement := range statements {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("migrate standard A2A schema: %w", err)
		}
	}
	return nil
}

func migrateGroupA2A(ctx context.Context, database *sql.DB) error {
	var applied int
	if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version = 6").Scan(&applied); err != nil {
		return err
	}
	if applied > 0 {
		return nil
	}
	statements := []string{
		`CREATE TABLE a2a_group_task_member (
    hub_id TEXT NOT NULL,
    circle_id TEXT NOT NULL,
    group_id TEXT NOT NULL,
    parent_task_id TEXT NOT NULL,
    member_task_id TEXT NOT NULL,
    target_agent_id TEXT NOT NULL,
    reply_policy TEXT NOT NULL,
    mentions_json TEXT NOT NULL,
    ordinal INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY (hub_id, parent_task_id, member_task_id),
    UNIQUE (hub_id, parent_task_id, target_agent_id)
)`,
		`CREATE INDEX idx_a2a_group_task_parent ON a2a_group_task_member (hub_id, circle_id, group_id, parent_task_id, ordinal)`,
		`CREATE INDEX idx_a2a_group_task_member ON a2a_group_task_member (hub_id, member_task_id, target_agent_id)`,
		`INSERT INTO schema_migrations (version, applied_at) VALUES (6, CURRENT_TIMESTAMP)`,
	}
	for _, statement := range statements {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("migrate group A2A schema: %w", err)
		}
	}
	return nil
}

func ensureColumn(ctx context.Context, database *sql.DB, table, column, definition string) error {
	rows, err := database.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		if name == column {
			return rows.Err()
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = database.ExecContext(ctx, "ALTER TABLE "+table+" ADD COLUMN "+column+" "+definition)
	return err
}

func migrateAgentRegistrationScope(ctx context.Context, database *sql.DB) error {
	var applied int
	if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version = 4").Scan(&applied); err != nil {
		return err
	}
	if applied > 0 {
		return nil
	}
	conn, err := database.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
		return err
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	rollback := func(cause error) error {
		_ = tx.Rollback()
		_, _ = conn.ExecContext(ctx, "PRAGMA foreign_keys = ON")
		return cause
	}
	statements := []string{
		`CREATE TABLE agent_v4 (
    hub_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    circle_id TEXT NOT NULL DEFAULT 'public',
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
    PRIMARY KEY (hub_id, agent_id)
)`,
		`INSERT INTO agent_v4 (
    hub_id, agent_id, circle_id, registration_key_hash, token_hash, display_name,
    provider_family, transport_id, capabilities_json, agent_card_json,
    automatic_execution, state, last_seen_at, expires_at, lease_expires_at,
    created_at, revoked_at, revoke_reason
)
SELECT hub_id, agent_id, circle_id, registration_key_hash, token_hash, display_name,
       provider_family, transport_id, capabilities_json, agent_card_json,
       automatic_execution, state, last_seen_at, expires_at, lease_expires_at,
       created_at, revoked_at, revoke_reason
FROM agent`,
		`DROP TABLE agent`,
		`ALTER TABLE agent_v4 RENAME TO agent`,
		`CREATE INDEX IF NOT EXISTS idx_agent_hub_state ON agent (hub_id, state)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_registration_circle ON agent (hub_id, circle_id, registration_key_hash)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_circle_state ON agent (hub_id, circle_id, state)`,
		`INSERT INTO schema_migrations (version, applied_at) VALUES (4, CURRENT_TIMESTAMP)`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return rollback(fmt.Errorf("migrate agent registration scope: %w", err))
		}
	}
	if err := tx.Commit(); err != nil {
		_, _ = conn.ExecContext(ctx, "PRAGMA foreign_keys = ON")
		return err
	}
	_, err = conn.ExecContext(ctx, "PRAGMA foreign_keys = ON")
	return err
}

const schemaSQL = `
CREATE TABLE IF NOT EXISTS hub_circle (
    hub_id TEXT NOT NULL,
    circle_id TEXT NOT NULL,
    alias TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL CHECK (state IN ('ACTIVE', 'DISABLED')),
    active_key_version INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    disabled_at TEXT,
    PRIMARY KEY (hub_id, circle_id)
);

CREATE TABLE IF NOT EXISTS hub_circle_key (
    hub_id TEXT NOT NULL,
    circle_id TEXT NOT NULL,
    version INTEGER NOT NULL,
    key_digest TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('ACTIVE', 'GRACE', 'REVOKED')),
    created_at TEXT NOT NULL,
    grace_until TEXT,
    revoked_at TEXT,
    PRIMARY KEY (hub_id, circle_id, version),
    UNIQUE (hub_id, key_digest),
    FOREIGN KEY (hub_id, circle_id) REFERENCES hub_circle (hub_id, circle_id)
);

CREATE INDEX IF NOT EXISTS idx_circle_state ON hub_circle (hub_id, state, circle_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_circle_alias
    ON hub_circle (hub_id, alias) WHERE alias <> '';
CREATE INDEX IF NOT EXISTS idx_circle_key_digest ON hub_circle_key (hub_id, key_digest, state);

CREATE TABLE IF NOT EXISTS hub_policy (
    hub_id TEXT PRIMARY KEY NOT NULL,
    registration_enabled INTEGER NOT NULL CHECK (registration_enabled IN (0, 1)),
    registration_ttl_seconds INTEGER NOT NULL,
    peer_lease_seconds INTEGER NOT NULL,
    max_registered_agents INTEGER NOT NULL,
    max_tasks_per_minute INTEGER NOT NULL,
    max_concurrent_tasks INTEGER NOT NULL,
    max_payload_bytes INTEGER NOT NULL,
    max_group_members INTEGER NOT NULL DEFAULT 32,
    max_group_fanout INTEGER NOT NULL DEFAULT 32,
    max_group_history_page INTEGER NOT NULL DEFAULT 100,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS agent (
    hub_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    circle_id TEXT NOT NULL DEFAULT 'public',
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
    PRIMARY KEY (hub_id, agent_id)
);

CREATE INDEX IF NOT EXISTS idx_agent_hub_state
    ON agent (hub_id, state);

CREATE TABLE IF NOT EXISTS inbox_item (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT,
    hub_id TEXT NOT NULL,
    circle_id TEXT NOT NULL DEFAULT 'public',
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
    group_id TEXT NOT NULL DEFAULT '',
    group_message_id INTEGER NOT NULL DEFAULT 0,
    protocol TEXT NOT NULL DEFAULT '',
    message_id TEXT NOT NULL DEFAULT '',
    turn_id TEXT NOT NULL DEFAULT '',
    task_revision INTEGER NOT NULL DEFAULT 0,
    parent_task_id TEXT NOT NULL DEFAULT '',
    member_task_id TEXT NOT NULL DEFAULT '',
    reply_policy TEXT NOT NULL DEFAULT '',
    mentions_json TEXT NOT NULL DEFAULT '[]',
    UNIQUE (hub_id, target_agent_id, requester_agent_id, idempotency_key),
    FOREIGN KEY (hub_id, target_agent_id) REFERENCES agent (hub_id, agent_id),
    FOREIGN KEY (hub_id, requester_agent_id) REFERENCES agent (hub_id, agent_id)
);

CREATE INDEX IF NOT EXISTS idx_inbox_pending
    ON inbox_item (hub_id, target_agent_id, state, sequence);

CREATE TABLE IF NOT EXISTS event_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    hub_id TEXT NOT NULL,
    circle_id TEXT NOT NULL DEFAULT 'public',
    event_type TEXT NOT NULL,
    actor_agent_id TEXT NOT NULL DEFAULT '',
    target_agent_id TEXT NOT NULL DEFAULT '',
    task_id TEXT NOT NULL DEFAULT '',
    details_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_event_log_hub_id
    ON event_log (hub_id, id);

CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS announcement (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    hub_id TEXT NOT NULL,
    revision_of_id INTEGER,
    revision INTEGER NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('DRAFT', 'PUBLISHED', 'EXPIRED')),
    severity TEXT NOT NULL CHECK (severity IN ('INFO', 'WARNING', 'CRITICAL')),
    title TEXT NOT NULL,
    summary TEXT NOT NULL,
    documentation_url TEXT NOT NULL DEFAULT '',
    published_at TEXT,
    expires_at TEXT,
    created_at TEXT NOT NULL,
    FOREIGN KEY (revision_of_id) REFERENCES announcement (id)
);

CREATE INDEX IF NOT EXISTS idx_announcement_active
    ON announcement (hub_id, status, id);

CREATE TABLE IF NOT EXISTS agent_group (
    hub_id TEXT NOT NULL,
    group_id TEXT NOT NULL,
    circle_id TEXT NOT NULL DEFAULT 'public',
    name TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('ACTIVE', 'ARCHIVED')),
    owner_agent_id TEXT NOT NULL,
    created_at TEXT NOT NULL,
    archived_at TEXT,
    PRIMARY KEY (hub_id, group_id),
    FOREIGN KEY (hub_id, owner_agent_id) REFERENCES agent (hub_id, agent_id)
);

CREATE INDEX IF NOT EXISTS idx_agent_group_owner
    ON agent_group (hub_id, owner_agent_id, state, created_at);

CREATE TABLE IF NOT EXISTS group_member (
    hub_id TEXT NOT NULL,
    group_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    circle_id TEXT NOT NULL DEFAULT 'public',
    role TEXT NOT NULL CHECK (role IN ('OWNER', 'ADMIN', 'MEMBER')),
    state TEXT NOT NULL CHECK (state IN ('ACTIVE', 'LEFT', 'REMOVED')),
    joined_at TEXT NOT NULL,
    left_at TEXT,
    removed_at TEXT,
    PRIMARY KEY (hub_id, group_id, agent_id),
    FOREIGN KEY (hub_id, group_id) REFERENCES agent_group (hub_id, group_id),
    FOREIGN KEY (hub_id, agent_id) REFERENCES agent (hub_id, agent_id)
);

CREATE INDEX IF NOT EXISTS idx_group_member_agent
    ON group_member (hub_id, agent_id, state, group_id);

CREATE TABLE IF NOT EXISTS group_invitation (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    hub_id TEXT NOT NULL,
    group_id TEXT NOT NULL,
    circle_id TEXT NOT NULL DEFAULT 'public',
    inviter_agent_id TEXT NOT NULL,
    invitee_agent_id TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('PENDING', 'ACCEPTED', 'DECLINED', 'EXPIRED', 'REVOKED')),
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    responded_at TEXT,
    FOREIGN KEY (hub_id, group_id) REFERENCES agent_group (hub_id, group_id),
    FOREIGN KEY (hub_id, inviter_agent_id) REFERENCES agent (hub_id, agent_id),
    FOREIGN KEY (hub_id, invitee_agent_id) REFERENCES agent (hub_id, agent_id)
);

CREATE INDEX IF NOT EXISTS idx_group_invitation_recipient
    ON group_invitation (hub_id, invitee_agent_id, state, expires_at);

CREATE UNIQUE INDEX IF NOT EXISTS idx_group_invitation_pending
    ON group_invitation (hub_id, group_id, invitee_agent_id) WHERE state = 'PENDING';

CREATE TABLE IF NOT EXISTS group_message (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    hub_id TEXT NOT NULL,
    group_id TEXT NOT NULL,
    circle_id TEXT NOT NULL DEFAULT 'public',
    sender_agent_id TEXT NOT NULL,
    context_id TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    message TEXT NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE (hub_id, group_id, sender_agent_id, idempotency_key),
    FOREIGN KEY (hub_id, group_id) REFERENCES agent_group (hub_id, group_id),
    FOREIGN KEY (hub_id, sender_agent_id) REFERENCES agent (hub_id, agent_id)
);

CREATE INDEX IF NOT EXISTS idx_group_message_history
    ON group_message (hub_id, group_id, id);

CREATE TABLE IF NOT EXISTS group_delivery (
    sequence INTEGER PRIMARY KEY,
    hub_id TEXT NOT NULL,
    group_id TEXT NOT NULL,
    circle_id TEXT NOT NULL DEFAULT 'public',
    group_message_id INTEGER NOT NULL,
    target_agent_id TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('PENDING', 'ACKNOWLEDGED', 'CANCELED')),
    polled_at TEXT,
    acknowledged_at TEXT,
    canceled_at TEXT,
    FOREIGN KEY (sequence) REFERENCES inbox_item (sequence),
    FOREIGN KEY (hub_id, group_id) REFERENCES agent_group (hub_id, group_id),
    FOREIGN KEY (group_message_id) REFERENCES group_message (id),
    FOREIGN KEY (hub_id, target_agent_id) REFERENCES agent (hub_id, agent_id)
);

CREATE INDEX IF NOT EXISTS idx_group_delivery_target
    ON group_delivery (hub_id, target_agent_id, state, sequence);

INSERT OR IGNORE INTO schema_migrations (version, applied_at)
VALUES (2, CURRENT_TIMESTAMP);
`

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}
