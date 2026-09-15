package config

import "testing"

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
