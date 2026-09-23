package hub

import (
	"testing"
	"time"
)

func TestWorkflowValidateBoundsAndPolicies(t *testing.T) {
	now := time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC)
	workflow := Workflow{HubID: "hub", CircleID: "circle", WorkflowID: "wf-1", OwnerAgentID: "owner", FlowType: "parallel", JoinPolicy: WorkflowJoinQuorum, ExpectedSteps: 3, MinimumSuccesses: 2, RetryLimit: 1, IdempotencyKey: "create-1", Deadline: now.Add(time.Hour)}
	if err := workflow.Validate(now); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	workflow.MinimumSuccesses = 0
	if err := workflow.Validate(now); err == nil {
		t.Fatal("Validate() accepted a zero quorum")
	}
	workflow.MinimumSuccesses = 2
	workflow.ExpectedSteps = MaxWorkflowSteps + 1
	if err := workflow.Validate(now); err == nil {
		t.Fatal("Validate() accepted too many workflow steps")
	}
}

func TestWorkflowRecomputeQuorumAndDeadLetter(t *testing.T) {
	now := time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC)
	workflow := Workflow{State: WorkflowRunning, JoinPolicy: WorkflowJoinQuorum, ExpectedSteps: 3, MinimumSuccesses: 2, Steps: []WorkflowStep{
		{StepID: "a", State: WorkflowStepCompleted},
		{StepID: "b", State: WorkflowStepCompleted},
		{StepID: "c", State: WorkflowStepSubmitted},
	}}
	workflow.Recompute(now)
	if workflow.State != WorkflowCompleted || workflow.Counts.Completed != 2 || workflow.Counts.Submitted != 1 {
		t.Fatalf("quorum result = %+v", workflow)
	}
	workflow = Workflow{State: WorkflowRunning, JoinPolicy: WorkflowJoinQuorum, ExpectedSteps: 3, MinimumSuccesses: 2, Steps: []WorkflowStep{
		{StepID: "a", State: WorkflowStepCompleted},
		{StepID: "b", State: WorkflowStepDeadLetter},
		{StepID: "c", State: WorkflowStepCanceled},
	}}
	workflow.Recompute(now)
	if workflow.State != WorkflowFailed || workflow.Counts.DeadLetter != 1 {
		t.Fatalf("impossible quorum result = %+v", workflow)
	}
}

func TestWorkflowExpiryAndCancellationCloseActiveSteps(t *testing.T) {
	now := time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC)
	workflow := Workflow{State: WorkflowRunning, Deadline: now.Add(-time.Second), Steps: []WorkflowStep{{StepID: "a", State: WorkflowStepWorking, Attempts: []WorkflowAttempt{{Attempt: 1, State: WorkflowStepWorking}}}}}
	if !workflow.Expire(now) || workflow.State != WorkflowTimedOut || workflow.Steps[0].State != WorkflowStepTimedOut {
		t.Fatalf("expired workflow = %+v", workflow)
	}
	workflow = Workflow{State: WorkflowRunning, Steps: []WorkflowStep{{StepID: "a", State: WorkflowStepSubmitted, Attempts: []WorkflowAttempt{{Attempt: 1, State: WorkflowStepSubmitted}}}}}
	if err := workflow.Cancel(now); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	if workflow.State != WorkflowCanceled || workflow.Steps[0].State != WorkflowStepCanceled || workflow.Steps[0].Attempts[0].State != WorkflowStepCanceled {
		t.Fatalf("canceled workflow = %+v", workflow)
	}
}
