package hub

import (
	"strings"
	"testing"
	"time"
)

func TestTokenHashAndVerify(t *testing.T) {
	const token = "agent-token-example"

	hash := HashToken(token)
	if hash == "" {
		t.Fatal("HashToken returned an empty hash")
	}
	if !VerifyToken(hash, token) {
		t.Fatal("VerifyToken rejected the original token")
	}
	if VerifyToken(hash, token+"-wrong") {
		t.Fatal("VerifyToken accepted a different token")
	}
	if strings.Contains(hash, token) {
		t.Fatal("token was included in its stored hash")
	}
}

func TestAgentStateAt(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	base := RegisteredAgent{
		ExpiresAt:      now.Add(time.Hour),
		LeaseExpiresAt: now.Add(time.Minute),
		LastSeenAt:     now.Add(-time.Second),
	}

	if got := base.StateAt(now); got != AgentStateOnline {
		t.Fatalf("online state = %q, want %q", got, AgentStateOnline)
	}
	base.LeaseExpiresAt = now.Add(-time.Second)
	if got := base.StateAt(now); got != AgentStateOffline {
		t.Fatalf("offline state = %q, want %q", got, AgentStateOffline)
	}
	base.ExpiresAt = now.Add(-time.Second)
	if got := base.StateAt(now); got != AgentStateExpired {
		t.Fatalf("expired state = %q, want %q", got, AgentStateExpired)
	}
	base.RevokedAt = timePtr(now.Add(-time.Hour))
	if got := base.StateAt(now); got != AgentStateRevoked {
		t.Fatalf("revoked state = %q, want %q", got, AgentStateRevoked)
	}
}

func TestSafeAgentViewDoesNotExposeSecrets(t *testing.T) {
	agent := RegisteredAgent{
		HubID:          "public",
		AgentID:        "agent-1",
		DisplayName:    "Example",
		ProviderFamily: "codex",
		TransportID:    "http-json",
		Capabilities:   []string{"text/plain"},
		AgentCardJSON:  `{"privateUrl":"http://10.0.0.4","apiKey":"secret"}`,
		TokenHash:      "token-hash-secret",
		ExpiresAt:      time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC),
	}

	view := agent.SafeView("https://hub.example")
	if view.AgentID != agent.AgentID || view.Card.Version != AgentCardVersion {
		t.Fatalf("safe view did not preserve public identity: %+v", view)
	}
	if view.Card.URL == "" || view.Card.URL != "https://hub.example/hub/v1/agents/agent-1/agent-card.json" {
		t.Fatalf("unexpected card URL: %q", view.Card.URL)
	}
	serialized := view.Card.Name + view.Card.ProviderFamily + strings.Join(view.Card.Capabilities, ",")
	if strings.Contains(serialized, "privateUrl") || strings.Contains(serialized, "secret") || strings.Contains(serialized, "token-hash") {
		t.Fatalf("safe view exposed private metadata: %q", serialized)
	}
}

func TestIdempotencyKeyValidation(t *testing.T) {
	if err := ValidateIdempotencyKey("installation-1"); err != nil {
		t.Fatalf("valid key rejected: %v", err)
	}
	for _, key := range []string{"", "   ", strings.Repeat("x", MaxIdempotencyKeyLength+1)} {
		if err := ValidateIdempotencyKey(key); err == nil {
			t.Fatalf("invalid key accepted: %q", key)
		}
	}
}

func timePtr(value time.Time) *time.Time { return &value }
