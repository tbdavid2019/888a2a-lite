package hub

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	GroupExtensionURI       = "https://github.com/tbdavid2019/888a2a-lite/extensions/agent-groups/v1"
	MaxGroupNameLength      = 128
	MaxGroupMembers         = 32
	MaxGroupFanout          = 32
	MaxGroupHistoryPageSize = 100
)

type GroupState string

const (
	GroupStateActive   GroupState = "ACTIVE"
	GroupStateArchived GroupState = "ARCHIVED"
)

type GroupRole string

const (
	GroupRoleOwner  GroupRole = "OWNER"
	GroupRoleAdmin  GroupRole = "ADMIN"
	GroupRoleMember GroupRole = "MEMBER"
)

type MembershipState string

const (
	MembershipActive  MembershipState = "ACTIVE"
	MembershipLeft    MembershipState = "LEFT"
	MembershipRemoved MembershipState = "REMOVED"
)

type InvitationState string

const (
	InvitationPending  InvitationState = "PENDING"
	InvitationAccepted InvitationState = "ACCEPTED"
	InvitationDeclined InvitationState = "DECLINED"
	InvitationExpired  InvitationState = "EXPIRED"
	InvitationRevoked  InvitationState = "REVOKED"
)

type Group struct {
	HubID        string     `json:"hubId"`
	GroupID      string     `json:"groupId"`
	Name         string     `json:"name"`
	State        GroupState `json:"state"`
	OwnerAgentID string     `json:"ownerAgentId"`
	CreatedAt    time.Time  `json:"createdAt"`
	ArchivedAt   *time.Time `json:"archivedAt,omitempty"`
}

type GroupMember struct {
	HubID     string          `json:"hubId"`
	GroupID   string          `json:"groupId"`
	AgentID   string          `json:"agentId"`
	Role      GroupRole       `json:"role"`
	State     MembershipState `json:"state"`
	JoinedAt  time.Time       `json:"joinedAt"`
	LeftAt    *time.Time      `json:"leftAt,omitempty"`
	RemovedAt *time.Time      `json:"removedAt,omitempty"`
	Agent     *AgentView      `json:"agent,omitempty"`
}

type GroupInvitation struct {
	ID             uint64          `json:"invitationId"`
	HubID          string          `json:"hubId"`
	GroupID        string          `json:"groupId"`
	InviterAgentID string          `json:"inviterAgentId"`
	InviteeAgentID string          `json:"inviteeAgentId"`
	State          InvitationState `json:"state"`
	CreatedAt      time.Time       `json:"createdAt"`
	ExpiresAt      time.Time       `json:"expiresAt"`
	RespondedAt    *time.Time      `json:"respondedAt,omitempty"`
}

type CreateGroupInput struct {
	Name string `json:"name"`
}

type GroupMessageInput struct {
	ContextID      string `json:"contextId"`
	IdempotencyKey string `json:"idempotencyKey"`
	Message        string `json:"message"`
}

type GroupDeliverySummary struct {
	TargetAgentID string        `json:"targetAgentId"`
	Sequence      uint64        `json:"sequence"`
	State         DeliveryState `json:"state"`
}

type GroupMessage struct {
	ID             uint64                 `json:"groupMessageId"`
	HubID          string                 `json:"hubId"`
	GroupID        string                 `json:"groupId"`
	SenderAgentID  string                 `json:"senderAgentId"`
	ContextID      string                 `json:"contextId"`
	IdempotencyKey string                 `json:"idempotencyKey"`
	Message        string                 `json:"message"`
	Trust          string                 `json:"trust"`
	CreatedAt      time.Time              `json:"createdAt"`
	Deliveries     []GroupDeliverySummary `json:"deliveries,omitempty"`
}

type GroupHistoryItem struct {
	GroupMessage
	Sender *AgentView `json:"sender,omitempty"`
}

func ValidateCreateGroup(input CreateGroupInput) error {
	return validateText("name", input.Name, MaxGroupNameLength)
}

func ValidateGroupMessage(input GroupMessageInput, maxPayloadBytes int64) error {
	if strings.TrimSpace(input.ContextID) == "" {
		return errors.New("contextId must not be empty")
	}
	if err := ValidateIdempotencyKey(input.IdempotencyKey); err != nil {
		return fmt.Errorf("idempotency key: %w", err)
	}
	if strings.TrimSpace(input.Message) == "" {
		return errors.New("message must not be empty")
	}
	if maxPayloadBytes > 0 && int64(len(input.Message)) > maxPayloadBytes {
		return fmt.Errorf("message exceeds payload limit of %d bytes", maxPayloadBytes)
	}
	return nil
}

func (group Group) IsActive() bool { return group.State == GroupStateActive }

func (member GroupMember) IsActive() bool {
	return member.State == MembershipActive
}

func (member GroupMember) CanManageMembers() bool {
	return member.IsActive() && (member.Role == GroupRoleOwner || member.Role == GroupRoleAdmin)
}
