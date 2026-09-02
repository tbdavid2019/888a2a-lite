package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tbdavid2019/888a2a-lite/internal/hub"
)

func TestCredentialFileIsPrivateAndRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.credentials.json")
	want := credentialFile{
		HubURL: "https://hub.example",
		Identity: hub.AgentIdentity{
			HubID: "public", AgentID: "agent-1", AgentToken: "fixture-agent-token",
		},
	}
	if err := saveCredentials(path, want); err != nil {
		t.Fatalf("saveCredentials: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat credential file: %v", err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("credential file mode = %o, want 600", got)
	}
	client, err := loadClient(path)
	if err != nil {
		t.Fatalf("loadClient: %v", err)
	}
	if client.AgentID != want.Identity.AgentID || client.AgentToken != want.Identity.AgentToken {
		t.Fatalf("loaded identity = %q/%q", client.AgentID, client.AgentToken)
	}
}
