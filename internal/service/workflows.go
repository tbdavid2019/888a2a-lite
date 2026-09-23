package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/tbdavid2019/888a2a-lite/internal/hub"
	"github.com/tbdavid2019/888a2a-lite/internal/store"
)

const maxWorkflowLifetime = 24 * time.Hour

type WorkflowCreateInput struct {
	WorkflowID       string                 `json:"workflowId"`
	FlowType         string                 `json:"flowType"`
	JoinPolicy       hub.WorkflowJoinPolicy `json:"joinPolicy"`
	ExpectedSteps    int                    `json:"expectedSteps"`
	MinimumSuccesses int                    `json:"minimumSuccesses"`
	RetryLimit       int                    `json:"retryLimit"`
	Deadline         time.Time              `json:"deadline"`
	IdempotencyKey   string                 `json:"idempotencyKey"`
}

func (service *Service) CreateWorkflow(ctx context.Context, agentID, token string, input WorkflowCreateInput) (hub.Workflow, bool, error) {
	principal, err := service.AuthenticateAgentPrincipal(ctx, agentID, token)
	if err != nil {
		return hub.Workflow{}, false, err
	}
	now := service.now().UTC()
	workflow := hub.Workflow{
		HubID: service.config.HubID, CircleID: principal.CircleID, WorkflowID: strings.TrimSpace(input.WorkflowID),
		OwnerAgentID: principal.AgentID, FlowType: strings.TrimSpace(input.FlowType), JoinPolicy: input.JoinPolicy,
		ExpectedSteps: input.ExpectedSteps, MinimumSuccesses: input.MinimumSuccesses, RetryLimit: input.RetryLimit,
		Deadline: input.Deadline.UTC(), IdempotencyKey: strings.TrimSpace(input.IdempotencyKey),
		State: hub.WorkflowRunning, CreatedAt: now, UpdatedAt: now,
	}
	if err := workflow.Validate(now); err != nil {
		return hub.Workflow{}, false, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	if workflow.Deadline.After(now.Add(maxWorkflowLifetime)) {
		return hub.Workflow{}, false, fmt.Errorf("%w: workflow deadline may not exceed 24 hours", ErrValidation)
	}
	created, duplicate, err := service.store.Workflows().CreateWorkflow(ctx, workflow)
	if err != nil || !duplicate {
		return created, duplicate, err
	}
	refreshed, err := service.store.Workflows().FindWorkflow(ctx, service.config.HubID, principal.CircleID, principal.AgentID, workflow.WorkflowID, now)
	return refreshed, duplicate, err
}

func (service *Service) ListWorkflows(ctx context.Context, agentID, token string, limit, offset int) ([]hub.Workflow, int, error) {
	principal, err := service.AuthenticateAgentPrincipal(ctx, agentID, token)
	if err != nil {
		return nil, 0, err
	}
	if limit < 1 || limit > 100 || offset < 0 {
		return nil, 0, fmt.Errorf("%w: workflow page size must be between 1 and 100", ErrValidation)
	}
	return service.store.Workflows().ListWorkflows(ctx, service.config.HubID, principal.CircleID, principal.AgentID, service.now().UTC(), limit, offset)
}

func (service *Service) GetWorkflow(ctx context.Context, agentID, token, workflowID string) (hub.Workflow, error) {
	principal, err := service.AuthenticateAgentPrincipal(ctx, agentID, token)
	if err != nil {
		return hub.Workflow{}, err
	}
	workflow, err := service.store.Workflows().FindWorkflow(ctx, service.config.HubID, principal.CircleID, principal.AgentID, workflowID, service.now().UTC())
	if errors.Is(err, store.ErrNotFound) {
		return hub.Workflow{}, store.ErrNotFound
	}
	return workflow, err
}

func (service *Service) RegisterWorkflowAttempt(ctx context.Context, agentID, token, workflowID string, registration hub.WorkflowAttemptRegistration) (hub.Workflow, bool, error) {
	principal, err := service.AuthenticateAgentPrincipal(ctx, agentID, token)
	if err != nil {
		return hub.Workflow{}, false, err
	}
	if strings.TrimSpace(registration.StepID) == "" || len(registration.StepID) > 128 || strings.ContainsAny(registration.StepID, "/?#\r\n") || strings.TrimSpace(registration.TaskID) == "" || len(registration.TaskID) > 128 || strings.TrimSpace(registration.TargetAgentID) == "" || len(registration.TargetAgentID) > 128 || strings.TrimSpace(registration.IdempotencyKey) == "" || len(registration.IdempotencyKey) > 128 || registration.Attempt < 1 {
		return hub.Workflow{}, false, fmt.Errorf("%w: workflow step registration is invalid", ErrValidation)
	}
	return service.store.Workflows().RegisterWorkflowAttempt(ctx, service.config.HubID, principal.CircleID, principal.AgentID, workflowID, registration, service.now().UTC())
}

func (service *Service) ReportWorkflowOutcome(ctx context.Context, agentID, token, workflowID string, outcome hub.WorkflowOutcome) (hub.Workflow, bool, error) {
	principal, err := service.AuthenticateAgentPrincipal(ctx, agentID, token)
	if err != nil {
		return hub.Workflow{}, false, err
	}
	if strings.TrimSpace(outcome.StepID) == "" || len(outcome.StepID) > 128 || strings.ContainsAny(outcome.StepID, "/?#\r\n") || outcome.Attempt < 1 || len(outcome.Result) > hub.MaxWorkflowResultBytes || len(outcome.Error) > hub.MaxWorkflowErrorBytes || (outcome.State != hub.WorkflowStepWorking && outcome.State != hub.WorkflowStepCompleted && outcome.State != hub.WorkflowStepFailed && outcome.State != hub.WorkflowStepCanceled) {
		return hub.Workflow{}, false, fmt.Errorf("%w: workflow outcome is invalid or exceeds its size limit", ErrValidation)
	}
	outcome.TargetAgentID = principal.AgentID
	return service.store.Workflows().ReportWorkflowOutcome(ctx, service.config.HubID, principal.CircleID, workflowID, outcome, service.now().UTC())
}

func (service *Service) CancelWorkflow(ctx context.Context, agentID, token, workflowID string) (hub.Workflow, error) {
	principal, err := service.AuthenticateAgentPrincipal(ctx, agentID, token)
	if err != nil {
		return hub.Workflow{}, err
	}
	return service.store.Workflows().CancelWorkflow(ctx, service.config.HubID, principal.CircleID, principal.AgentID, workflowID, service.now().UTC())
}
