package store

import (
	"context"
	"errors"
	"time"

	"github.com/tbdavid2019/888a2a-lite/internal/hub"
)

var (
	ErrNotFound     = errors.New("store record not found")
	ErrCanceled     = errors.New("store inbox item is canceled")
	ErrInvalidState = errors.New("store record has an invalid state")
	ErrForbidden    = errors.New("store operation is forbidden")
)

type AgentStore interface {
	CreateAgent(context.Context, hub.RegisteredAgent) error
	FindAgent(context.Context, string) (hub.RegisteredAgent, error)
	FindAgentByRegistrationKey(context.Context, string) (hub.RegisteredAgent, error)
	ListAgents(context.Context) ([]hub.RegisteredAgent, error)
	CountAgents(context.Context) (int, error)
	AuthenticateAgent(context.Context, string, string) (hub.RegisteredAgent, error)
	HeartbeatAgent(context.Context, string, time.Time, time.Time) (hub.RegisteredAgent, error)
	DisconnectAgent(context.Context, string, time.Time) error
	RevokeAgent(context.Context, string, string, time.Time) error
}

type PolicyStore interface {
	GetPolicy(context.Context) (hub.HubPolicy, error)
	SavePolicy(context.Context, hub.HubPolicy) error
	SetRegistrationEnabled(context.Context, bool) error
}

type InboxStore interface {
	Enqueue(context.Context, hub.InboxItem) (hub.InboxItem, bool, error)
	FindByIdempotencyKey(context.Context, hub.IdempotencyKey) (hub.InboxItem, bool, error)
	Poll(context.Context, string, uint64, int) ([]hub.InboxItem, error)
	Acknowledge(context.Context, string, uint64, time.Time) error
	CancelTask(context.Context, string, string, time.Time) error
	PendingCount(context.Context, string) (int, error)
}

type EventStore interface {
	AppendEvent(context.Context, hub.Event) error
	ListEvents(context.Context, uint64, int) ([]hub.Event, error)
}

type AnnouncementStore interface {
	CreateAnnouncement(context.Context, hub.Announcement) (hub.Announcement, error)
	FindAnnouncement(context.Context, uint64) (hub.Announcement, error)
	ListAnnouncements(context.Context, uint64, int) ([]hub.Announcement, error)
	ListActiveAnnouncements(context.Context, uint64, int, time.Time) ([]hub.Announcement, error)
	UpdateDraft(context.Context, uint64, hub.AnnouncementInput, time.Time) (hub.Announcement, error)
	PublishAnnouncement(context.Context, uint64, time.Time) (hub.Announcement, error)
	CreateRevision(context.Context, uint64, hub.AnnouncementInput, time.Time) (hub.Announcement, error)
}

type GroupStore interface {
	CreateGroup(context.Context, hub.Group) (hub.Group, error)
	FindGroup(context.Context, string) (hub.Group, error)
	ListGroups(context.Context, string) ([]hub.Group, error)
	FindMember(context.Context, string, string) (hub.GroupMember, error)
	ListMembers(context.Context, string) ([]hub.GroupMember, error)
	CreateInvitation(context.Context, hub.GroupInvitation) (hub.GroupInvitation, error)
	FindInvitation(context.Context, uint64) (hub.GroupInvitation, error)
	AcceptInvitation(context.Context, uint64, string, time.Time) (hub.GroupMember, error)
	LeaveGroup(context.Context, string, string, time.Time) error
	RemoveMember(context.Context, string, string, time.Time) error
	TransferOwnership(context.Context, string, string, string) error
	ArchiveGroup(context.Context, string, time.Time) error
	SendGroupMessage(context.Context, hub.GroupMessage, int) (hub.GroupMessage, bool, error)
	ListGroupMessages(context.Context, string, string, uint64, int) ([]hub.GroupMessage, error)
	CancelPendingGroupDeliveries(context.Context, string, string, time.Time) error
}

type TxStore interface {
	Agents() AgentStore
	Policy() PolicyStore
	Inbox() InboxStore
	Events() EventStore
	Announcements() AnnouncementStore
	Groups() GroupStore
}

type Store interface {
	TxStore
	WithTransaction(context.Context, func(TxStore) error) error
	Close() error
}
