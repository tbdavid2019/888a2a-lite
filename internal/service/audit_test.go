package service

import (
	"strings"
	"testing"
)

func TestRedactAuditDetailsRemovesURLQueryCredentials(t *testing.T) {
	details := redactAuditDetails(map[string]any{
		"attachment": "https://box.david888.com/storage/report.pdf?signature=secret-token&expires=999",
		"nested": map[string]any{
			"urls": []any{"https://example.com/file.png?token=another-secret"},
		},
	})
	serialized := details["attachment"].(string) + details["nested"].(map[string]any)["urls"].([]any)[0].(string)
	if strings.Contains(serialized, "secret-token") || strings.Contains(serialized, "another-secret") {
		t.Fatalf("redacted details still contain query credentials: %s", serialized)
	}
	if !strings.Contains(serialized, "redacted") {
		t.Fatalf("redacted details do not preserve the URL indicator: %s", serialized)
	}
}
