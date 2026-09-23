package service

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tbdavid2019/888a2a-lite/internal/config"
	"github.com/tbdavid2019/888a2a-lite/internal/hub"
	"github.com/tbdavid2019/888a2a-lite/internal/store/sqlite"
)

func TestWorkflowHTTPQuorumAndOwnerIsolation(t *testing.T) {
	ctx := context.Background()
	database, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "workflow.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer func() { _ = database.Close() }()
	cfg := config.Config{HubID: "public", ListenAddr: ":0", DatabasePath: filepath.Join(t.TempDir(), "unused.db"), RegistrationEnabled: true, RegistrationTTL: time.Hour, PeerLease: time.Minute, MaxRegisteredAgents: 10, MaxTasksPerMinute: 50, MaxConcurrentTasks: 20, MaxPayloadBytes: 1 << 20, RegistrationPerMinute: 50}
	svc := New(sqlite.NewRepository(database), cfg)
	handler := NewHTTPServer(svc).Handler()
	owner := registerWorkflowTestAgent(t, ctx, svc, "workflow-owner")
	worker1 := registerWorkflowTestAgent(t, ctx, svc, "workflow-worker-1")
	worker2 := registerWorkflowTestAgent(t, ctx, svc, "workflow-worker-2")
	worker3 := registerWorkflowTestAgent(t, ctx, svc, "workflow-worker-3")
	observer := registerWorkflowTestAgent(t, ctx, svc, "workflow-observer")
	workflowID := "wf-quorum"
	createBody := WorkflowCreateInput{WorkflowID: workflowID, FlowType: "parallel", JoinPolicy: hub.WorkflowJoinQuorum, ExpectedSteps: 3, MinimumSuccesses: 2, RetryLimit: 1, Deadline: time.Now().UTC().Add(time.Hour), IdempotencyKey: "create-quorum"}
	created := doJSON(t, handler, http.MethodPost, "/hub/v1/workflows", owner.AgentID, owner.AgentToken, createBody)
	if created.Code != http.StatusCreated {
		t.Fatalf("create workflow = %d/%s", created.Code, created.Body.String())
	}
	duplicate := doJSON(t, handler, http.MethodPost, "/hub/v1/workflows", owner.AgentID, owner.AgentToken, createBody)
	if duplicate.Code != http.StatusOK || !strings.Contains(duplicate.Body.String(), `"duplicate":true`) {
		t.Fatalf("duplicate create = %d/%s", duplicate.Code, duplicate.Body.String())
	}
	workers := []hub.AgentIdentity{worker1, worker2, worker3}
	for index, worker := range workers {
		taskID := "wf-task-" + itoa(index+1)
		_, _, err := svc.SendTask(ctx, owner.AgentID, owner.AgentToken, hub.TaskDelivery{TargetAgentID: worker.AgentID, ContextID: workflowID, IdempotencyKey: "idem-" + taskID, Message: "work", TaskID: taskID})
		if err != nil {
			t.Fatalf("send task %s: %v", taskID, err)
		}
		registration := hub.WorkflowAttemptRegistration{StepID: "step-" + itoa(index+1), TargetAgentID: worker.AgentID, TaskID: taskID, Attempt: 1, IdempotencyKey: "attempt-" + taskID}
		registered := doJSON(t, handler, http.MethodPost, "/hub/v1/workflows/"+workflowID+"/steps", owner.AgentID, owner.AgentToken, registration)
		if registered.Code != http.StatusCreated {
			t.Fatalf("register step = %d/%s", registered.Code, registered.Body.String())
		}
	}
	for index, worker := range workers[:2] {
		outcomeURL := "/hub/v1/workflows/" + workflowID + "/steps/step-" + itoa(index+1) + "/attempts/1/outcome"
		outcome := doJSON(t, handler, http.MethodPost, outcomeURL, worker.AgentID, worker.AgentToken, map[string]any{"state": hub.WorkflowStepCompleted, "result": "done"})
		if outcome.Code != http.StatusOK {
			t.Fatalf("report outcome = %d/%s", outcome.Code, outcome.Body.String())
		}
	}
	duplicateOutcome := doJSON(t, handler, http.MethodPost, "/hub/v1/workflows/"+workflowID+"/steps/step-2/attempts/1/outcome", worker2.AgentID, worker2.AgentToken, map[string]any{"state": hub.WorkflowStepCompleted, "result": "done"})
	if duplicateOutcome.Code != http.StatusOK || !strings.Contains(duplicateOutcome.Body.String(), `"duplicate":true`) {
		t.Fatalf("replayed terminal outcome = %d/%s", duplicateOutcome.Code, duplicateOutcome.Body.String())
	}
	read := doJSON(t, handler, http.MethodGet, "/hub/v1/workflows/"+workflowID, owner.AgentID, owner.AgentToken, nil)
	var result hub.Workflow
	if err := json.NewDecoder(read.Body).Decode(&result); err != nil {
		t.Fatalf("decode workflow: %v", err)
	}
	if read.Code != http.StatusOK || result.State != hub.WorkflowCompleted || result.Counts.Completed != 2 || result.Counts.Canceled != 1 {
		t.Fatalf("quorum workflow = %d/%+v", read.Code, result)
	}
	forbidden := doJSON(t, handler, http.MethodGet, "/hub/v1/workflows/"+workflowID, observer.AgentID, observer.AgentToken, nil)
	if forbidden.Code != http.StatusNotFound {
		t.Fatalf("non-owner workflow read = %d/%s", forbidden.Code, forbidden.Body.String())
	}
	inbox, err := svc.Poll(ctx, worker3.AgentID, worker3.AgentToken, 0, 10)
	if err != nil || len(inbox) != 0 {
		t.Fatalf("quorum did not cancel pending delivery: items=%+v err=%v", inbox, err)
	}
}

func TestWorkflowHTTPRetryBudgetCreatesDeadLetter(t *testing.T) {
	ctx := context.Background()
	database, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "workflow-retry.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer func() { _ = database.Close() }()
	cfg := config.Config{HubID: "public", ListenAddr: ":0", DatabasePath: filepath.Join(t.TempDir(), "unused.db"), RegistrationEnabled: true, RegistrationTTL: time.Hour, PeerLease: time.Minute, MaxRegisteredAgents: 10, MaxTasksPerMinute: 50, MaxConcurrentTasks: 20, MaxPayloadBytes: 1 << 20, RegistrationPerMinute: 50}
	svc := New(sqlite.NewRepository(database), cfg)
	handler := NewHTTPServer(svc).Handler()
	owner := registerWorkflowTestAgent(t, ctx, svc, "retry-owner")
	worker := registerWorkflowTestAgent(t, ctx, svc, "retry-worker")
	workflowID := "wf-retry"
	created := doJSON(t, handler, http.MethodPost, "/hub/v1/workflows", owner.AgentID, owner.AgentToken, WorkflowCreateInput{WorkflowID: workflowID, FlowType: "pipeline", JoinPolicy: hub.WorkflowJoinAllSuccess, ExpectedSteps: 1, MinimumSuccesses: 1, RetryLimit: 1, Deadline: time.Now().UTC().Add(time.Hour), IdempotencyKey: "create-retry"})
	if created.Code != http.StatusCreated {
		t.Fatalf("create retry workflow = %d/%s", created.Code, created.Body.String())
	}
	for attempt := 1; attempt <= 2; attempt++ {
		taskID := "retry-task-" + itoa(attempt)
		_, _, err := svc.SendTask(ctx, owner.AgentID, owner.AgentToken, hub.TaskDelivery{TargetAgentID: worker.AgentID, ContextID: workflowID, IdempotencyKey: "idem-" + taskID, Message: "work", TaskID: taskID})
		if err != nil {
			t.Fatalf("send retry task: %v", err)
		}
		registration := hub.WorkflowAttemptRegistration{StepID: "step", TargetAgentID: worker.AgentID, TaskID: taskID, Attempt: attempt, IdempotencyKey: "attempt-" + taskID}
		registered := doJSON(t, handler, http.MethodPost, "/hub/v1/workflows/"+workflowID+"/steps", owner.AgentID, owner.AgentToken, registration)
		if registered.Code != http.StatusCreated {
			t.Fatalf("register attempt %d = %d/%s", attempt, registered.Code, registered.Body.String())
		}
		outcome := doJSON(t, handler, http.MethodPost, "/hub/v1/workflows/"+workflowID+"/steps/step/attempts/"+itoa(attempt)+"/outcome", worker.AgentID, worker.AgentToken, map[string]any{"state": hub.WorkflowStepFailed, "error": "worker failed"})
		if outcome.Code != http.StatusOK {
			t.Fatalf("report attempt %d = %d/%s", attempt, outcome.Code, outcome.Body.String())
		}
	}
	read := doJSON(t, handler, http.MethodGet, "/hub/v1/workflows/"+workflowID, owner.AgentID, owner.AgentToken, nil)
	var result hub.Workflow
	if err := json.NewDecoder(read.Body).Decode(&result); err != nil {
		t.Fatalf("decode dead-letter workflow: %v", err)
	}
	if read.Code != http.StatusOK || result.State != hub.WorkflowFailed || result.Counts.DeadLetter != 1 || result.Steps[0].Attempts[0].State != hub.WorkflowStepFailed {
		t.Fatalf("dead-letter workflow = %d/%+v", read.Code, result)
	}
}

func TestWorkflowHTTPAllowsAssignedParticipantToRegisterChildStep(t *testing.T) {
	ctx := context.Background()
	database, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "workflow-child.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer func() { _ = database.Close() }()
	cfg := config.Config{HubID: "public", ListenAddr: ":0", DatabasePath: filepath.Join(t.TempDir(), "unused.db"), RegistrationEnabled: true, RegistrationTTL: time.Hour, PeerLease: time.Minute, MaxRegisteredAgents: 10, MaxTasksPerMinute: 50, MaxConcurrentTasks: 20, MaxPayloadBytes: 1 << 20, RegistrationPerMinute: 50}
	svc := New(sqlite.NewRepository(database), cfg)
	handler := NewHTTPServer(svc).Handler()
	owner := registerWorkflowTestAgent(t, ctx, svc, "child-owner")
	worker1 := registerWorkflowTestAgent(t, ctx, svc, "child-worker-1")
	worker2 := registerWorkflowTestAgent(t, ctx, svc, "child-worker-2")
	workflowID := "wf-child"
	created := doJSON(t, handler, http.MethodPost, "/hub/v1/workflows", owner.AgentID, owner.AgentToken, WorkflowCreateInput{WorkflowID: workflowID, FlowType: "pipeline", JoinPolicy: hub.WorkflowJoinAllSuccess, ExpectedSteps: 2, MinimumSuccesses: 2, RetryLimit: 0, Deadline: time.Now().UTC().Add(time.Hour), IdempotencyKey: "create-child"})
	if created.Code != http.StatusCreated {
		t.Fatalf("create workflow = %d/%s", created.Code, created.Body.String())
	}
	firstTask := "child-task-1"
	if _, _, err := svc.SendTask(ctx, owner.AgentID, owner.AgentToken, hub.TaskDelivery{TargetAgentID: worker1.AgentID, ContextID: workflowID, IdempotencyKey: "idem-" + firstTask, Message: "first", TaskID: firstTask}); err != nil {
		t.Fatalf("send first task: %v", err)
	}
	firstRegistration := hub.WorkflowAttemptRegistration{StepID: "first", TargetAgentID: worker1.AgentID, TaskID: firstTask, Attempt: 1, IdempotencyKey: "attempt-first"}
	if response := doJSON(t, handler, http.MethodPost, "/hub/v1/workflows/"+workflowID+"/steps", owner.AgentID, owner.AgentToken, firstRegistration); response.Code != http.StatusCreated {
		t.Fatalf("register first step = %d/%s", response.Code, response.Body.String())
	}
	childTask := "child-task-2"
	if _, _, err := svc.SendTask(ctx, worker1.AgentID, worker1.AgentToken, hub.TaskDelivery{TargetAgentID: worker2.AgentID, ContextID: workflowID, IdempotencyKey: "idem-" + childTask, Message: "second", TaskID: childTask}); err != nil {
		t.Fatalf("send child task: %v", err)
	}
	childRegistration := hub.WorkflowAttemptRegistration{StepID: "second", TargetAgentID: worker2.AgentID, TaskID: childTask, Attempt: 1, IdempotencyKey: "attempt-second"}
	childResponse := doJSON(t, handler, http.MethodPost, "/hub/v1/workflows/"+workflowID+"/steps", worker1.AgentID, worker1.AgentToken, childRegistration)
	if childResponse.Code != http.StatusCreated || !strings.Contains(childResponse.Body.String(), `"stepId":"second"`) || strings.Contains(childResponse.Body.String(), `"steps"`) {
		t.Fatalf("participant child-step registration = %d/%s", childResponse.Code, childResponse.Body.String())
	}
}

func TestWorkflowHTTPCancelClosesPendingDeliveryAndRejectsLateOutcome(t *testing.T) {
	ctx := context.Background()
	database, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "workflow-cancel.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer func() { _ = database.Close() }()
	cfg := config.Config{HubID: "public", ListenAddr: ":0", DatabasePath: filepath.Join(t.TempDir(), "unused.db"), RegistrationEnabled: true, RegistrationTTL: time.Hour, PeerLease: time.Minute, MaxRegisteredAgents: 10, MaxTasksPerMinute: 50, MaxConcurrentTasks: 20, MaxPayloadBytes: 1 << 20, RegistrationPerMinute: 50}
	svc := New(sqlite.NewRepository(database), cfg)
	handler := NewHTTPServer(svc).Handler()
	owner := registerWorkflowTestAgent(t, ctx, svc, "cancel-owner")
	worker := registerWorkflowTestAgent(t, ctx, svc, "cancel-worker")
	workflowID := "wf-cancel"
	created := doJSON(t, handler, http.MethodPost, "/hub/v1/workflows", owner.AgentID, owner.AgentToken, WorkflowCreateInput{WorkflowID: workflowID, FlowType: "pipeline", JoinPolicy: hub.WorkflowJoinAllSuccess, ExpectedSteps: 1, MinimumSuccesses: 1, RetryLimit: 0, Deadline: time.Now().UTC().Add(time.Hour), IdempotencyKey: "create-cancel"})
	if created.Code != http.StatusCreated {
		t.Fatalf("create workflow = %d/%s", created.Code, created.Body.String())
	}
	taskID := "cancel-task"
	if _, _, err := svc.SendTask(ctx, owner.AgentID, owner.AgentToken, hub.TaskDelivery{TargetAgentID: worker.AgentID, ContextID: workflowID, IdempotencyKey: "idem-" + taskID, Message: "work", TaskID: taskID}); err != nil {
		t.Fatalf("send task: %v", err)
	}
	registration := hub.WorkflowAttemptRegistration{StepID: "only", TargetAgentID: worker.AgentID, TaskID: taskID, Attempt: 1, IdempotencyKey: "attempt-cancel"}
	registered := doJSON(t, handler, http.MethodPost, "/hub/v1/workflows/"+workflowID+"/steps", owner.AgentID, owner.AgentToken, registration)
	if registered.Code != http.StatusCreated {
		t.Fatalf("register step = %d/%s", registered.Code, registered.Body.String())
	}
	canceled := doJSON(t, handler, http.MethodPost, "/hub/v1/workflows/"+workflowID+"/cancel", owner.AgentID, owner.AgentToken, map[string]any{})
	if canceled.Code != http.StatusOK || !strings.Contains(canceled.Body.String(), string(hub.WorkflowCanceled)) {
		t.Fatalf("cancel workflow = %d/%s", canceled.Code, canceled.Body.String())
	}
	inbox, err := svc.Poll(ctx, worker.AgentID, worker.AgentToken, 0, 10)
	if err != nil || len(inbox) != 0 {
		t.Fatalf("cancel did not close pending delivery: items=%+v err=%v", inbox, err)
	}
	lateOutcome := doJSON(t, handler, http.MethodPost, "/hub/v1/workflows/"+workflowID+"/steps/only/attempts/1/outcome", worker.AgentID, worker.AgentToken, map[string]any{"state": hub.WorkflowStepCompleted, "result": "too late"})
	if lateOutcome.Code != http.StatusConflict {
		t.Fatalf("late outcome = %d/%s", lateOutcome.Code, lateOutcome.Body.String())
	}
}

func registerWorkflowTestAgent(t *testing.T, ctx context.Context, service *Service, name string) hub.AgentIdentity {
	t.Helper()
	identity, _, err := service.Register(ctx, hub.AgentDeclaration{DisplayName: name, ProviderFamily: "test", TransportID: "http-json", Capabilities: []string{"text/plain"}, RegistrationIdempotency: name})
	if err != nil {
		t.Fatalf("register workflow Agent %s: %v", name, err)
	}
	return identity
}
