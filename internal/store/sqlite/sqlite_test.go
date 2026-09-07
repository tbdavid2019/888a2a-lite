package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/tbdavid2019/888a2a-lite/internal/hub"

	_ "modernc.org/sqlite"
)

func TestOpenBootstrapsSQLiteWithRequiredPragmas(t *testing.T) {
	database, err := Open(context.Background(), filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatalf("Open returned an error: %v", err)
	}
	defer func() {
		if err := database.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	}()

	var journalMode string
	if err := database.SQL().QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("read journal mode: %v", err)
	}
	if journalMode != "wal" {
		t.Fatalf("journal mode = %q, want wal", journalMode)
	}

	var foreignKeys int
	if err := database.SQL().QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatalf("read foreign keys pragma: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign keys = %d, want 1", foreignKeys)
	}

	for _, table := range []string{"hub_policy", "agent", "inbox_item"} {
		var name string
		if err := database.SQL().QueryRow(
			"SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?", table,
		).Scan(&name); err != nil {
			t.Fatalf("find table %q: %v", table, err)
		}
		if name != table {
			t.Fatalf("table = %q, want %q", name, table)
		}
	}
}

func TestLegacyAgentRegistrationConstraintMigratesToCircleScope(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	database, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	legacySchema := `
CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL);
INSERT INTO schema_migrations (version, applied_at) VALUES (2, '2026-01-01T00:00:00Z');
CREATE TABLE agent (
    hub_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    registration_key_hash TEXT NOT NULL,
    token_hash TEXT NOT NULL,
    display_name TEXT NOT NULL,
    provider_family TEXT NOT NULL,
    transport_id TEXT NOT NULL,
    capabilities_json TEXT NOT NULL,
    agent_card_json TEXT NOT NULL DEFAULT '',
    automatic_execution INTEGER NOT NULL DEFAULT 0,
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
INSERT INTO agent (hub_id, agent_id, registration_key_hash, token_hash, display_name, provider_family, transport_id, capabilities_json, state, expires_at, created_at)
VALUES ('public', 'legacy-agent', 'legacy-registration-hash', 'token-hash', 'legacy', 'test', 'http', '[]', 'ONLINE', '2030-01-01T00:00:00Z', '2026-01-01T00:00:00Z');`
	if _, err := database.Exec(legacySchema); err != nil {
		database.Close()
		t.Fatalf("create legacy schema: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close legacy database: %v", err)
	}

	migrated, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open migrated database: %v", err)
	}
	defer func() { _ = migrated.Close() }()
	repository := NewRepository(migrated)
	if err := repository.CreateCircle(context.Background(), hub.Circle{HubID: "public", CircleID: "circle-private", State: hub.CircleStateActive, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("create private circle: %v", err)
	}
	if err := repository.CreateAgent(context.Background(), hub.RegisteredAgent{
		HubID: "public", AgentID: "private-agent", CircleID: "circle-private",
		RegistrationKeyHash: "legacy-registration-hash", TokenHash: "private-token",
		DisplayName: "private", ProviderFamily: "test", TransportID: "http", Capabilities: []string{},
		State: hub.AgentStateOnline, ExpiresAt: time.Now().UTC().Add(time.Hour), CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("same registration key in another circle should succeed: %v", err)
	}
	agent, err := repository.FindAgent(context.Background(), "legacy-agent")
	if err != nil || agent.CircleID != "public" {
		t.Fatalf("legacy agent circle = %q, err=%v; want public", agent.CircleID, err)
	}
}

func TestSQLiteDSNDoesNotDuplicateQuery(t *testing.T) {
	for _, test := range []struct {
		path string
		want string
	}{
		{path: "/tmp/hub.db", want: "file:/tmp/hub.db?"},
		{path: "file:/tmp/hub.db?cache=shared", want: "file:/tmp/hub.db?cache=shared&"},
		{path: ":memory:", want: ":memory:?"},
	} {
		got := sqliteDSN(test.path)
		if len(got) < len(test.want) || got[:len(test.want)] != test.want {
			t.Fatalf("sqliteDSN(%q) = %q, want prefix %q", test.path, got, test.want)
		}
	}
}
