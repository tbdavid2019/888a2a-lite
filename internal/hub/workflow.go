package hub

import (
	"fmt"
	"strings"
	"time"
)

type WorkflowState string
type WorkflowStepState string
type WorkflowJoinPolicy string

const (
	WorkflowRunning   WorkflowState = "RUNNING"
	WorkflowCompleted WorkflowState = "COMPLETED"
	WorkflowFailed    WorkflowState = "FAILED"
	WorkflowCanceled  WorkflowState = "CANCELED"
	WorkflowTimedOut  WorkflowState = "TIMED_OUT"

	WorkflowStepSubmitted  WorkflowStepState = "SUBMITTED"
	WorkflowStepWorking    WorkflowStepState = "WORKING"
	WorkflowStepCompleted  WorkflowStepState = "COMPLETED"
	WorkflowStepFailed     WorkflowStepState = "FAILED"
	WorkflowStepCanceled   WorkflowStepState = "CANCELED"
	WorkflowStepTimedOut   WorkflowStepState = "TIMED_OUT"
	WorkflowStepDeadLetter WorkflowStepState = "DEAD_LETTER"

	WorkflowJoinAllSuccess     WorkflowJoinPolicy = "ALL_SUCCESS"
	WorkflowJoinQuorum         WorkflowJoinPolicy = "QUORUM"
	WorkflowJoinFirstSuccess   WorkflowJoinPolicy = "FIRST_SUCCESS"
	WorkflowJoinPartialFailure WorkflowJoinPolicy = "PARTIAL_FAILURE"
)

const (
	MaxWorkflowSteps       = 32
	MaxActiveWorkflows     = 100
	MaxWorkflowRetries     = 8
	MaxWorkflowResultBytes = 2048
	MaxWorkflowErrorBytes  = 1024
)

type Workflow struct {
	HubID            string             `json:"hubId"`
	CircleID         string             `json:"circleId"`
	WorkflowID       string             `json:"workflowId"`
	OwnerAgentID     string             `json:"ownerAgentId"`
	FlowType         string             `json:"flowType"`
	JoinPolicy       WorkflowJoinPolicy `json:"joinPolicy"`
	ExpectedSteps    int                `json:"expectedSteps"`
	MinimumSuccesses int                `json:"minimumSuccesses"`
	RetryLimit       int                `json:"retryLimit"`
	IdempotencyKey   string             `json:"idempotencyKey"`
	State            WorkflowState      `json:"state"`
	Deadline         time.Time          `json:"deadline"`
	Steps            []WorkflowStep     `json:"steps"`
	Counts           WorkflowCounts     `json:"counts"`
	CreatedAt        time.Time          `json:"createdAt"`
	UpdatedAt        time.Time          `json:"updatedAt"`
	CompletedAt      *time.Time         `json:"completedAt,omitempty"`
	CanceledAt       *time.Time         `json:"canceledAt,omitempty"`
}

type WorkflowCounts struct {
	Expected   int `json:"expected"`
	Submitted  int `json:"submitted"`
	Working    int `json:"working"`
	Completed  int `json:"completed"`
	Failed     int `json:"failed"`
	Canceled   int `json:"canceled"`
	TimedOut   int `json:"timedOut"`
	DeadLetter int `json:"deadLetter"`
}

type WorkflowStep struct {
	StepID        string            `json:"stepId"`
	TargetAgentID string            `json:"targetAgentId"`
	TaskID        string            `json:"taskId"`
	State         WorkflowStepState `json:"state"`
	Attempt       int               `json:"attempt"`
	Attempts      []WorkflowAttempt `json:"attempts"`
	UpdatedAt     time.Time         `json:"updatedAt"`
}

type WorkflowAttempt struct {
	Attempt        int               `json:"attempt"`
	TargetAgentID  string            `json:"targetAgentId"`
	TaskID         string            `json:"taskId"`
	IdempotencyKey string            `json:"idempotencyKey"`
	State          WorkflowStepState `json:"state"`
	Result         string            `json:"result,omitempty"`
	Error          string            `json:"error,omitempty"`
	CreatedAt      time.Time         `json:"createdAt"`
	UpdatedAt      time.Time         `json:"updatedAt"`
}

type WorkflowAttemptRegistration struct {
	StepID         string `json:"stepId"`
	TargetAgentID  string `json:"targetAgentId"`
	TaskID         string `json:"taskId"`
	Attempt        int    `json:"attempt"`
	IdempotencyKey string `json:"idempotencyKey"`
}

type WorkflowOutcome struct {
	StepID        string            `json:"stepId"`
	Attempt       int               `json:"attempt"`
	TargetAgentID string            `json:"-"`
	State         WorkflowStepState `json:"state"`
	Result        string            `json:"result,omitempty"`
	Error         string            `json:"error,omitempty"`
}

func (workflow Workflow) Validate(now time.Time) error {
	if strings.TrimSpace(workflow.WorkflowID) == "" || len(workflow.WorkflowID) > 128 || strings.ContainsAny(workflow.WorkflowID, "/?#\r\n") {
		return fmt.Errorf("workflowId must contain 1 to 128 characters")
	}
	if strings.TrimSpace(workflow.OwnerAgentID) == "" || strings.TrimSpace(workflow.CircleID) == "" || strings.TrimSpace(workflow.HubID) == "" {
		return fmt.Errorf("workflow owner and scope are required")
	}
	switch workflow.FlowType {
	case "sequential", "pipeline", "parallel", "supervisor", "debate":
	default:
		return fmt.Errorf("flowType is unsupported")
	}
	if strings.TrimSpace(workflow.IdempotencyKey) == "" || len(workflow.IdempotencyKey) > 128 {
		return fmt.Errorf("idempotencyKey must contain 1 to 128 characters")
	}
	if workflow.JoinPolicy != WorkflowJoinAllSuccess && workflow.JoinPolicy != WorkflowJoinQuorum && workflow.JoinPolicy != WorkflowJoinFirstSuccess && workflow.JoinPolicy != WorkflowJoinPartialFailure {
		return fmt.Errorf("joinPolicy is unsupported")
	}
	if workflow.ExpectedSteps < 1 || workflow.ExpectedSteps > MaxWorkflowSteps {
		return fmt.Errorf("expectedSteps must be between 1 and %d", MaxWorkflowSteps)
	}
	if workflow.MinimumSuccesses < 1 || workflow.MinimumSuccesses > workflow.ExpectedSteps {
		return fmt.Errorf("minimumSuccesses must be between 1 and expectedSteps")
	}
	if workflow.JoinPolicy == WorkflowJoinFirstSuccess && workflow.MinimumSuccesses != 1 {
		return fmt.Errorf("FIRST_SUCCESS requires minimumSuccesses=1")
	}
	if workflow.JoinPolicy == WorkflowJoinAllSuccess && workflow.MinimumSuccesses != workflow.ExpectedSteps {
		return fmt.Errorf("ALL_SUCCESS requires minimumSuccesses=expectedSteps")
	}
	if workflow.RetryLimit < 0 || workflow.RetryLimit > MaxWorkflowRetries {
		return fmt.Errorf("retryLimit must be between 0 and %d", MaxWorkflowRetries)
	}
	if workflow.Deadline.IsZero() || !workflow.Deadline.After(now) {
		return fmt.Errorf("deadline must be in the future")
	}
	return nil
}

func (workflow *Workflow) Recompute(now time.Time) {
	if workflow.State != WorkflowRunning {
		workflow.updateCounts()
		return
	}
	workflow.updateCounts()
	successes := workflow.Counts.Completed
	possible := workflow.ExpectedSteps - workflow.Counts.DeadLetter - workflow.Counts.Canceled - workflow.Counts.TimedOut
	switch workflow.JoinPolicy {
	case WorkflowJoinAllSuccess:
		if workflow.Counts.DeadLetter+workflow.Counts.Canceled+workflow.Counts.TimedOut > 0 {
			workflow.State = WorkflowFailed
		} else if successes == workflow.ExpectedSteps {
			workflow.State = WorkflowCompleted
		}
	case WorkflowJoinQuorum, WorkflowJoinFirstSuccess:
		if successes >= workflow.MinimumSuccesses {
			workflow.State = WorkflowCompleted
		} else if possible < workflow.MinimumSuccesses {
			workflow.State = WorkflowFailed
		}
	case WorkflowJoinPartialFailure:
		if len(workflow.Steps) == workflow.ExpectedSteps && workflow.Counts.Submitted+workflow.Counts.Working+workflow.Counts.Failed == 0 {
			if successes >= workflow.MinimumSuccesses {
				workflow.State = WorkflowCompleted
			} else {
				workflow.State = WorkflowFailed
			}
		}
	}
	if workflow.State != WorkflowRunning && workflow.CompletedAt == nil {
		completedAt := now.UTC()
		workflow.CompletedAt = &completedAt
	}
	workflow.UpdatedAt = now.UTC()
	workflow.updateCounts()
}

func (workflow *Workflow) Expire(now time.Time) bool {
	if workflow.State != WorkflowRunning || workflow.Deadline.After(now) {
		return false
	}
	workflow.State = WorkflowTimedOut
	completedAt := now.UTC()
	workflow.CompletedAt = &completedAt
	workflow.UpdatedAt = now.UTC()
	for stepIndex := range workflow.Steps {
		step := &workflow.Steps[stepIndex]
		if step.State == WorkflowStepSubmitted || step.State == WorkflowStepWorking || step.State == WorkflowStepFailed {
			step.State = WorkflowStepTimedOut
			step.UpdatedAt = now.UTC()
			if len(step.Attempts) > 0 {
				last := &step.Attempts[len(step.Attempts)-1]
				if last.State == WorkflowStepSubmitted || last.State == WorkflowStepWorking {
					last.State = WorkflowStepTimedOut
					last.UpdatedAt = now.UTC()
				}
			}
		}
	}
	workflow.updateCounts()
	return true
}

func (workflow *Workflow) Cancel(now time.Time) error {
	if workflow.State != WorkflowRunning {
		return fmt.Errorf("workflow is terminal")
	}
	workflow.State = WorkflowCanceled
	now = now.UTC()
	workflow.CanceledAt = &now
	workflow.CompletedAt = &now
	workflow.UpdatedAt = now
	for stepIndex := range workflow.Steps {
		step := &workflow.Steps[stepIndex]
		if step.State == WorkflowStepSubmitted || step.State == WorkflowStepWorking || step.State == WorkflowStepFailed {
			step.State = WorkflowStepCanceled
			step.UpdatedAt = now
			if len(step.Attempts) > 0 {
				last := &step.Attempts[len(step.Attempts)-1]
				if last.State == WorkflowStepSubmitted || last.State == WorkflowStepWorking {
					last.State = WorkflowStepCanceled
					last.UpdatedAt = now
				}
			}
		}
	}
	workflow.updateCounts()
	return nil
}

func (workflow *Workflow) CancelActiveSteps(now time.Time) {
	now = now.UTC()
	for stepIndex := range workflow.Steps {
		step := &workflow.Steps[stepIndex]
		if step.State != WorkflowStepSubmitted && step.State != WorkflowStepWorking && step.State != WorkflowStepFailed {
			continue
		}
		step.State = WorkflowStepCanceled
		step.UpdatedAt = now
		if len(step.Attempts) == 0 {
			continue
		}
		last := &step.Attempts[len(step.Attempts)-1]
		if last.State == WorkflowStepSubmitted || last.State == WorkflowStepWorking {
			last.State = WorkflowStepCanceled
			last.UpdatedAt = now
		}
	}
	workflow.UpdatedAt = now
	workflow.updateCounts()
}

func (workflow *Workflow) updateCounts() {
	counts := WorkflowCounts{Expected: workflow.ExpectedSteps}
	for _, step := range workflow.Steps {
		switch step.State {
		case WorkflowStepSubmitted:
			counts.Submitted++
		case WorkflowStepWorking:
			counts.Working++
		case WorkflowStepCompleted:
			counts.Completed++
		case WorkflowStepFailed:
			counts.Failed++
		case WorkflowStepCanceled:
			counts.Canceled++
		case WorkflowStepTimedOut:
			counts.TimedOut++
		case WorkflowStepDeadLetter:
			counts.DeadLetter++
		}
	}
	workflow.Counts = counts
}

func IsTerminalWorkflowState(state WorkflowState) bool {
	return state == WorkflowCompleted || state == WorkflowFailed || state == WorkflowCanceled || state == WorkflowTimedOut
}
