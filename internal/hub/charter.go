package hub

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

const MaxGroupCharterBytes = 32 * 1024

var (
	charterHTMLPattern       = regexp.MustCompile(`(?is)<\s*/?\s*[a-z][^>]*>`)
	charterEventPattern      = regexp.MustCompile(`(?i)\bon[a-z]+\s*=`)
	charterCredentialPattern = regexp.MustCompile(`(?i)(api[_-]?key|access[_-]?token|bearer\s+|password|secret|private[_-]?key)\s*[:=]\s*\S+`)
	charterResourcePattern   = regexp.MustCompile(`(?i)(\]\(\s*(https?:|//)|<\s*(https?:|javascript:))`)
)

type GroupCharter struct {
	HubID          string  `json:"hubId,omitempty"`
	CircleID       string  `json:"circleId,omitempty"`
	GroupID        string  `json:"groupId"`
	CharterVersion int64   `json:"charterVersion"`
	HasCharter     bool    `json:"hasCharter"`
	ContentHash    string  `json:"contentHash,omitempty"`
	Content        string  `json:"content,omitempty"`
	UpdatedBy      string  `json:"updatedBy,omitempty"`
	CreatedAt      string  `json:"createdAt,omitempty"`
	UpdatedAt      string  `json:"updatedAt,omitempty"`
	SupersededAt   *string `json:"supersededAt,omitempty"`
}

type GroupCharterInput struct {
	Content         string `json:"content"`
	ExpectedVersion int64  `json:"expectedVersion"`
	IdempotencyKey  string `json:"idempotencyKey"`
}

func ValidateGroupCharter(content string) error {
	if !utf8.ValidString(content) {
		return errors.New("charter must be valid UTF-8")
	}
	if len([]byte(content)) > MaxGroupCharterBytes {
		return fmt.Errorf("charter exceeds %d bytes", MaxGroupCharterBytes)
	}
	if charterHTMLPattern.MatchString(content) || charterEventPattern.MatchString(content) {
		return errors.New("charter contains raw HTML or event-handler markup")
	}
	if charterCredentialPattern.MatchString(content) {
		return errors.New("charter contains credential-like content")
	}
	if charterResourcePattern.MatchString(content) {
		return errors.New("charter contains an uncontrolled external resource")
	}
	return nil
}

func GroupCharterContentHash(content string) string {
	digest := sha256.Sum256([]byte(content))
	return hex.EncodeToString(digest[:])
}

func ValidateGroupCharterInput(input GroupCharterInput) error {
	if strings.TrimSpace(input.IdempotencyKey) == "" || len(input.IdempotencyKey) > 128 || strings.ContainsAny(input.IdempotencyKey, "\r\n") {
		return errors.New("idempotencyKey must be a bounded non-empty value")
	}
	return ValidateGroupCharter(input.Content)
}
