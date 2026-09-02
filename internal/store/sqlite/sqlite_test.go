package sqlite

import (
	"context"
	"path/filepath"
	"testing"
)

func TestOpenBootstrapsSQLiteWithRequiredPragmas(t *testing.T) {
	database, err := Open(context.Background(), filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatalf("Open returned an error: %v", err)
	}
	defer database.Close()

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
