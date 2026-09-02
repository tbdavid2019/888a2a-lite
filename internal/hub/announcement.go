package hub

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	AnnouncementExtensionURI = "https://github.com/tbdavid2019/888a2a-lite/extensions/hub-announcements/v1"
	MaxAnnouncementTitle     = 128
	MaxAnnouncementSummary   = 4096
	MaxAnnouncementURL       = 2048
)

type AnnouncementSeverity string

const (
	AnnouncementInfo     AnnouncementSeverity = "INFO"
	AnnouncementWarning  AnnouncementSeverity = "WARNING"
	AnnouncementCritical AnnouncementSeverity = "CRITICAL"
)

type AnnouncementStatus string

const (
	AnnouncementDraft     AnnouncementStatus = "DRAFT"
	AnnouncementPublished AnnouncementStatus = "PUBLISHED"
	AnnouncementExpired   AnnouncementStatus = "EXPIRED"
)

type Announcement struct {
	ID               uint64               `json:"id"`
	HubID            string               `json:"hubId"`
	RevisionOfID     *uint64              `json:"revisionOfId,omitempty"`
	Revision         int                  `json:"revision"`
	Status           AnnouncementStatus   `json:"status"`
	Severity         AnnouncementSeverity `json:"severity"`
	Title            string               `json:"title"`
	Summary          string               `json:"summary"`
	DocumentationURL string               `json:"documentationUrl,omitempty"`
	PublishedAt      *time.Time           `json:"publishedAt,omitempty"`
	ExpiresAt        *time.Time           `json:"expiresAt,omitempty"`
	CreatedAt        time.Time            `json:"createdAt"`
}

type AnnouncementInput struct {
	Title            string               `json:"title"`
	Summary          string               `json:"summary"`
	Severity         AnnouncementSeverity `json:"severity"`
	DocumentationURL string               `json:"documentationUrl,omitempty"`
	ExpiresAt        *time.Time           `json:"expiresAt,omitempty"`
}

type HubMetadata struct {
	SystemCardURL       string                `json:"systemCardUrl"`
	AnnouncementFeedURL string                `json:"announcementFeedUrl"`
	AnnouncementCursor  uint64                `json:"announcementCursor"`
	Announcements       []AnnouncementSummary `json:"announcements"`
	ExtensionURI        string                `json:"extensionUri"`
}

type AnnouncementSummary struct {
	ID               uint64               `json:"id"`
	Revision         int                  `json:"revision"`
	Severity         AnnouncementSeverity `json:"severity"`
	Title            string               `json:"title"`
	Summary          string               `json:"summary"`
	DocumentationURL string               `json:"documentationUrl,omitempty"`
	PublishedAt      *time.Time           `json:"publishedAt,omitempty"`
	ExpiresAt        *time.Time           `json:"expiresAt,omitempty"`
}

type SystemCardExtension struct {
	URI      string `json:"uri"`
	Required bool   `json:"required"`
}

type HubSystemCard struct {
	HubID                string                `json:"hubId"`
	SelfURL              string                `json:"selfUrl"`
	Mode                 string                `json:"mode"`
	Protocol             string                `json:"protocol"`
	ProtocolVersion      string                `json:"protocolVersion"`
	DeliverySemantics    string                `json:"deliverySemantics"`
	CapabilityTrust      string                `json:"capabilityTrust"`
	IncomingMessageTrust string                `json:"incomingMessageTrust"`
	SystemMetadataTrust  string                `json:"systemMetadataTrust"`
	RemoteExecution      bool                  `json:"remoteExecution"`
	SystemCardURL        string                `json:"systemCardUrl"`
	AnnouncementFeedURL  string                `json:"announcementFeedUrl"`
	GroupBaseURL         string                `json:"groupBaseUrl,omitempty"`
	Limits               map[string]int64      `json:"limits"`
	Extensions           []SystemCardExtension `json:"extensions"`
	UpdatedAt            time.Time             `json:"updatedAt"`
}

func (input AnnouncementInput) Validate(now time.Time) error {
	if err := validateAnnouncementText("title", input.Title, MaxAnnouncementTitle); err != nil {
		return err
	}
	if err := validateAnnouncementText("summary", input.Summary, MaxAnnouncementSummary); err != nil {
		return err
	}
	switch input.Severity {
	case AnnouncementInfo, AnnouncementWarning, AnnouncementCritical:
	default:
		return fmt.Errorf("severity must be INFO, WARNING, or CRITICAL")
	}
	if err := validateAnnouncementURL(input.DocumentationURL); err != nil {
		return err
	}
	if input.ExpiresAt != nil && !input.ExpiresAt.After(now) {
		return errors.New("expiresAt must be in the future")
	}
	return nil
}

func (announcement Announcement) IsActive(now time.Time) bool {
	return announcement.Status == AnnouncementPublished &&
		(announcement.ExpiresAt == nil || announcement.ExpiresAt.After(now))
}

func (announcement Announcement) SummaryView() AnnouncementSummary {
	return AnnouncementSummary{
		ID: announcement.ID, Revision: announcement.Revision, Severity: announcement.Severity,
		Title: announcement.Title, Summary: announcement.Summary,
		DocumentationURL: announcement.DocumentationURL, PublishedAt: announcement.PublishedAt,
		ExpiresAt: announcement.ExpiresAt,
	}
}

func validateAnnouncementText(name, value string, max int) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("%s must not be empty", name)
	}
	if len(value) > max {
		return fmt.Errorf("%s must not exceed %d bytes", name, max)
	}
	lower := strings.ToLower(value)
	for _, marker := range []string{"-----begin", "api_key=", "password=", "bearer ", "token="} {
		if strings.Contains(lower, marker) {
			return fmt.Errorf("%s contains prohibited credential-like content", name)
		}
	}
	return nil
}

func validateAnnouncementURL(value string) error {
	if value == "" {
		return nil
	}
	if len(value) > MaxAnnouncementURL {
		return fmt.Errorf("documentationUrl must not exceed %d bytes", MaxAnnouncementURL)
	}
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return errors.New("documentationUrl must be an http or https URL without userinfo")
	}
	return nil
}
