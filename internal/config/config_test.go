package config

import (
	"testing"
	"time"
)

func TestLoadUsesSafeAttachmentDefaults(t *testing.T) {
	for _, name := range []string{
		"A2A888_HUB_MAX_ATTACHMENT_URL_LENGTH",
		"A2A888_HUB_MAX_ATTACHMENT_FILENAME_LENGTH",
		"A2A888_HUB_MAX_ATTACHMENT_MEDIA_TYPE_LENGTH",
		"A2A888_HUB_MAX_ATTACHMENT_PARTS",
		"A2A888_HUB_MAX_ATTACHMENT_ARTIFACTS",
		"A2A888_HUB_MAX_ATTACHMENT_METADATA_BYTES",
	} {
		t.Setenv(name, "")
	}
	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.AttachmentLimits.MaxURLLength <= 0 || loaded.AttachmentLimits.MaxParts <= 0 || loaded.AttachmentLimits.MaxMetadataBytes <= 0 {
		t.Fatalf("Load() attachment limits = %+v", loaded.AttachmentLimits)
	}
	if loaded.MaxCircles != DefaultMaxCircles || loaded.EmptyCircleTTL != 24*time.Hour || loaded.IdleAgentTTL != 7*24*time.Hour {
		t.Fatalf("Load() circle/idle defaults = (%d, %v, %v)", loaded.MaxCircles, loaded.EmptyCircleTTL, loaded.IdleAgentTTL)
	}
}

func TestLoadRejectsNegativeCircleOrIdleLimits(t *testing.T) {
	t.Setenv("A2A888_HUB_MAX_CIRCLES", "-1")
	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted negative A2A888_HUB_MAX_CIRCLES")
	}
	t.Setenv("A2A888_HUB_MAX_CIRCLES", "10")
	t.Setenv("A2A888_HUB_EMPTY_CIRCLE_TTL_SECONDS", "-5")
	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted negative A2A888_HUB_EMPTY_CIRCLE_TTL_SECONDS")
	}
}

func TestLoadRejectsUnsafeAttachmentLimit(t *testing.T) {
	t.Setenv("A2A888_HUB_MAX_ATTACHMENT_URL_LENGTH", "17000")
	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted unsafe attachment URL limit")
	}
}

func TestZeroValueConfigValidatesWithAttachmentDefaults(t *testing.T) {
	cfg := Config{HubID: "public", ListenAddr: ":0", DatabasePath: "/tmp/hub.db", RegistrationTTL: DefaultRegistrationTTL, PeerLease: DefaultPeerLease, MaxRegisteredAgents: 1, MaxTasksPerMinute: 1, MaxConcurrentTasks: 1, MaxPayloadBytes: 1 << 20, RegistrationPerMinute: 1}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestLoadUploadEndpointAndDisable(t *testing.T) {
	t.Setenv("A2A888_HUB_UPLOAD_ENDPOINT", "")
	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	norm := loaded.Normalize()
	if norm.UploadEndpoint != DefaultUploadEndpoint || norm.UploadOrigin != "https://box.david888.com" {
		t.Fatalf("unexpected upload config: %+v", norm)
	}

	t.Setenv("A2A888_HUB_UPLOAD_ENDPOINT", "disabled")
	disabled, err := Load()
	if err != nil {
		t.Fatalf("Load() disabled error = %v", err)
	}
	if disabled.Normalize().UploadEndpoint != "" {
		t.Fatalf("expected disabled upload endpoint, got %q", disabled.Normalize().UploadEndpoint)
	}

	t.Setenv("A2A888_HUB_UPLOAD_ENDPOINT", "http://insecure.example/upload")
	if _, err := Load(); err == nil {
		t.Fatal("expected non-https upload endpoint to fail validation")
	}
}
