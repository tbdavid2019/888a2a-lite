package a2a

import (
	"strings"
	"testing"
)

func urlPart(value, mediaType string) Part {
	return Part{URL: &value, MediaType: mediaType, Filename: "asset.bin"}
}

func TestValidateMessageAcceptsTextAndHTTPSURLParts(t *testing.T) {
	text := "inspect this"
	message := Message{MessageID: "message-1", Role: "ROLE_USER", Parts: []Part{
		{Text: &text, MediaType: "text/plain"},
		urlPart("https://box.david888.com/storage/asset.pdf?signature=short-lived", "application/pdf"),
	}}
	if err := ValidateMessage(message, DefaultAttachmentLimits()); err != nil {
		t.Fatalf("ValidateMessage() error = %v", err)
	}
}

func TestValidatePartRejectsUnsafeURLAndInlineContent(t *testing.T) {
	for name, part := range map[string]Part{
		"http":     func() Part { value := "http://example.com/a"; return urlPart(value, "application/pdf") }(),
		"userinfo": func() Part { value := "https://user:pass@example.com/a"; return urlPart(value, "application/pdf") }(),
		"fragment": func() Part { value := "https://example.com/a#page=1"; return urlPart(value, "application/pdf") }(),
		"raw":      func() Part { value := "encoded"; return Part{Raw: &value, MediaType: "image/png"} }(),
		"data":     Part{Data: []byte(`{"kind":"file"}`)},
		"mixed": func() Part {
			text := "hello"
			raw := "encoded"
			return Part{Text: &text, Raw: &raw}
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidatePart(part, DefaultAttachmentLimits()); err == nil {
				t.Fatalf("ValidatePart() accepted %s", name)
			}
		})
	}
}

func TestValidateArtifactsRejectsInvalidPartAtomically(t *testing.T) {
	value := "https://box.david888.com/storage/asset.pdf"
	artifacts := []Artifact{{ArtifactID: "artifact-1", Parts: []Part{{URL: &value, MediaType: "application/x-unknown"}}}}
	if err := ValidateArtifacts(artifacts, DefaultAttachmentLimits()); err == nil {
		t.Fatal("ValidateArtifacts() accepted unsupported media type")
	}
}

func TestURLAttachmentModesCoverAcceptedMediaTypes(t *testing.T) {
	for _, mediaType := range []string{
		"text/plain", "text/csv", "image/png", "audio/mpeg", "video/mp4",
		"application/pdf", "application/octet-stream", "application/zip", "application/gzip",
		"application/rtf", "application/msword", "application/vnd.ms-excel",
		"application/vnd.ms-powerpoint", "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		"application/vnd.openxmlformats-officedocument.presentationml.presentation",
	} {
		advertised := false
		for _, mode := range URLAttachmentInputModes() {
			if mode == mediaType || strings.HasSuffix(mode, "/*") && strings.HasPrefix(mediaType, strings.TrimSuffix(mode, "*")) {
				advertised = true
				break
			}
		}
		if !advertised {
			t.Errorf("accepted MIME type %q is not advertised", mediaType)
		}
		part := urlPart("https://example.com/asset", mediaType)
		if err := ValidatePart(part, DefaultAttachmentLimits()); err != nil {
			t.Errorf("advertised MIME type %q is rejected: %v", mediaType, err)
		}
	}
}

func TestTextPartModeIsAdvertised(t *testing.T) {
	text := "table"
	part := Part{Text: &text, MediaType: "text/plain"}
	if err := ValidatePart(part, DefaultAttachmentLimits()); err != nil {
		t.Fatalf("ValidatePart() error = %v", err)
	}
}

func TestAttachmentLimitsNormalizeAndValidate(t *testing.T) {
	if got := (AttachmentLimits{}).Normalize(); got.MaxParts != DefaultMaxParts || got.MaxURLLength != DefaultMaxURLLength {
		t.Fatalf("Normalize() = %+v", got)
	}
	if err := (AttachmentLimits{MaxURLLength: 17 << 10}).Validate(); err == nil {
		t.Fatal("Validate() accepted unsafe URL limit")
	}
}
