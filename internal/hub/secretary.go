package hub

import "time"

type SecretaryState string

const (
	SecretaryActive  SecretaryState = "ACTIVE"
	SecretaryRevoked SecretaryState = "REVOKED"
)

type GroupSecretary struct {
	HubID          string         `json:"hubId"`
	CircleID       string         `json:"circleId"`
	GroupID        string         `json:"groupId"`
	AgentID        string         `json:"agentId"`
	Epoch          int64          `json:"epoch"`
	State          SecretaryState `json:"state"`
	LeaseExpiresAt time.Time      `json:"leaseExpiresAt"`
	AppointedBy    string         `json:"appointedBy"`
	UpdatedAt      time.Time      `json:"updatedAt"`
}

type GroupSecretaryAppointmentInput struct {
	AgentID       string `json:"agentId"`
	ExpectedEpoch int64  `json:"expectedEpoch"`
	LeaseSeconds  int64  `json:"leaseSeconds"`
}
