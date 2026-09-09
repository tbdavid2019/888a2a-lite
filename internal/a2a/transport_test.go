package a2a

import "testing"

func TestValidateContentTypeAcceptsA2AAndJSON(t *testing.T) {
	for _, contentType := range []string{MediaType, JSONMediaType, MediaType + "; charset=utf-8"} {
		if err := ValidateContentType(contentType); err != nil {
			t.Fatalf("ValidateContentType(%q): %v", contentType, err)
		}
	}
}

func TestValidateVersionRejectsUnsupportedVersion(t *testing.T) {
	if err := ValidateVersion("2.0"); err == nil || err.Error()[:len(ReasonVersionNotSupported)] != ReasonVersionNotSupported {
		t.Fatalf("ValidateVersion did not return %s: %v", ReasonVersionNotSupported, err)
	}
}

func TestTextFromPartsRejectsMixedContent(t *testing.T) {
	text := "hello"
	if _, err := TextFromParts([]Part{{Text: &text}, {URL: "https://example.invalid/file"}}); err == nil {
		t.Fatal("TextFromParts accepted a mixed text and URL message")
	}
}

func TestA2AJSONFieldNamesAreCamelCase(t *testing.T) {
	if got := (AgentInterface{}).ProtocolBinding; got != "" {
		t.Fatalf("zero AgentInterface protocol binding = %q", got)
	}
	if TextPart("hello").MediaType != "text/plain" {
		t.Fatal("TextPart did not set the text media type")
	}
}
