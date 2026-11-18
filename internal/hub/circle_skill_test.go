package hub

import (
	"strings"
	"testing"
)

func TestValidateCircleSkillSlug(t *testing.T) {
	for _, valid := range []string{"a", "deploy-playbook", "code_review_v2", "09-test"} {
		if err := ValidateCircleSkillSlug(valid); err != nil {
			t.Fatalf("expected valid slug %q, got error: %v", valid, err)
		}
	}
	for _, invalid := range []string{
		"",
		"../secret",
		"foo/bar",
		`foo\bar`,
		"UpperCase",
		"-leading-dash",
		"has space",
		strings.Repeat("a", 65),
	} {
		if err := ValidateCircleSkillSlug(invalid); err == nil {
			t.Fatalf("expected error for invalid slug %q", invalid)
		}
	}
}

func TestValidateCircleSkillTTL(t *testing.T) {
	zero := int64(0)
	if err := ValidateCircleSkillTTL(nil); err != nil {
		t.Fatalf("nil TTL should be valid: %v", err)
	}
	if err := ValidateCircleSkillTTL(&zero); err != nil {
		t.Fatalf("zero TTL should be valid: %v", err)
	}
	okTTL := int64(3600)
	if err := ValidateCircleSkillTTL(&okTTL); err != nil {
		t.Fatalf("3600s TTL should be valid: %v", err)
	}
	for _, bad := range []int64{-1, 30, MaxCircleSkillTTLSeconds + 1} {
		v := bad
		if err := ValidateCircleSkillTTL(&v); err == nil {
			t.Fatalf("expected error for out-of-range TTL %d", bad)
		}
	}
}

func TestValidateCircleSkillContentPositiveAndNegative(t *testing.T) {
	validDoc := `---
name: deploy-playbook
description: Safe deployment checklist with API links
tags:
  - ops
  - release
---
# Deploy Playbook

See [API Docs](https://docs.example.com/api#deploy) for details.

` + "```bash\n" + `curl -H "Authorization: Bearer <YOUR_TOKEN>" https://a2a.david888.com/hub/v1/status
echo "<placeholder-tag>"
` + "```"

	title, summary, err := ValidateCircleSkillContent(validDoc)
	if err != nil {
		t.Fatalf("expected valid SKILL.md to pass, got: %v", err)
	}
	if title != "deploy-playbook" {
		t.Fatalf("title = %q, want deploy-playbook", title)
	}
	if summary != "Safe deployment checklist with API links" {
		t.Fatalf("summary = %q, want Safe deployment checklist with API links", summary)
	}

	invalidCases := []struct {
		name    string
		content string
	}{
		{"empty", "   \n  "},
		{"oversized", strings.Repeat("a", MaxCircleSkillBytes+1)},
		{"raw_html", "# Title\n<script>alert(1)</script>"},
		{"event_handler", "# Title\n<div onclick=alert(1)>bad</div>"},
		{"js_ref_link", "# Title\nClick [here][bad]\n\n[bad]: javascript:alert(1)"},
		{"data_uri_image", "# Title\n![img](data:image/svg+xml;base64,PHN2Zz4=)"},
		{"http_link", "# Title\n[plain](http://insecure.example.com)"},
		{"traversal_link", "# Title\n[escape](../etc/passwd)"},
		{"real_secret_outside_fence", "# Title\napi_key: secret-production-value-12345"},
		{"real_secret_inside_fence", "```\nsk-1234567890abcdefghij123456\n```"},
		{"yaml_tag", "---\nname: !!python/object:os.system {}\n---\n# Hi"},
		{"yaml_disallowed_key", "---\nexec_cmd: rm -rf /\n---\n# Hi"},
		{"unclosed_frontmatter", "---\nname: test\n# Hi"},
		{"charter_remains_strict", ""},
	}
	for _, tc := range invalidCases {
		if tc.name == "charter_remains_strict" {
			if err := ValidateGroupCharter("See [link](https://example.com)"); err == nil {
				t.Fatal("ValidateGroupCharter must continue rejecting external markdown links")
			}
			continue
		}
		if _, _, err := ValidateCircleSkillContent(tc.content); err == nil {
			t.Fatalf("case %s: expected validation error, got nil", tc.name)
		}
	}
}
