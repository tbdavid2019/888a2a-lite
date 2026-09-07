package hub

import "time"

type CircleState string

const (
	CircleStateActive   CircleState = "ACTIVE"
	CircleStateDisabled CircleState = "DISABLED"
)

type Circle struct {
	HubID            string      `json:"hubId"`
	CircleID         string      `json:"circleId"`
	Alias            string      `json:"alias,omitempty"`
	State            CircleState `json:"state"`
	ActiveKeyVersion int         `json:"activeKeyVersion,omitempty"`
	CreatedAt        time.Time   `json:"createdAt"`
	DisabledAt       *time.Time  `json:"disabledAt,omitempty"`
}

type CircleKey struct {
	HubID      string     `json:"hubId"`
	CircleID   string     `json:"circleId"`
	Version    int        `json:"version"`
	KeyDigest  string     `json:"-"`
	State      string     `json:"state"`
	CreatedAt  time.Time  `json:"createdAt"`
	GraceUntil *time.Time `json:"graceUntil,omitempty"`
	RevokedAt  *time.Time `json:"revokedAt,omitempty"`
}
