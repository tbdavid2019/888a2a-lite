package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/tbdavid2019/888a2a-lite/internal/hub"
)

func TestWorkflowRepositoryPersistsAndDeduplicatesAcrossRestart(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "workflows.db")
	database, err := Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	repository := NewRepository(database)
	now := time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC)
	workflow := hub.Workflow{HubID: "hub", CircleID: "circle", WorkflowID: "wf-persist", OwnerAgentID: "owner", FlowType: "pipeline", JoinPolicy: hub.WorkflowJoinAllSuccess, ExpectedSteps: 1, MinimumSuccesses: 1, RetryLimit: 2, IdempotencyKey: "create-1", State: hub.WorkflowRunning, Deadline: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now, Counts: hub.WorkflowCounts{Expected: 1}, Steps: []hub.WorkflowStep{}}
	created, duplicate, err := repository.Workflows().CreateWorkflow(ctx, workflow)
	if err != nil || duplicate || created.WorkflowID != workflow.WorkflowID {
		t.Fatalf("CreateWorkflow() = (%+v, %v, %v)", created, duplicate, err)
	}
	if _, duplicate, err := repository.Workflows().CreateWorkflow(ctx, workflow); err != nil || !duplicate {
		t.Fatalf("duplicate CreateWorkflow() = duplicate %v, err %v", duplicate, err)
	}
	expiring := workflow
	expiring.WorkflowID = "wf-expire"
	expiring.IdempotencyKey = "create-expire"
	expiring.Deadline = now.Add(time.Second)
	if _, _, err := repository.Workflows().CreateWorkflow(ctx, expiring); err != nil {
		t.Fatalf("create expiring workflow: %v", err)
	}
	expired, err := repository.Workflows().FindWorkflow(ctx, "hub", "circle", "owner", expiring.WorkflowID, now.Add(2*time.Second))
	if err != nil || expired.State != hub.WorkflowTimedOut {
		t.Fatalf("FindWorkflow() at deadline = %+v, err %v", expired, err)
	}
	if err := repository.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	database, err = Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = database.Close() }()
	recovered, err := NewRepository(database).Workflows().FindWorkflow(ctx, "hub", "circle", "owner", "wf-persist", now)
	if err != nil || recovered.State != hub.WorkflowRunning || recovered.Deadline != workflow.Deadline {
		t.Fatalf("FindWorkflow() after restart = %+v, err %v", recovered, err)
	}
	recoveredExpired, err := NewRepository(database).Workflows().FindWorkflow(ctx, "hub", "circle", "owner", expiring.WorkflowID, now.Add(3*time.Second))
	if err != nil || recoveredExpired.State != hub.WorkflowTimedOut {
		t.Fatalf("expired workflow after restart = %+v, err %v", recoveredExpired, err)
	}
}
