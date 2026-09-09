package hub

import "time"

type MeetingSessionState string

const (
	MeetingSessionOpen         MeetingSessionState = "OPEN"
	MeetingSessionSynthesizing MeetingSessionState = "SYNTHESIZING"
	MeetingSessionConcluded    MeetingSessionState = "CONCLUDED"
	MeetingSessionCancelled    MeetingSessionState = "CANCELLED"
)

type MeetingSession struct {
	HubID            string              `json:"hubId"`
	CircleID         string              `json:"circleId"`
	GroupID          string              `json:"groupId"`
	SessionID        string              `json:"sessionId"`
	StartRevision    int64               `json:"startRevision"`
	CutoffRevision   int64               `json:"cutoffRevision"`
	TriggerMessageID string              `json:"triggerMessageId"`
	TriggeredBy      string              `json:"triggeredBy"`
	CharterVersion   int64               `json:"charterVersion"`
	State            MeetingSessionState `json:"state"`
	SynthesisJobID   string              `json:"synthesisJobId"`
	CreatedAt        time.Time           `json:"createdAt"`
	ConcludedAt      *time.Time          `json:"concludedAt,omitempty"`
}

type TriggerMeetingSessionInput struct {
	GroupID          string `json:"groupId,omitempty"`
	Command          string `json:"command,omitempty"`
	TriggerMessageID string `json:"triggerMessageId,omitempty"`
	IdempotencyKey   string `json:"idempotencyKey,omitempty"`
}

type ConcludeMeetingSessionInput struct {
	Summary   string `json:"summary,omitempty"`
	Decisions int    `json:"decisions,omitempty"`
	Actions   int    `json:"actions,omitempty"`
}
