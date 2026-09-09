package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/tbdavid2019/888a2a-lite/internal/a2a"
	"github.com/tbdavid2019/888a2a-lite/internal/hub"
	"github.com/tbdavid2019/888a2a-lite/internal/store"
)

func TestStandardTaskPersistsAcrossRestartAndCorrelatesUpdates(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "hub.db")
	database, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	repository := NewRepository(database)
	now := time.Now().UTC().Truncate(time.Microsecond)
	if err := repository.CreateAgent(ctx, testAgent("standard-sender", now)); err != nil {
		t.Fatalf("sender: %v", err)
	}
	if err := repository.CreateAgent(ctx, testAgent("standard-target", now)); err != nil {
		t.Fatalf("target: %v", err)
	}
	message := a2a.Message{MessageID: "standard-message-1", Role: "ROLE_USER", Parts: []a2a.Part{a2a.TextPart("hello")}}
	task := a2a.TaskRecord{HubID: "public", ID: "standard-task-1", CircleID: "public", RequesterAgentID: "standard-sender", TargetAgentID: "standard-target", ContextID: "standard-context", MessageID: message.MessageID, TurnID: "turn-1", Revision: 1, State: a2a.TaskStateSubmitted, Message: message, History: []a2a.Message{message}, ContentDigest: "digest-1", CreatedAt: now, UpdatedAt: now}
	item := hub.InboxItem{HubID: "public", CircleID: "public", TargetAgentID: task.TargetAgentID, RequesterAgentID: task.RequesterAgentID, TaskID: task.ID, ContextID: task.ContextID, IdempotencyKey: "a2a:" + message.MessageID, Message: "hello", Protocol: "A2A/1.0", MessageID: message.MessageID, TurnID: task.TurnID, TaskRevision: task.Revision, CreatedAt: now}
	created, duplicate, err := repository.CreateTaskWithDelivery(ctx, task, item)
	if err != nil || duplicate || created.MailboxSequence == 0 {
		t.Fatalf("create = %+v duplicate=%v err=%v", created, duplicate, err)
	}
	duplicateTask, duplicate, err := repository.CreateTaskWithDelivery(ctx, task, item)
	if err != nil || !duplicate || duplicateTask.ID != task.ID {
		t.Fatalf("duplicate = %+v duplicate=%v err=%v", duplicateTask, duplicate, err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	database, err = Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	repository = NewRepository(database)
	recovered, err := repository.FindTask(ctx, "public", "public", task.RequesterAgentID, task.ID)
	if err != nil || recovered.MailboxSequence != created.MailboxSequence || recovered.Message.MessageID != message.MessageID {
		t.Fatalf("recovered = %+v err=%v", recovered, err)
	}
	replyText := "hello back"
	reply := &a2a.Message{MessageID: "reply-1", ContextID: task.ContextID, TaskID: task.ID, Role: "ROLE_AGENT", Parts: []a2a.Part{a2a.TextPart(replyText)}}
	updated, duplicate, err := repository.ApplyUpdate(ctx, a2a.TaskUpdate{HubID: "public", TaskID: task.ID, TargetAgentID: task.TargetAgentID, UpdateID: "update-1", TurnID: task.TurnID, ExpectedRevision: 1, State: a2a.TaskStateCompleted, Message: reply, Artifacts: []a2a.Artifact{}})
	if err != nil || duplicate || updated.State != a2a.TaskStateCompleted || updated.Revision != 2 || updated.ResultMessage == nil {
		t.Fatalf("update = %+v duplicate=%v err=%v", updated, duplicate, err)
	}
	retried, duplicate, err := repository.ApplyUpdate(ctx, a2a.TaskUpdate{HubID: "public", TaskID: task.ID, TargetAgentID: task.TargetAgentID, UpdateID: "update-1", TurnID: task.TurnID, ExpectedRevision: 1, State: a2a.TaskStateCompleted, Message: reply, Artifacts: []a2a.Artifact{}})
	if err != nil || !duplicate || retried.Revision != updated.Revision {
		t.Fatalf("retry = %+v duplicate=%v err=%v", retried, duplicate, err)
	}
	_, _, err = repository.ApplyUpdate(ctx, a2a.TaskUpdate{HubID: "public", TaskID: task.ID, TargetAgentID: task.TargetAgentID, UpdateID: "update-1", TurnID: task.TurnID, ExpectedRevision: 1, State: a2a.TaskStateFailed})
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("changed duplicate error = %v, want conflict", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("final close: %v", err)
	}
}
