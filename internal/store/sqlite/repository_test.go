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
	defer func() {
		if err := database.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	}()
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
	defer func() {
		if err := database.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	}()
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

func TestRepositoryPersistsSafeAuditEvents(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatalf("Open returned an error: %v", err)
	}
	defer func() {
		if err := database.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	}()
	repository := NewRepository(database)
	createdAt := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	if err := repository.AppendEvent(ctx, hub.Event{
		HubID: "public", Type: hub.EventAgentRegistered, ActorAgentID: "agent-1",
		Details: map[string]any{"count": 1}, CreatedAt: createdAt,
	}); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	events, err := repository.ListEvents(ctx, 0, 10)
	if err != nil || len(events) != 1 {
		t.Fatalf("ListEvents = events:%+v err:%v", events, err)
	}
	if events[0].ID == 0 || events[0].Type != hub.EventAgentRegistered || events[0].ActorAgentID != "agent-1" {
		t.Fatalf("event = %+v", events[0])
	}
}

func TestRepositoryPersistsAnnouncementLifecycle(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "hub.db")
	database, err := Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("Open returned an error: %v", err)
	}
	defer func() {
		if err := database.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	}()
	repository := NewRepository(database)
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	expires := now.Add(time.Hour)
	created, err := repository.CreateAnnouncement(ctx, hub.Announcement{
		HubID: "public", Revision: 1, Status: hub.AnnouncementPublished, Severity: hub.AnnouncementInfo,
		Title: "Initial", Summary: "Initial summary", PublishedAt: &now, ExpiresAt: &expires, CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("CreateAnnouncement: %v", err)
	}
	active, err := repository.ListActiveAnnouncements(ctx, 0, 10, now)
	if err != nil || len(active) != 1 || active[0].ID != created.ID {
		t.Fatalf("active announcements = %+v err:%v", active, err)
	}
	revision, err := repository.CreateRevision(ctx, created.ID, hub.AnnouncementInput{
		Title: "Revision", Summary: "Revised summary", Severity: hub.AnnouncementWarning,
	}, now.Add(time.Minute))
	if err != nil || revision.Revision != 2 || revision.Status != hub.AnnouncementDraft || revision.RevisionOfID == nil || *revision.RevisionOfID != created.ID {
		t.Fatalf("revision = %+v err:%v", revision, err)
	}
	updated, err := repository.UpdateDraft(ctx, revision.ID, hub.AnnouncementInput{
		Title: "Updated", Summary: "Updated summary", Severity: hub.AnnouncementCritical,
	}, now.Add(2*time.Minute))
	if err != nil || updated.Title != "Updated" || updated.Severity != hub.AnnouncementCritical {
		t.Fatalf("updated draft = %+v err:%v", updated, err)
	}
	if _, err := repository.CreateRevision(ctx, revision.ID, hub.AnnouncementInput{
		Title: "Invalid", Summary: "Invalid", Severity: hub.AnnouncementInfo,
	}, now); !errors.Is(err, store.ErrInvalidState) {
		t.Fatalf("draft revision error = %v, want ErrInvalidState", err)
	}
	published, err := repository.PublishAnnouncement(ctx, revision.ID, now.Add(3*time.Minute))
	if err != nil || published.Status != hub.AnnouncementPublished || published.PublishedAt == nil {
		t.Fatalf("published revision = %+v err:%v", published, err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close before reopen: %v", err)
	}
	database, err = Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("reopen returned an error: %v", err)
	}
	repository = NewRepository(database)
	all, err := repository.ListAnnouncements(ctx, 0, 10)
	if err != nil || len(all) != 2 || all[1].Title != "Updated" {
		t.Fatalf("announcements after reopen = %+v err:%v", all, err)
	}
}

func TestRepositoryPersistsGroupLifecycleAndFanout(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "hub.db")
	database, err := Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("Open returned an error: %v", err)
	}
	repository := NewRepository(database)
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	for _, agentID := range []string{"owner", "member", "other"} {
		if err := repository.CreateAgent(ctx, testAgent(agentID, now)); err != nil {
			t.Fatalf("CreateAgent(%q): %v", agentID, err)
		}
	}
	group, err := repository.Groups().CreateGroup(ctx, hub.Group{
		HubID: "public", GroupID: "group-1", Name: "team", State: hub.GroupStateActive,
		OwnerAgentID: "owner", CreatedAt: now,
	})
	if err != nil || group.GroupID != "group-1" {
		t.Fatalf("CreateGroup = %+v err:%v", group, err)
	}
	invitation, err := repository.Groups().CreateInvitation(ctx, hub.GroupInvitation{
		HubID: "public", GroupID: group.GroupID, InviterAgentID: "owner", InviteeAgentID: "member",
		State: hub.InvitationPending, CreatedAt: now, ExpiresAt: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("CreateInvitation: %v", err)
	}
	if _, err := repository.Groups().AcceptInvitation(ctx, invitation.ID, "member", now.Add(time.Minute)); err != nil {
		t.Fatalf("AcceptInvitation: %v", err)
	}
	members, err := repository.Groups().ListMembers(ctx, group.GroupID)
	if err != nil || len(members) != 2 {
		t.Fatalf("ListMembers = %+v err:%v", members, err)
	}

	message, duplicate, err := repository.Groups().SendGroupMessage(ctx, hub.GroupMessage{
		HubID: "public", GroupID: group.GroupID, SenderAgentID: "owner", ContextID: "ctx-1",
		IdempotencyKey: "message-1", Message: "hello group", CreatedAt: now.Add(2 * time.Minute),
	}, 32)
	if err != nil || duplicate || message.ID == 0 || len(message.Deliveries) != 1 || message.Deliveries[0].TargetAgentID != "member" {
		t.Fatalf("SendGroupMessage = %+v duplicate:%v err:%v", message, duplicate, err)
	}
	duplicateMessage, duplicate, err := repository.Groups().SendGroupMessage(ctx, hub.GroupMessage{
		HubID: "public", GroupID: group.GroupID, SenderAgentID: "owner", ContextID: "ctx-1",
		IdempotencyKey: "message-1", Message: "hello group", CreatedAt: now.Add(2 * time.Minute),
	}, 32)
	if err != nil || !duplicate || duplicateMessage.ID != message.ID {
		t.Fatalf("duplicate SendGroupMessage = %+v duplicate:%v err:%v", duplicateMessage, duplicate, err)
	}

	pending, err := repository.Poll(ctx, "member", 0, 10)
	if err != nil || len(pending) != 1 || pending[0].GroupID != group.GroupID || pending[0].GroupMessageID != message.ID {
		t.Fatalf("group poll = %+v err:%v", pending, err)
	}
	history, err := repository.Groups().ListGroupMessages(ctx, group.GroupID, "member", 0, 10)
	if err != nil || len(history) != 1 || history[0].ID != message.ID {
		t.Fatalf("group history = %+v err:%v", history, err)
	}
	if err := repository.Acknowledge(ctx, "member", pending[0].Sequence, now.Add(3*time.Minute)); err != nil {
		t.Fatalf("group acknowledge: %v", err)
	}

	second, _, err := repository.Groups().SendGroupMessage(ctx, hub.GroupMessage{
		HubID: "public", GroupID: group.GroupID, SenderAgentID: "owner", ContextID: "ctx-2",
		IdempotencyKey: "message-2", Message: "remove me", CreatedAt: now.Add(4 * time.Minute),
	}, 32)
	if err != nil {
		t.Fatalf("second group message: %v", err)
	}
	if err := repository.Groups().RemoveMember(ctx, group.GroupID, "member", now.Add(5*time.Minute)); err != nil {
		t.Fatalf("RemoveMember: %v", err)
	}
	if pending, err := repository.Poll(ctx, "member", 0, 10); err != nil || len(pending) != 0 {
		t.Fatalf("poll after member removal = %+v err:%v", pending, err)
	}
	if second.ID == 0 {
		t.Fatal("second message has no id")
	}

	if err := database.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}
	database, err = Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("reopen returned an error: %v", err)
	}
	repository = NewRepository(database)
	history, err = repository.Groups().ListGroupMessages(ctx, group.GroupID, "owner", 0, 10)
	if err != nil || len(history) != 2 || history[0].ID != message.ID || history[1].ID != second.ID {
		t.Fatalf("history after reopen = %+v err:%v", history, err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("final close database: %v", err)
	}
}

func TestRepositoryMigratesExistingDatabaseForGroups(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "hub.db")
	database, err := Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("initial Open: %v", err)
	}
	if _, err := database.SQL().ExecContext(ctx, "INSERT INTO hub_policy (hub_id, registration_enabled, registration_ttl_seconds, peer_lease_seconds, max_registered_agents, max_tasks_per_minute, max_concurrent_tasks, max_payload_bytes, updated_at) VALUES (?, 1, 86400, 90, 10, 20, 4, 1024, ?)", "legacy", time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("insert legacy policy: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close legacy database: %v", err)
	}
	database, err = Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("reopen legacy database: %v", err)
	}
	defer func() { _ = database.Close() }()
	for _, table := range []string{"agent_group", "group_member", "group_invitation", "group_message", "group_delivery"} {
		var name string
		if err := database.SQL().QueryRowContext(ctx, "SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&name); err != nil {
			t.Fatalf("find migrated table %q: %v", table, err)
		}
	}
	var maxMembers int
	if err := database.SQL().QueryRowContext(ctx, "SELECT max_group_members FROM hub_policy WHERE hub_id = ?", "legacy").Scan(&maxMembers); err != nil {
		t.Fatalf("read migrated group policy: %v", err)
	}
	if maxMembers != MaxGroupMembersDefault {
		t.Fatalf("migrated max_group_members = %d, want %d", maxMembers, MaxGroupMembersDefault)
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
