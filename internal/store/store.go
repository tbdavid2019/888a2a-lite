package store

import (
	"context"
	"errors"
	"time"

	"github.com/tbdavid2019/888a2a-lite/internal/a2a"
	"github.com/tbdavid2019/888a2a-lite/internal/hub"
)

var (
	ErrNotFound     = errors.New("store record not found")
	ErrCanceled     = errors.New("store inbox item is canceled")
	ErrInvalidState = errors.New("store record has an invalid state")
	ErrForbidden    = errors.New("store operation is forbidden")
	ErrConflict     = errors.New("store operation conflicts with current state")
)

type AgentStore interface {
	CreateAgent(context.Context, hub.RegisteredAgent) error
	FindAgent(context.Context, string) (hub.RegisteredAgent, error)
	FindAgentByRegistrationKey(context.Context, string, string) (hub.RegisteredAgent, error)
	ListAgents(context.Context) ([]hub.RegisteredAgent, error)
	CountAgents(context.Context) (int, error)
	CountAgentsInCircle(context.Context, string) (int, error)
	AuthenticateAgent(context.Context, string, string) (hub.RegisteredAgent, error)
	AuthenticateAgentByToken(context.Context, string) (hub.RegisteredAgent, error)
	HeartbeatAgent(context.Context, string, time.Time, time.Time) (hub.RegisteredAgent, error)
	DisconnectAgent(context.Context, string, time.Time) error
	RevokeAgent(context.Context, string, string, time.Time) error
	RevokeAgentsInCircle(context.Context, string, string, time.Time) (int64, error)
	DeleteAgent(context.Context, string) error
	PruneInactiveAgents(context.Context, time.Time) (int64, error)
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
	AcknowledgeTask(context.Context, string, string, time.Time) error
	CancelTask(context.Context, string, string, time.Time) error
	PendingCount(context.Context, string) (int, error)
	PendingCountInCircle(context.Context, string) (int, error)
	ListDirectMessagesAdmin(context.Context, uint64, int, string) ([]hub.InboxItem, error)
	ListDirectMessagesAdminInCircle(context.Context, uint64, int, string, string) ([]hub.InboxItem, error)
}

type StandardTaskStore interface {
	CreateTaskWithDelivery(context.Context, a2a.TaskRecord, hub.InboxItem) (a2a.TaskRecord, bool, error)
	ResumeTaskWithDelivery(context.Context, a2a.TaskRecord, int64, hub.InboxItem) (a2a.TaskRecord, bool, error)
	FindTask(context.Context, string, string, string, string) (a2a.TaskRecord, error)
	FindTaskByMessage(context.Context, string, string, string, string, string) (a2a.TaskRecord, error)
	ListTasks(context.Context, a2a.TaskFilter) ([]a2a.TaskRecord, int, error)
	ListTaskEvents(context.Context, string, string, string, string, int64) ([]a2a.TaskEvent, error)
	ApplyUpdate(context.Context, a2a.TaskUpdate) (a2a.TaskRecord, bool, error)
	CancelStandardTask(context.Context, string, string, string, string, time.Time) (a2a.TaskRecord, error)
	CreateGroupTask(context.Context, a2a.TaskRecord, []a2a.TaskRecord, []hub.InboxItem, []a2a.GroupTaskMember, int, int) (a2a.TaskRecord, bool, error)
	ListGroupTaskMembers(context.Context, string, string, string, string) ([]a2a.TaskRecord, error)
	ExpireStandardTasks(context.Context, time.Time) (int, error)
}

type EventStore interface {
	AppendEvent(context.Context, hub.Event) error
	ListEvents(context.Context, uint64, int) ([]hub.Event, error)
	ListEventsInCircle(context.Context, uint64, int, string) ([]hub.Event, error)
}

type CircleStore interface {
	CreateCircle(context.Context, hub.Circle) error
	FindCircle(context.Context, string) (hub.Circle, error)
	ListCircles(context.Context) ([]hub.Circle, error)
	SetCircleState(context.Context, string, hub.CircleState, *time.Time) error
	CreateCircleKey(context.Context, hub.CircleKey) error
	RotateCircleKey(context.Context, string, hub.CircleKey, *time.Time) error
	FindActiveCircleKey(context.Context, string, string, time.Time) (hub.CircleKey, error)
	FindCircleByKeyDigest(context.Context, string, time.Time) (hub.Circle, error)
	ListCircleKeys(context.Context, string) ([]hub.CircleKey, error)
	RevokeCircleKey(context.Context, string, int, time.Time) error
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
	FindPendingInvitation(context.Context, string, string) (hub.GroupInvitation, error)
	ListInvitations(context.Context, string) ([]hub.GroupInvitation, error)
	AcceptInvitation(context.Context, uint64, string, time.Time) (hub.GroupMember, error)
	LeaveGroup(context.Context, string, string, time.Time) error
	RemoveMember(context.Context, string, string, time.Time) error
	TransferOwnership(context.Context, string, string, string) error
	ArchiveGroup(context.Context, string, time.Time) error
	SendGroupMessage(context.Context, hub.GroupMessage, int) (hub.GroupMessage, bool, error)
	ListGroupMessages(context.Context, string, string, uint64, int) ([]hub.GroupMessage, error)
	CancelPendingGroupDeliveries(context.Context, string, string, time.Time) error
	ListGroupMessagesAdmin(context.Context, uint64, int, string, string) ([]hub.GroupMessage, error)
	ListGroupMessagesAdminInCircle(context.Context, uint64, int, string, string, string) ([]hub.GroupMessage, error)
}

type GroupCharterStore interface {
	GetGroupCharter(context.Context, string, string, string) (hub.GroupCharter, error)
	ListGroupCharterRevisions(context.Context, string, string, string) ([]hub.GroupCharter, error)
	PutGroupCharter(context.Context, hub.GroupCharter, int64, string) (hub.GroupCharter, bool, error)
}

type GroupSecretaryStore interface {
	GetGroupSecretary(context.Context, string, string, string) (hub.GroupSecretary, error)
	AppointGroupSecretary(context.Context, hub.GroupSecretary, int64) (hub.GroupSecretary, error)
	RenewGroupSecretary(context.Context, string, string, string, string, int64, time.Time) (hub.GroupSecretary, error)
	RevokeGroupSecretary(context.Context, string, string, string, int64, time.Time) error
}

type TxStore interface {
	Circles() CircleStore
	Agents() AgentStore
	Policy() PolicyStore
	Inbox() InboxStore
	Events() EventStore
	Announcements() AnnouncementStore
	Groups() GroupStore
	GroupCharters() GroupCharterStore
	GroupSecretaries() GroupSecretaryStore
	StandardTasks() StandardTaskStore
}

type Store interface {
	TxStore
	WithTransaction(context.Context, func(TxStore) error) error
	Close() error
}
