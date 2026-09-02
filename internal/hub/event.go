package hub

import "time"

type Event struct {
	ID            uint64         `json:"id"`
	HubID         string         `json:"hubId"`
	Type          string         `json:"type"`
	ActorAgentID  string         `json:"actorAgentId,omitempty"`
	TargetAgentID string         `json:"targetAgentId,omitempty"`
	TaskID        string         `json:"taskId,omitempty"`
	Details       map[string]any `json:"details,omitempty"`
	CreatedAt     time.Time      `json:"createdAt"`
}

const (
	EventAgentRegistered        = "agent.registered"
	EventAgentRegistrationRetry = "agent.registration_retry"
	EventAgentHeartbeat         = "agent.heartbeat"
	EventAgentDisconnected      = "agent.disconnected"
	EventAgentRevoked           = "agent.revoked"
	EventTaskQueued             = "task.queued"
	EventTaskDuplicate          = "task.duplicate"
	EventInboxPolled            = "inbox.polled"
	EventInboxAcknowledged      = "inbox.acknowledged"
	EventTaskCanceled           = "task.canceled"
	EventRegistrationChanged    = "registration.changed"
	EventHubStarted             = "hub.started"
	EventHubStopped             = "hub.stopped"
)
