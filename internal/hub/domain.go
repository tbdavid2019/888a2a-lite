package hub

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	AgentCardVersion        = "1"
	MaxDisplayNameLength    = 128
	MaxProviderFamilyLength = 64
	MaxTransportIDLength    = 128
	MaxCapabilityLength     = 128
	MaxCapabilities         = 32
	MaxIdempotencyKeyLength = 256
)

type AgentState string

const (
	AgentStatePending AgentState = "PENDING"
	AgentStateOnline  AgentState = "ONLINE"
	AgentStateOffline AgentState = "OFFLINE"
	AgentStateExpired AgentState = "EXPIRED"
	AgentStateRevoked AgentState = "REVOKED"
)

type AgentDeclaration struct {
	DisplayName             string   `json:"displayName"`
	ProviderFamily          string   `json:"providerFamily"`
	TransportID             string   `json:"transportId"`
	Capabilities            []string `json:"capabilities"`
	RegistrationIdempotency string   `json:"registrationIdempotencyKey"`
}

type AgentIdentity struct {
	HubID      string    `json:"hubId"`
	AgentID    string    `json:"agentId"`
	AgentToken string    `json:"agentToken,omitempty"`
	ExpiresAt  time.Time `json:"expiresAt"`
}

type HubPolicy struct {
	HubID               string        `json:"hubId"`
	RegistrationEnabled bool          `json:"registrationEnabled"`
	RegistrationTTL     time.Duration `json:"registrationTtl"`
	PeerLease           time.Duration `json:"peerLease"`
	MaxRegisteredAgents int           `json:"maxRegisteredAgents"`
	MaxTasksPerMinute   int           `json:"maxTasksPerMinute"`
	MaxConcurrentTasks  int           `json:"maxConcurrentTasks"`
	MaxPayloadBytes     int64         `json:"maxPayloadBytes"`
	MaxGroupMembers     int           `json:"maxGroupMembers"`
	MaxGroupFanout      int           `json:"maxGroupFanout"`
	MaxGroupHistoryPage int           `json:"maxGroupHistoryPage"`
}

type RegisteredAgent struct {
	HubID               string
	AgentID             string
	DisplayName         string
	ProviderFamily      string
	TransportID         string
	Capabilities        []string
	AgentCardJSON       string
	RegistrationKeyHash string
	TokenHash           string
	AutomaticExecution  bool
	State               AgentState
	LastSeenAt          time.Time
	ExpiresAt           time.Time
	LeaseExpiresAt      time.Time
	CreatedAt           time.Time
	RevokedAt           *time.Time
	RevokeReason        string
}

type AgentCard struct {
	URL                string   `json:"url"`
	Name               string   `json:"name"`
	Version            string   `json:"version"`
	ProviderFamily     string   `json:"providerFamily"`
	TransportID        string   `json:"transportId"`
	Capabilities       []string `json:"capabilities"`
	AutomaticExecution bool     `json:"automaticExecution"`
}

type AgentView struct {
	HubID              string     `json:"hubId"`
	AgentID            string     `json:"agentId"`
	DisplayName        string     `json:"displayName"`
	ProviderFamily     string     `json:"providerFamily"`
	TransportID        string     `json:"transportId"`
	Capabilities       []string   `json:"capabilities"`
	State              AgentState `json:"state"`
	LastSeenAt         time.Time  `json:"lastSeenAt,omitempty"`
	ExpiresAt          time.Time  `json:"expiresAt"`
	AutomaticExecution bool       `json:"automaticExecution"`
	Card               AgentCard  `json:"card"`
}

func GenerateToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate agent token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func VerifyToken(hash, token string) bool {
	stored, err := hex.DecodeString(hash)
	if err != nil || len(stored) != sha256.Size {
		return false
	}
	calculated := sha256.Sum256([]byte(token))
	return subtle.ConstantTimeCompare(stored, calculated[:]) == 1
}

func (agent RegisteredAgent) StateAt(now time.Time) AgentState {
	if agent.RevokedAt != nil {
		return AgentStateRevoked
	}
	if !agent.ExpiresAt.IsZero() && !now.Before(agent.ExpiresAt) {
		return AgentStateExpired
	}
	if agent.LastSeenAt.IsZero() || agent.LeaseExpiresAt.IsZero() {
		return AgentStatePending
	}
	if !now.Before(agent.LeaseExpiresAt) {
		return AgentStateOffline
	}
	return AgentStateOnline
}

func (agent RegisteredAgent) SafeView(baseURL string) AgentView {
	capabilities := append([]string(nil), agent.Capabilities...)
	baseURL = strings.TrimRight(baseURL, "/")
	return AgentView{
		HubID:              agent.HubID,
		AgentID:            agent.AgentID,
		DisplayName:        agent.DisplayName,
		ProviderFamily:     agent.ProviderFamily,
		TransportID:        agent.TransportID,
		Capabilities:       capabilities,
		State:              agent.StateAt(time.Now().UTC()),
		LastSeenAt:         agent.LastSeenAt,
		ExpiresAt:          agent.ExpiresAt,
		AutomaticExecution: agent.AutomaticExecution,
		Card: AgentCard{
			URL:                fmt.Sprintf("%s/hub/v1/agents/%s/agent-card.json", baseURL, agent.AgentID),
			Name:               agent.DisplayName,
			Version:            AgentCardVersion,
			ProviderFamily:     agent.ProviderFamily,
			TransportID:        agent.TransportID,
			Capabilities:       capabilities,
			AutomaticExecution: agent.AutomaticExecution,
		},
	}
}

func ValidateDeclaration(declaration AgentDeclaration) error {
	if err := validateText("displayName", declaration.DisplayName, MaxDisplayNameLength); err != nil {
		return err
	}
	if err := validateText("providerFamily", declaration.ProviderFamily, MaxProviderFamilyLength); err != nil {
		return err
	}
	if err := validateText("transportId", declaration.TransportID, MaxTransportIDLength); err != nil {
		return err
	}
	if err := ValidateIdempotencyKey(declaration.RegistrationIdempotency); err != nil {
		return fmt.Errorf("registration idempotency key: %w", err)
	}
	if len(declaration.Capabilities) > MaxCapabilities {
		return fmt.Errorf("capabilities exceed maximum of %d", MaxCapabilities)
	}
	for _, capability := range declaration.Capabilities {
		if err := validateText("capability", capability, MaxCapabilityLength); err != nil {
			return err
		}
	}
	return nil
}

func ValidateIdempotencyKey(key string) error {
	if strings.TrimSpace(key) == "" {
		return errors.New("must not be empty")
	}
	if len(key) > MaxIdempotencyKeyLength {
		return fmt.Errorf("must not exceed %d bytes", MaxIdempotencyKeyLength)
	}
	return nil
}

func validateText(name, value string, maxLength int) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("%s must not be empty", name)
	}
	if len(value) > maxLength {
		return fmt.Errorf("%s must not exceed %d bytes", name, maxLength)
	}
	return nil
}
