package a2a

import (
	"encoding/json"
	"fmt"
	"mime"
	"net/url"
	"strings"
)

const (
	ReasonResourceExhausted         = "RESOURCE_EXHAUSTED"
	DefaultMaxURLLength             = 4096
	DefaultMaxFilenameLength        = 255
	DefaultMaxMediaTypeLength       = 127
	DefaultMaxParts                 = 16
	DefaultMaxArtifacts             = 16
	DefaultMaxMetadataBytes   int64 = 64 << 10
)

// AttachmentLimits bounds metadata carried by URL-reference Parts. Inline raw
// bytes and structured data remain disabled by the current standard profile.
type AttachmentLimits struct {
	MaxURLLength       int
	MaxFilenameLength  int
	MaxMediaTypeLength int
	MaxParts           int
	MaxArtifacts       int
	MaxMetadataBytes   int64
}

func DefaultAttachmentLimits() AttachmentLimits {
	return AttachmentLimits{
		MaxURLLength:       DefaultMaxURLLength,
		MaxFilenameLength:  DefaultMaxFilenameLength,
		MaxMediaTypeLength: DefaultMaxMediaTypeLength,
		MaxParts:           DefaultMaxParts,
		MaxArtifacts:       DefaultMaxArtifacts,
		MaxMetadataBytes:   DefaultMaxMetadataBytes,
	}
}

func (limits AttachmentLimits) Normalize() AttachmentLimits {
	defaults := DefaultAttachmentLimits()
	if limits.MaxURLLength == 0 {
		limits.MaxURLLength = defaults.MaxURLLength
	}
	if limits.MaxFilenameLength == 0 {
		limits.MaxFilenameLength = defaults.MaxFilenameLength
	}
	if limits.MaxMediaTypeLength == 0 {
		limits.MaxMediaTypeLength = defaults.MaxMediaTypeLength
	}
	if limits.MaxParts == 0 {
		limits.MaxParts = defaults.MaxParts
	}
	if limits.MaxArtifacts == 0 {
		limits.MaxArtifacts = defaults.MaxArtifacts
	}
	if limits.MaxMetadataBytes == 0 {
		limits.MaxMetadataBytes = defaults.MaxMetadataBytes
	}
	return limits
}

func (limits AttachmentLimits) Validate() error {
	if limits.MaxURLLength < 0 || limits.MaxFilenameLength < 0 || limits.MaxMediaTypeLength < 0 || limits.MaxParts < 0 || limits.MaxArtifacts < 0 || limits.MaxMetadataBytes < 0 {
		return fmt.Errorf("attachment limits must not be negative")
	}
	limits = limits.Normalize()
	if limits.MaxURLLength > 16<<10 || limits.MaxFilenameLength > 1024 || limits.MaxMediaTypeLength > 256 || limits.MaxParts > 64 || limits.MaxArtifacts > 64 || limits.MaxMetadataBytes > 1<<20 {
		return fmt.Errorf("attachment limits exceed safe maximums")
	}
	return nil
}

type ValidationError struct {
	Reason  string
	Message string
}

func (err *ValidationError) Error() string { return err.Message }

func invalidContent(format string, args ...any) error {
	return &ValidationError{Reason: ReasonContentTypeNotSupported, Message: fmt.Sprintf(format, args...)}
}

func exhausted(format string, args ...any) error {
	return &ValidationError{Reason: ReasonResourceExhausted, Message: fmt.Sprintf(format, args...)}
}

// ValidateMessage validates the content profile without performing any
// network request or inspecting a referenced resource.
func ValidateMessage(message Message, limits AttachmentLimits) error {
	limits = limits.Normalize()
	if len(message.Parts) == 0 {
		return invalidContent("%s: message requires at least one part", ReasonContentTypeNotSupported)
	}
	if len(message.Parts) > limits.MaxParts {
		return exhausted("message parts exceed the configured limit of %d", limits.MaxParts)
	}
	metadataBytes, err := json.Marshal(message.Metadata)
	if err != nil {
		return invalidContent("%s: message metadata is invalid", ReasonContentTypeNotSupported)
	}
	if int64(len(metadataBytes)) > limits.MaxMetadataBytes {
		return exhausted("message metadata exceeds the configured limit")
	}
	var totalMetadata = int64(len(metadataBytes))
	for index, part := range message.Parts {
		if err := ValidatePart(part, limits); err != nil {
			return fmt.Errorf("part %d: %w", index, err)
		}
		partMetadata, marshalErr := json.Marshal(part.Metadata)
		if marshalErr != nil {
			return invalidContent("%s: part %d metadata is invalid", ReasonContentTypeNotSupported, index)
		}
		totalMetadata += int64(len(partMetadata))
		if totalMetadata > limits.MaxMetadataBytes {
			return exhausted("attachment metadata exceeds the configured limit")
		}
	}
	return nil
}

// TextForDelivery returns the textual projection retained for legacy inbox
// consumers. URL Parts remain available through the standard Parts field.
func TextForDelivery(parts []Part) string {
	var builder strings.Builder
	for _, part := range parts {
		if part.Text != nil {
			builder.WriteString(*part.Text)
		}
	}
	return builder.String()
}

func ValidateArtifacts(artifacts []Artifact, limits AttachmentLimits) error {
	limits = limits.Normalize()
	if len(artifacts) > limits.MaxArtifacts {
		return exhausted("artifacts exceed the configured limit of %d", limits.MaxArtifacts)
	}
	var totalMetadata int64
	for index, artifact := range artifacts {
		if strings.TrimSpace(artifact.ArtifactID) == "" {
			return invalidContent("%s: artifact %d requires artifactId", ReasonContentTypeNotSupported, index)
		}
		if len(artifact.Parts) == 0 {
			return invalidContent("%s: artifact %d requires at least one part", ReasonContentTypeNotSupported, index)
		}
		if len(artifact.Parts) > limits.MaxParts {
			return exhausted("artifact %d parts exceed the configured limit of %d", index, limits.MaxParts)
		}
		artifactMetadata, err := json.Marshal(artifact.Metadata)
		if err != nil {
			return invalidContent("%s: artifact %d metadata is invalid", ReasonContentTypeNotSupported, index)
		}
		totalMetadata += int64(len(artifactMetadata))
		for partIndex, part := range artifact.Parts {
			if err := ValidatePart(part, limits); err != nil {
				return fmt.Errorf("artifact %d part %d: %w", index, partIndex, err)
			}
			partMetadata, marshalErr := json.Marshal(part.Metadata)
			if marshalErr != nil {
				return invalidContent("%s: artifact %d part %d metadata is invalid", ReasonContentTypeNotSupported, index, partIndex)
			}
			totalMetadata += int64(len(partMetadata))
			if totalMetadata > limits.MaxMetadataBytes {
				return exhausted("artifact metadata exceeds the configured limit")
			}
		}
	}
	return nil
}

func ValidatePart(part Part, limits AttachmentLimits) error {
	limits = limits.Normalize()
	contentCount := 0
	if part.Text != nil {
		contentCount++
	}
	if part.Raw != nil {
		contentCount++
	}
	if part.URL != nil {
		contentCount++
	}
	if len(part.Data) != 0 {
		contentCount++
	}
	if contentCount != 1 {
		return invalidContent("%s: Part must contain exactly one content field", ReasonContentTypeNotSupported)
	}
	if len(part.Filename) > limits.MaxFilenameLength {
		return exhausted("filename exceeds the configured limit of %d bytes", limits.MaxFilenameLength)
	}
	if strings.ContainsAny(part.Filename, "/\\\r\n\x00") {
		return invalidContent("%s: filename contains a forbidden character", ReasonContentTypeNotSupported)
	}
	if len(part.MediaType) > limits.MaxMediaTypeLength {
		return exhausted("mediaType exceeds the configured limit of %d bytes", limits.MaxMediaTypeLength)
	}
	mediaType, _, mediaErr := mime.ParseMediaType(strings.TrimSpace(part.MediaType))
	if part.URL != nil {
		if strings.TrimSpace(*part.URL) == "" {
			return invalidContent("%s: URL must not be empty", ReasonContentTypeNotSupported)
		}
		if len(*part.URL) > limits.MaxURLLength {
			return exhausted("URL exceeds the configured limit of %d bytes", limits.MaxURLLength)
		}
		parsed, err := url.Parse(*part.URL)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" || parsed.Opaque != "" {
			return invalidContent("%s: URL must be an absolute HTTPS URL without userinfo or fragment", ReasonContentTypeNotSupported)
		}
		if strings.TrimSpace(part.MediaType) == "" || mediaErr != nil || !supportedURLMediaType(mediaType) {
			return invalidContent("%s: URL Part requires a supported mediaType", ReasonContentTypeNotSupported)
		}
		return nil
	}
	if part.Raw != nil || len(part.Data) != 0 {
		return invalidContent("%s: raw and data Parts are not supported", ReasonContentTypeNotSupported)
	}
	if part.Text != nil && strings.TrimSpace(part.MediaType) != "" && (mediaErr != nil || mediaType != "text/plain") {
		return invalidContent("%s: text Part must use mediaType text/plain", ReasonContentTypeNotSupported)
	}
	return nil
}

func supportedURLMediaType(mediaType string) bool {
	if strings.HasPrefix(mediaType, "image/") || strings.HasPrefix(mediaType, "audio/") || strings.HasPrefix(mediaType, "video/") {
		return true
	}
	switch mediaType {
	case "text/plain", "text/csv", "application/pdf", "application/octet-stream", "application/zip", "application/gzip", "application/rtf", "application/msword", "application/vnd.ms-excel", "application/vnd.ms-powerpoint", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "application/vnd.openxmlformats-officedocument.presentationml.presentation":
		return true
	default:
		return false
	}
}

func URLAttachmentInputModes() []string {
	return []string{
		"text/plain", "text/csv", "image/*", "audio/*", "video/*", "application/pdf",
		"application/octet-stream", "application/zip", "application/gzip", "application/rtf",
		"application/msword", "application/vnd.ms-excel", "application/vnd.ms-powerpoint",
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		"application/vnd.openxmlformats-officedocument.presentationml.presentation",
	}
}
