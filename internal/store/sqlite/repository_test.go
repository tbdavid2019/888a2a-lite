package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/tbdavid2019/888a2a-lite/internal/hub"
	"github.com/tbdavid2019/888a2a-lite/internal/store"
)

func TestRepositoryPersistsInboxAndRecoversPendingItems(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "hub.db")
	database, err := Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("Open returned an error: %v", err)
	}
	repository := NewRepository(database)
	createdAt := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	for _, agentID := range []string{"sender", "target"} {
		if err := repository.CreateAgent(ctx, testAgent(agentID, createdAt)); err != nil {
			t.Fatalf("CreateAgent(%q): %v", agentID, err)
		}
	}

	item := hub.InboxItem{
		HubID:            "public",
		TargetAgentID:    "target",
		RequesterAgentID: "sender",
		TaskID:           "task-1",
		ContextID:        "context-1",
		IdempotencyKey:   "delivery-1",
		Message:          "hello",
		CreatedAt:        createdAt,
	}
	stored, duplicate, err := repository.Enqueue(ctx, item)
	if err != nil || duplicate {
		t.Fatalf("first enqueue = item:%+v duplicate:%v err:%v", stored, duplicate, err)
	}
	if stored.Sequence == 0 || stored.State != hub.DeliveryStatePending {
		t.Fatalf("stored item = %+v", stored)
	}

	duplicateItem, duplicate, err := repository.Enqueue(ctx, item)
	if err != nil || !duplicate || duplicateItem.Sequence != stored.Sequence {
		t.Fatalf("duplicate enqueue = item:%+v duplicate:%v err:%v", duplicateItem, duplicate, err)
	}

	items, err := repository.Poll(ctx, "target", 0, 10)
	if err != nil || len(items) != 1 || items[0].Sequence != stored.Sequence {
		t.Fatalf("poll = items:%+v err:%v", items, err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}

	database, err = Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("reopen returned an error: %v", err)
	}
	repository = NewRepository(database)
	items, err = repository.Poll(ctx, "target", 0, 10)
	if err != nil || len(items) != 1 || items[0].TaskID != "task-1" || items[0].Sequence != stored.Sequence {
		t.Fatalf("recovered poll = items:%+v err:%v", items, err)
	}

	nextItem := item
	nextItem.TaskID = "task-2"
	nextItem.IdempotencyKey = "delivery-2"
	nextItem.CreatedAt = createdAt.Add(time.Second)
	nextStored, duplicate, err := repository.Enqueue(ctx, nextItem)
	if err != nil || duplicate || nextStored.Sequence <= stored.Sequence {
		t.Fatalf("post-restart enqueue = item:%+v duplicate:%v err:%v", nextStored, duplicate, err)
	}
	if err := repository.CancelTask(ctx, "task-2", "test cancellation", createdAt.Add(2*time.Minute)); err != nil {
		t.Fatalf("cancel task: %v", err)
	}

	if err := repository.Acknowledge(ctx, "target", stored.Sequence, createdAt.Add(time.Minute)); err != nil {
		t.Fatalf("acknowledge: %v", err)
	}
	if err := repository.Acknowledge(ctx, "target", stored.Sequence, createdAt.Add(2*time.Minute)); err != nil {
		t.Fatalf("repeated acknowledge: %v", err)
	}
	items, err = repository.Poll(ctx, "target", 0, 10)
	if err != nil || len(items) != 0 {
		t.Fatalf("poll after acknowledge = items:%+v err:%v", items, err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("final close database: %v", err)
	}
}

func TestRepositoryTransactionRollsBackAgentMutation(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatalf("Open returned an error: %v", err)
	}
	repository := NewRepository(database)
	err = repository.WithTransaction(ctx, func(txStore store.TxStore) error {
		if err := txStore.Agents().CreateAgent(ctx, testAgent("rolled-back", time.Now().UTC())); err != nil {
			return err
		}
		return errors.New("force rollback")
	})
	if err == nil {
		t.Fatal("transaction unexpectedly succeeded")
	}
	if _, err := repository.FindAgent(ctx, "rolled-back"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rolled back agent lookup error = %v, want ErrNotFound", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}
}

func TestRepositorySerializesConcurrentAgentWrites(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatalf("Open returned an error: %v", err)
	}
	defer database.Close()
	repository := NewRepository(database)

	var wait sync.WaitGroup
	errorsCh := make(chan error, 8)
	for i := 0; i < 8; i++ {
		i := i
		wait.Add(1)
		go func() {
			defer wait.Done()
			agent := testAgent("agent-"+string(rune('a'+i)), time.Now().UTC())
			errorsCh <- repository.CreateAgent(ctx, agent)
		}()
	}
	wait.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatalf("concurrent CreateAgent: %v", err)
		}
	}
	count, err := repository.CountAgents(ctx)
	if err != nil || count != 8 {
		t.Fatalf("CountAgents = count:%d err:%v", count, err)
	}
}

func TestRepositoryPersistsAndUpdatesPolicy(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatalf("Open returned an error: %v", err)
	}
	defer database.Close()
	repository := NewRepository(database)
	want := hub.HubPolicy{
		HubID:               "public",
		RegistrationEnabled: true,
		RegistrationTTL:     24 * time.Hour,
		PeerLease:           90 * time.Second,
		MaxRegisteredAgents: 100,
		MaxTasksPerMinute:   60,
		MaxConcurrentTasks:  4,
		MaxPayloadBytes:     1 << 20,
	}
	if err := repository.SavePolicy(ctx, want); err != nil {
		t.Fatalf("SavePolicy: %v", err)
	}
	got, err := repository.GetPolicy(ctx)
	if err != nil {
		t.Fatalf("GetPolicy: %v", err)
	}
	if got != want {
		t.Fatalf("GetPolicy = %+v, want %+v", got, want)
	}
	if err := repository.SetRegistrationEnabled(ctx, false); err != nil {
		t.Fatalf("SetRegistrationEnabled: %v", err)
	}
	got, err = repository.GetPolicy(ctx)
	if err != nil || got.RegistrationEnabled {
		t.Fatalf("policy after disable = %+v, err:%v", got, err)
	}
}

func testAgent(agentID string, createdAt time.Time) hub.RegisteredAgent {
	return hub.RegisteredAgent{
		HubID:               "public",
		AgentID:             agentID,
		RegistrationKeyHash: hub.HashToken("registration-" + agentID),
		TokenHash:           hub.HashToken("token-" + agentID),
		DisplayName:         agentID,
		ProviderFamily:      "test",
		TransportID:         "http-json",
		Capabilities:        []string{"text/plain"},
		State:               hub.AgentStatePending,
		ExpiresAt:           createdAt.Add(24 * time.Hour),
		CreatedAt:           createdAt,
	}
}
