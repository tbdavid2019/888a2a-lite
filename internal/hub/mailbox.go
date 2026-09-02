package hub

import (
	"fmt"
	"strings"
	"time"
)

type DeliveryState string

const (
	DeliveryStatePending      DeliveryState = "PENDING"
	DeliveryStateAcknowledged DeliveryState = "ACKNOWLEDGED"
	DeliveryStateCanceled     DeliveryState = "CANCELED"
)

type TaskDelivery struct {
	TargetAgentID  string `json:"targetAgentId"`
	ContextID      string `json:"contextId"`
	IdempotencyKey string `json:"idempotencyKey"`
	Message        string `json:"message"`
	TaskID         string `json:"taskId"`
	MaxOutputBytes int64  `json:"maxOutputBytes,omitempty"`
}

type IdempotencyKey struct {
	HubID            string
	TargetAgentID    string
	RequesterAgentID string
	Key              string
}

func (key IdempotencyKey) String() string {
	return strings.Join([]string{key.HubID, key.TargetAgentID, key.RequesterAgentID, key.Key}, "\x00")
}

type InboxItem struct {
	Sequence         uint64        `json:"sequence"`
	HubID            string        `json:"hubId"`
	TargetAgentID    string        `json:"targetAgentId"`
	RequesterAgentID string        `json:"requesterAgentId"`
	TaskID           string        `json:"taskId"`
	ContextID        string        `json:"contextId"`
	IdempotencyKey   string        `json:"idempotencyKey"`
	Message          string        `json:"message"`
	State            DeliveryState `json:"state"`
	CreatedAt        time.Time     `json:"createdAt"`
	AcknowledgedAt   *time.Time    `json:"acknowledgedAt,omitempty"`
	CanceledAt       *time.Time    `json:"canceledAt,omitempty"`
}

type AcknowledgeRecord struct {
	HubID         string
	TargetAgentID string
	Sequence      uint64
	At            time.Time
}

type CancelRecord struct {
	HubID      string
	TaskID     string
	Reason     string
	CanceledAt time.Time
}

func ValidateTaskDelivery(task TaskDelivery) error {
	if strings.TrimSpace(task.TargetAgentID) == "" {
		return fmt.Errorf("targetAgentId must not be empty")
	}
	if strings.TrimSpace(task.ContextID) == "" {
		return fmt.Errorf("contextId must not be empty")
	}
	if err := ValidateIdempotencyKey(task.IdempotencyKey); err != nil {
		return fmt.Errorf("idempotency key: %w", err)
	}
	if strings.TrimSpace(task.Message) == "" {
		return fmt.Errorf("message must not be empty")
	}
	if strings.TrimSpace(task.TaskID) == "" {
		return fmt.Errorf("taskId must not be empty")
	}
	return nil
}
