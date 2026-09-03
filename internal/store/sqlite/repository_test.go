package sqlite

import (
	"context"
	"database/sql"
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
	createdAt := time.Now().UTC().Truncate(time.Second)
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
	createdAt := time.Now().UTC().Truncate(time.Second)
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
	now := time.Now().UTC().Truncate(time.Second)
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
	now := time.Now().UTC().Truncate(time.Second)
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
	legacy, err := sql.Open("sqlite", sqliteDSN(databasePath))
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	_, err = legacy.ExecContext(ctx, `
CREATE TABLE hub_policy (
    hub_id TEXT PRIMARY KEY NOT NULL,
    registration_enabled INTEGER NOT NULL,
    registration_ttl_seconds INTEGER NOT NULL,
    peer_lease_seconds INTEGER NOT NULL,
    max_registered_agents INTEGER NOT NULL,
    max_tasks_per_minute INTEGER NOT NULL,
    max_concurrent_tasks INTEGER NOT NULL,
    max_payload_bytes INTEGER NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE TABLE agent (
    hub_id TEXT NOT NULL, agent_id TEXT NOT NULL, registration_key_hash TEXT NOT NULL,
    token_hash TEXT NOT NULL, display_name TEXT NOT NULL, provider_family TEXT NOT NULL,
    transport_id TEXT NOT NULL, capabilities_json TEXT NOT NULL, agent_card_json TEXT NOT NULL DEFAULT '',
    automatic_execution INTEGER NOT NULL DEFAULT 0, state TEXT NOT NULL, last_seen_at TEXT,
    expires_at TEXT NOT NULL, lease_expires_at TEXT, created_at TEXT NOT NULL, revoked_at TEXT,
    revoke_reason TEXT NOT NULL DEFAULT '', PRIMARY KEY (hub_id, agent_id)
);
CREATE TABLE inbox_item (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT, hub_id TEXT NOT NULL, target_agent_id TEXT NOT NULL,
    requester_agent_id TEXT NOT NULL, task_id TEXT NOT NULL, context_id TEXT NOT NULL,
    idempotency_key TEXT NOT NULL, message TEXT NOT NULL, state TEXT NOT NULL,
    created_at TEXT NOT NULL, acknowledged_at TEXT, canceled_at TEXT,
    cancel_reason TEXT NOT NULL DEFAULT '', UNIQUE (hub_id, target_agent_id, requester_agent_id, idempotency_key)
);
CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL);
INSERT INTO hub_policy VALUES ('legacy', 1, 86400, 90, 10, 20, 4, 1024, ?);
INSERT INTO schema_migrations VALUES (2, ?);`, time.Now().UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatalf("create legacy schema: %v", err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatalf("close legacy database: %v", err)
	}
	database, err := Open(ctx, databasePath)
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

func TestRepositoryListMessagesAdmin(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = database.Close() }()
	repo := NewRepository(database)
	now := time.Now().UTC().Truncate(time.Second)
	for _, id := range []string{"s-agent", "t-agent"} {
		if err := repo.CreateAgent(ctx, testAgent(id, now)); err != nil {
			t.Fatalf("CreateAgent: %v", err)
		}
	}

	// Enqueue direct tasks
	task1 := hub.InboxItem{
		HubID: "public", TargetAgentID: "t-agent", RequesterAgentID: "s-agent",
		TaskID: "task-adm-1", ContextID: "ctx-1", IdempotencyKey: "key-1",
		Message: "hello direct", CreatedAt: now,
	}
	if _, _, err := repo.Enqueue(ctx, task1); err != nil {
		t.Fatalf("Enqueue task1: %v", err)
	}
	task2 := hub.InboxItem{
		HubID: "public", TargetAgentID: "t-agent", RequesterAgentID: "s-agent",
		TaskID: "task-adm-2", ContextID: "ctx-2", IdempotencyKey: "key-2",
		Message: "second direct", CreatedAt: now.Add(time.Second),
	}
	if _, _, err := repo.Enqueue(ctx, task2); err != nil {
		t.Fatalf("Enqueue task2: %v", err)
	}

	directs, err := repo.ListDirectMessagesAdmin(ctx, 0, 10, "")
	if err != nil {
		t.Fatalf("ListDirectMessagesAdmin: %v", err)
	}
	if len(directs) != 2 {
		t.Fatalf("expected 2 direct messages, got %d", len(directs))
	}
	if directs[0].TaskID != "task-adm-2" {
		t.Fatalf("expected newest task-adm-2 first, got %s", directs[0].TaskID)
	}

	// Filter by agentID
	filtered, err := repo.ListDirectMessagesAdmin(ctx, 0, 10, "t-agent")
	if err != nil || len(filtered) != 2 {
		t.Fatalf("filter by t-agent: len=%d err=%v", len(filtered), err)
	}
	filteredEmpty, err := repo.ListDirectMessagesAdmin(ctx, 0, 10, "unknown-agent")
	if err != nil || len(filteredEmpty) != 0 {
		t.Fatalf("filter by unknown: len=%d err=%v", len(filteredEmpty), err)
	}

	// Create group and group message
	grp, err := repo.Groups().CreateGroup(ctx, hub.Group{
		HubID: "public", GroupID: "g-adm", Name: "adm-group", State: hub.GroupStateActive,
		OwnerAgentID: "s-agent", CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	inv, err := repo.Groups().CreateInvitation(ctx, hub.GroupInvitation{
		HubID: "public", GroupID: grp.GroupID, InviterAgentID: "s-agent", InviteeAgentID: "t-agent",
		State: hub.InvitationPending, CreatedAt: now, ExpiresAt: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("CreateInvitation: %v", err)
	}
	if _, err := repo.Groups().AcceptInvitation(ctx, inv.ID, "t-agent", now.Add(time.Minute)); err != nil {
		t.Fatalf("AcceptInvitation: %v", err)
	}

	msg, _, err := repo.Groups().SendGroupMessage(ctx, hub.GroupMessage{
		HubID: "public", GroupID: grp.GroupID, SenderAgentID: "s-agent", ContextID: "ctx-grp",
		IdempotencyKey: "grp-msg-1", Message: "hello group admin", CreatedAt: now.Add(2 * time.Minute),
	}, 10)
	if err != nil {
		t.Fatalf("SendGroupMessage: %v", err)
	}

	grpMsgs, err := repo.Groups().ListGroupMessagesAdmin(ctx, 0, 10, "g-adm", "")
	if err != nil {
		t.Fatalf("ListGroupMessagesAdmin: %v", err)
	}
	if len(grpMsgs) != 1 || grpMsgs[0].ID != msg.ID {
		t.Fatalf("expected 1 group message with id %d, got %+v", msg.ID, grpMsgs)
	}
	if len(grpMsgs[0].Deliveries) != 1 || grpMsgs[0].Deliveries[0].TargetAgentID != "t-agent" {
		t.Fatalf("expected deliveries for t-agent, got %+v", grpMsgs[0].Deliveries)
	}
}
