package store

import (
	"context"
	"errors"
	"time"

	"github.com/tbdavid2019/888a2a-lite/internal/hub"
)

var (
	ErrNotFound = errors.New("store record not found")
	ErrCanceled = errors.New("store inbox item is canceled")
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

type TxStore interface {
	Agents() AgentStore
	Policy() PolicyStore
	Inbox() InboxStore
	Events() EventStore
}

type Store interface {
	TxStore
	WithTransaction(context.Context, func(TxStore) error) error
	Close() error
}
