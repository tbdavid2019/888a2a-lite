package a2a

import (
	"fmt"
	"mime"
	"strings"
)

const (
	ReasonContentTypeNotSupported = "CONTENT_TYPE_NOT_SUPPORTED"
	ReasonVersionNotSupported     = "VERSION_NOT_SUPPORTED"
)

func ValidateContentType(value string) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil || (mediaType != MediaType && mediaType != JSONMediaType) {
		return fmt.Errorf("%s: unsupported content type", ReasonContentTypeNotSupported)
	}
	return nil
}

func ValidateVersion(value string) error {
	value = strings.TrimSpace(value)
	if value == "" || value == ProtocolVersion || value == "1.0.0" {
		return nil
	}
	return fmt.Errorf("%s: unsupported A2A version %q", ReasonVersionNotSupported, value)
}

func TextPart(text string) Part {
	return Part{Text: &text, MediaType: "text/plain"}
}

func TextFromParts(parts []Part) (string, error) {
	if len(parts) == 0 {
		return "", fmt.Errorf("%s: message requires at least one part", ReasonContentTypeNotSupported)
	}
	var builder strings.Builder
	for index, part := range parts {
		if part.Text == nil || part.Raw != "" || part.URL != "" || part.Data != nil {
			return "", fmt.Errorf("%s: part %d is not text-only", ReasonContentTypeNotSupported, index)
		}
		if strings.TrimSpace(part.MediaType) != "" && !strings.EqualFold(part.MediaType, "text/plain") {
			return "", fmt.Errorf("%s: part %d has media type %q", ReasonContentTypeNotSupported, index, part.MediaType)
		}
		builder.WriteString(*part.Text)
	}
	if strings.TrimSpace(builder.String()) == "" {
		return "", fmt.Errorf("%s: text must not be empty", ReasonContentTypeNotSupported)
	}
	return builder.String(), nil
}
