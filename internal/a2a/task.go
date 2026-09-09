package a2a

import "time"

// TaskRecord is the durable adapter-side representation. It deliberately
// keeps the original Message/Parts instead of only a flattened mailbox text.
type TaskRecord struct {
	HubID             string
	ID                string
	CircleID          string
	RequesterAgentID  string
	TargetAgentID     string
	ContextID         string
	MessageID         string
	TurnID            string
	Revision          int64
	State             TaskState
	Message           Message
	History           []Message
	ResultMessage     *Message
	Artifacts         []Artifact
	MailboxSequence   uint64
	ContentDigest     string
	ExecutionDeadline time.Time
	RetryBudget       int
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type TaskFilter struct {
	HubID            string
	CircleID         string
	RequesterAgentID string
	TargetAgentID    string
	ContextID        string
	State            TaskState
	PageSize         int
	Offset           int
}

type TaskUpdate struct {
	HubID            string
	TaskID           string
	TargetAgentID    string
	UpdateID         string
	TurnID           string
	ExpectedRevision int64
	State            TaskState
	Message          *Message
	Artifacts        []Artifact
}

type TaskEvent struct {
	Revision  int64
	EventType string
	Task      Task
	CreatedAt time.Time
}

func (record TaskRecord) PublicTask(historyLength int, includeArtifacts bool) Task {
	history := append([]Message(nil), record.History...)
	if historyLength >= 0 && len(history) > historyLength {
		history = history[len(history)-historyLength:]
	}
	artifacts := append([]Artifact(nil), record.Artifacts...)
	if !includeArtifacts {
		artifacts = nil
	}
	statusMessage := record.ResultMessage
	if statusMessage == nil && record.State == TaskStateSubmitted {
		statusMessage = nil
	}
	return Task{
		ID: record.ID, ContextID: record.ContextID,
		Status:    TaskStatus{State: record.State, Message: statusMessage, Timestamp: record.UpdatedAt},
		Artifacts: artifacts, History: history,
	}
}

func (update TaskUpdate) Digestable() any {
	return struct {
		TaskID           string     `json:"taskId"`
		TurnID           string     `json:"turnId"`
		ExpectedRevision int64      `json:"expectedRevision"`
		State            TaskState  `json:"state"`
		Message          *Message   `json:"message,omitempty"`
		Artifacts        []Artifact `json:"artifacts,omitempty"`
	}{update.TaskID, update.TurnID, update.ExpectedRevision, update.State, update.Message, update.Artifacts}
}
