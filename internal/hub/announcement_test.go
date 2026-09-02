package hub

import (
	"strings"
	"testing"
	"time"
)

func TestAnnouncementValidation(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	valid := AnnouncementInput{
		Title: "Maintenance", Summary: "The Hub will be updated.", Severity: AnnouncementWarning,
		DocumentationURL: "https://example.com/notice", ExpiresAt: timePtr(now.Add(time.Hour)),
	}
	if err := valid.Validate(now); err != nil {
		t.Fatalf("valid announcement rejected: %v", err)
	}
	for _, input := range []AnnouncementInput{
		{Title: "x", Summary: "api_key=secret", Severity: AnnouncementInfo},
		{Title: strings.Repeat("x", MaxAnnouncementTitle+1), Summary: "summary", Severity: AnnouncementInfo},
		{Title: "x", Summary: "summary", Severity: "UNKNOWN"},
		{Title: "x", Summary: "summary", Severity: AnnouncementInfo, DocumentationURL: "https://user:pass@example.com"},
	} {
		if err := input.Validate(now); err == nil {
			t.Fatalf("invalid announcement accepted: %+v", input)
		}
	}
}

func TestAnnouncementSummaryAndActivity(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	expires := now.Add(time.Hour)
	announcement := Announcement{
		ID: 7, Revision: 2, Status: AnnouncementPublished, Severity: AnnouncementInfo,
		Title: "Notice", Summary: "Summary", PublishedAt: timePtr(now), ExpiresAt: &expires,
	}
	if !announcement.IsActive(now) || announcement.IsActive(expires) {
		t.Fatal("announcement activity state is incorrect")
	}
	if summary := announcement.SummaryView(); summary.ID != 7 || summary.Revision != 2 || summary.Summary != "Summary" {
		t.Fatalf("summary = %+v", summary)
	}
}
