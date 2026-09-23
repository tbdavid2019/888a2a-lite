package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/tbdavid2019/888a2a-lite/internal/hub"
	"github.com/tbdavid2019/888a2a-lite/internal/store"
)

func migrateWorkflows(ctx context.Context, database *sql.DB) error {
	var applied int
	if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version = 10").Scan(&applied); err != nil {
		return err
	}
	if applied > 0 {
		return nil
	}
	statements := []string{
		`CREATE TABLE agent_workflow (
    hub_id TEXT NOT NULL,
    circle_id TEXT NOT NULL,
    owner_agent_id TEXT NOT NULL,
    workflow_id TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    request_digest TEXT NOT NULL,
    state TEXT NOT NULL,
    deadline TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    workflow_json TEXT NOT NULL,
    PRIMARY KEY (hub_id, circle_id, workflow_id),
    UNIQUE (hub_id, circle_id, owner_agent_id, idempotency_key)
)`,
		`CREATE INDEX idx_agent_workflow_owner ON agent_workflow (hub_id, circle_id, owner_agent_id, created_at DESC)`,
		`CREATE INDEX idx_agent_workflow_owner_active ON agent_workflow (hub_id, circle_id, owner_agent_id, state, deadline)`,
		`CREATE INDEX idx_agent_workflow_deadline ON agent_workflow (hub_id, state, deadline)`,
		`INSERT INTO schema_migrations (version, applied_at) VALUES (10, CURRENT_TIMESTAMP)`,
	}
	for _, statement := range statements {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("migrate workflows: %w", err)
		}
	}
	return nil
}

func workflowRequestDigest(workflow hub.Workflow) string {
	payload := struct {
		HubID            string                 `json:"hubId"`
		CircleID         string                 `json:"circleId"`
		WorkflowID       string                 `json:"workflowId"`
		OwnerAgentID     string                 `json:"ownerAgentId"`
		FlowType         string                 `json:"flowType"`
		JoinPolicy       hub.WorkflowJoinPolicy `json:"joinPolicy"`
		ExpectedSteps    int                    `json:"expectedSteps"`
		MinimumSuccesses int                    `json:"minimumSuccesses"`
		RetryLimit       int                    `json:"retryLimit"`
		IdempotencyKey   string                 `json:"idempotencyKey"`
		Deadline         time.Time              `json:"deadline"`
	}{workflow.HubID, workflow.CircleID, workflow.WorkflowID, workflow.OwnerAgentID, workflow.FlowType, workflow.JoinPolicy, workflow.ExpectedSteps, workflow.MinimumSuccesses, workflow.RetryLimit, workflow.IdempotencyKey, workflow.Deadline.UTC()}
	encoded, _ := json.Marshal(payload)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func (repository *Repository) CreateWorkflow(ctx context.Context, workflow hub.Workflow) (hub.Workflow, bool, error) {
	var result hub.Workflow
	duplicate := false
	err := repository.withTransaction(ctx, func(tx *Repository) error {
		var existingJSON, existingDigest string
		err := tx.executor().QueryRowContext(ctx, `SELECT workflow_json, request_digest FROM agent_workflow
WHERE hub_id = ? AND circle_id = ? AND owner_agent_id = ? AND idempotency_key = ?`, workflow.HubID, workflow.CircleID, workflow.OwnerAgentID, workflow.IdempotencyKey).Scan(&existingJSON, &existingDigest)
		if err == nil {
			if existingDigest != workflowRequestDigest(workflow) {
				return store.ErrConflict
			}
			if err := json.Unmarshal([]byte(existingJSON), &result); err != nil {
				return fmt.Errorf("decode existing workflow: %w", err)
			}
			duplicate = true
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var workflowCount int
		if err := tx.executor().QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_workflow WHERE hub_id=? AND circle_id=? AND workflow_id=?`, workflow.HubID, workflow.CircleID, workflow.WorkflowID).Scan(&workflowCount); err != nil {
			return err
		}
		if workflowCount != 0 {
			return store.ErrConflict
		}
		var activeCount int
		if err := tx.executor().QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_workflow WHERE hub_id=? AND circle_id=? AND owner_agent_id=? AND state=? AND deadline>?`, workflow.HubID, workflow.CircleID, workflow.OwnerAgentID, string(hub.WorkflowRunning), formatTime(workflow.CreatedAt)).Scan(&activeCount); err != nil {
			return err
		}
		if activeCount >= hub.MaxActiveWorkflows {
			return store.ErrLimitReached
		}
		workflow.State = hub.WorkflowRunning
		workflow.Steps = []hub.WorkflowStep{}
		workflow.Counts = hub.WorkflowCounts{Expected: workflow.ExpectedSteps}
		encoded, err := json.Marshal(workflow)
		if err != nil {
			return err
		}
		_, err = tx.executor().ExecContext(ctx, `INSERT INTO agent_workflow
(hub_id,circle_id,owner_agent_id,workflow_id,idempotency_key,request_digest,state,deadline,created_at,updated_at,workflow_json)
VALUES (?,?,?,?,?,?,?,?,?,?,?)`, workflow.HubID, workflow.CircleID, workflow.OwnerAgentID, workflow.WorkflowID, workflow.IdempotencyKey, workflowRequestDigest(workflow), string(workflow.State), formatTime(workflow.Deadline), formatTime(workflow.CreatedAt), formatTime(workflow.UpdatedAt), string(encoded))
		if err != nil {
			return err
		}
		result = workflow
		return nil
	})
	return result, duplicate, err
}

func (repository *Repository) FindWorkflow(ctx context.Context, hubID, circleID, ownerAgentID, workflowID string, now time.Time) (hub.Workflow, error) {
	var result hub.Workflow
	err := repository.withTransaction(ctx, func(tx *Repository) error {
		workflow, err := tx.loadWorkflow(ctx, hubID, circleID, ownerAgentID, workflowID)
		if err != nil {
			return err
		}
		if workflow.Expire(now) {
			if err := tx.cancelPendingWorkflowDeliveries(ctx, workflow); err != nil {
				return err
			}
			if err := tx.saveWorkflow(ctx, workflow); err != nil {
				return err
			}
		}
		result = workflow
		return nil
	})
	return result, err
}

func (repository *Repository) ListWorkflows(ctx context.Context, hubID, circleID, ownerAgentID string, now time.Time, limit, offset int) ([]hub.Workflow, int, error) {
	if limit < 1 || limit > 100 || offset < 0 {
		return nil, 0, fmt.Errorf("workflow pagination is invalid")
	}
	var workflows []hub.Workflow
	var total int
	err := repository.withTransaction(ctx, func(tx *Repository) error {
		rows, err := tx.executor().QueryContext(ctx, `SELECT workflow_id, workflow_json FROM agent_workflow
WHERE hub_id=? AND circle_id=? AND owner_agent_id=? ORDER BY created_at DESC LIMIT ? OFFSET ?`, hubID, circleID, ownerAgentID, limit, offset)
		if err != nil {
			return err
		}
		for rows.Next() {
			var workflowID, encoded string
			if err := rows.Scan(&workflowID, &encoded); err != nil {
				_ = rows.Close()
				return err
			}
			var workflow hub.Workflow
			if err := json.Unmarshal([]byte(encoded), &workflow); err != nil {
				_ = rows.Close()
				return err
			}
			workflows = append(workflows, workflow)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for index := range workflows {
			workflow := &workflows[index]
			if workflow.Expire(now) {
				if err := tx.cancelPendingWorkflowDeliveries(ctx, *workflow); err != nil {
					return err
				}
				if err := tx.saveWorkflow(ctx, *workflow); err != nil {
					return err
				}
			}
		}
		return tx.executor().QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_workflow WHERE hub_id=? AND circle_id=? AND owner_agent_id=?`, hubID, circleID, ownerAgentID).Scan(&total)
	})
	return workflows, total, err
}

func (repository *Repository) RegisterWorkflowAttempt(ctx context.Context, hubID, circleID, actorAgentID, workflowID string, registration hub.WorkflowAttemptRegistration, now time.Time) (hub.Workflow, bool, error) {
	var result hub.Workflow
	duplicate := false
	err := repository.withTransaction(ctx, func(tx *Repository) error {
		workflow, err := tx.loadWorkflowByScope(ctx, hubID, circleID, workflowID)
		if err != nil {
			return err
		}
		participant := workflow.OwnerAgentID == actorAgentID
		if !participant {
			for _, priorStep := range workflow.Steps {
				if priorStep.TargetAgentID == actorAgentID {
					participant = true
					break
				}
			}
		}
		if !participant {
			return store.ErrNotFound
		}
		if workflow.Expire(now) {
			if err := tx.cancelPendingWorkflowDeliveries(ctx, workflow); err != nil {
				return err
			}
			if err := tx.saveWorkflow(ctx, workflow); err != nil {
				return err
			}
		}
		if strings.TrimSpace(registration.StepID) == "" || len(registration.StepID) > 128 || strings.TrimSpace(registration.TaskID) == "" || strings.TrimSpace(registration.TargetAgentID) == "" || strings.TrimSpace(registration.IdempotencyKey) == "" || len(registration.IdempotencyKey) > 128 || registration.Attempt < 1 {
			return store.ErrInvalidState
		}
		var taskCount int
		if err := tx.executor().QueryRowContext(ctx, `SELECT COUNT(*) FROM inbox_item WHERE hub_id=? AND circle_id=? AND requester_agent_id=? AND target_agent_id=? AND task_id=?`, hubID, circleID, actorAgentID, registration.TargetAgentID, registration.TaskID).Scan(&taskCount); err != nil {
			return err
		}
		if taskCount != 1 {
			return store.ErrNotFound
		}
		var step *hub.WorkflowStep
		for index := range workflow.Steps {
			if workflow.Steps[index].StepID == registration.StepID {
				step = &workflow.Steps[index]
			}
			for _, prior := range workflow.Steps[index].Attempts {
				if prior.IdempotencyKey == registration.IdempotencyKey || prior.TaskID == registration.TaskID {
					if workflow.Steps[index].StepID == registration.StepID && prior.Attempt == registration.Attempt && prior.TaskID == registration.TaskID && prior.TargetAgentID == registration.TargetAgentID && prior.IdempotencyKey == registration.IdempotencyKey {
						duplicate = true
						result = workflow
						return nil
					}
					return store.ErrConflict
				}
			}
		}
		if workflow.State != hub.WorkflowRunning {
			return store.ErrInvalidState
		}
		if step == nil {
			if len(workflow.Steps) >= workflow.ExpectedSteps || registration.Attempt != 1 {
				return store.ErrInvalidState
			}
			workflow.Steps = append(workflow.Steps, hub.WorkflowStep{StepID: registration.StepID, TargetAgentID: registration.TargetAgentID, TaskID: registration.TaskID, State: hub.WorkflowStepSubmitted, Attempt: 1, UpdatedAt: now.UTC(), Attempts: []hub.WorkflowAttempt{{Attempt: 1, TargetAgentID: registration.TargetAgentID, TaskID: registration.TaskID, IdempotencyKey: registration.IdempotencyKey, State: hub.WorkflowStepSubmitted, CreatedAt: now.UTC(), UpdatedAt: now.UTC()}}})
		} else {
			if registration.Attempt == step.Attempt {
				last := step.Attempts[len(step.Attempts)-1]
				if last.TaskID == registration.TaskID && last.TargetAgentID == registration.TargetAgentID && last.IdempotencyKey == registration.IdempotencyKey {
					duplicate = true
					result = workflow
					return nil
				}
				return store.ErrConflict
			}
			if registration.Attempt != step.Attempt+1 || step.Attempt > workflow.RetryLimit || step.State != hub.WorkflowStepFailed {
				return store.ErrInvalidState
			}
			step.TargetAgentID, step.TaskID, step.Attempt, step.State, step.UpdatedAt = registration.TargetAgentID, registration.TaskID, registration.Attempt, hub.WorkflowStepSubmitted, now.UTC()
			step.Attempts = append(step.Attempts, hub.WorkflowAttempt{Attempt: registration.Attempt, TargetAgentID: registration.TargetAgentID, TaskID: registration.TaskID, IdempotencyKey: registration.IdempotencyKey, State: hub.WorkflowStepSubmitted, CreatedAt: now.UTC(), UpdatedAt: now.UTC()})
		}
		workflow.Recompute(now)
		if err := tx.saveWorkflow(ctx, workflow); err != nil {
			return err
		}
		result = workflow
		return nil
	})
	return result, duplicate, err
}

func (repository *Repository) ReportWorkflowOutcome(ctx context.Context, hubID, circleID, workflowID string, outcome hub.WorkflowOutcome, now time.Time) (hub.Workflow, bool, error) {
	var result hub.Workflow
	duplicate := false
	err := repository.withTransaction(ctx, func(tx *Repository) error {
		workflow, err := tx.loadWorkflowByScope(ctx, hubID, circleID, workflowID)
		if err != nil {
			return err
		}
		if workflow.Expire(now) {
			if err := tx.cancelPendingWorkflowDeliveries(ctx, workflow); err != nil {
				return err
			}
			if err := tx.saveWorkflow(ctx, workflow); err != nil {
				return err
			}
			return store.ErrInvalidState
		}
		if strings.TrimSpace(outcome.StepID) == "" || outcome.Attempt < 1 || len(outcome.Result) > hub.MaxWorkflowResultBytes || len(outcome.Error) > hub.MaxWorkflowErrorBytes {
			return store.ErrInvalidState
		}
		if outcome.State != hub.WorkflowStepWorking && outcome.State != hub.WorkflowStepCompleted && outcome.State != hub.WorkflowStepFailed && outcome.State != hub.WorkflowStepCanceled {
			return store.ErrInvalidState
		}
		var step *hub.WorkflowStep
		for index := range workflow.Steps {
			if workflow.Steps[index].StepID == outcome.StepID {
				step = &workflow.Steps[index]
				break
			}
		}
		if step == nil || step.Attempt != outcome.Attempt || step.TargetAgentID != outcome.TargetAgentID {
			return store.ErrNotFound
		}
		last := &step.Attempts[len(step.Attempts)-1]
		if outcome.State == hub.WorkflowStepFailed && outcome.Attempt >= workflow.RetryLimit+1 {
			outcome.State = hub.WorkflowStepDeadLetter
		}
		if workflow.State != hub.WorkflowRunning {
			if last.State == outcome.State && last.Result == outcome.Result && last.Error == outcome.Error {
				duplicate = true
				result = workflow
				return nil
			}
			return store.ErrInvalidState
		}
		if last.State == hub.WorkflowStepCompleted || last.State == hub.WorkflowStepFailed || last.State == hub.WorkflowStepDeadLetter || last.State == hub.WorkflowStepCanceled || last.State == hub.WorkflowStepTimedOut {
			if last.State == outcome.State && last.Result == outcome.Result && last.Error == outcome.Error {
				duplicate = true
				result = workflow
				return nil
			}
			return store.ErrConflict
		}
		if outcome.State == hub.WorkflowStepWorking && last.State != hub.WorkflowStepSubmitted && last.State != hub.WorkflowStepWorking {
			return store.ErrInvalidState
		}
		last.State, last.Result, last.Error, last.UpdatedAt = outcome.State, outcome.Result, outcome.Error, now.UTC()
		step.State, step.UpdatedAt = outcome.State, now.UTC()
		workflow.Recompute(now)
		if hub.IsTerminalWorkflowState(workflow.State) {
			workflow.CancelActiveSteps(now)
			if err := tx.cancelPendingWorkflowDeliveries(ctx, workflow); err != nil {
				return err
			}
		}
		if err := tx.saveWorkflow(ctx, workflow); err != nil {
			return err
		}
		result = workflow
		return nil
	})
	return result, duplicate, err
}

func (repository *Repository) CancelWorkflow(ctx context.Context, hubID, circleID, ownerAgentID, workflowID string, now time.Time) (hub.Workflow, error) {
	var result hub.Workflow
	err := repository.withTransaction(ctx, func(tx *Repository) error {
		workflow, err := tx.loadWorkflow(ctx, hubID, circleID, ownerAgentID, workflowID)
		if err != nil {
			return err
		}
		if workflow.Expire(now) {
			if err := tx.cancelPendingWorkflowDeliveries(ctx, workflow); err != nil {
				return err
			}
			if err := tx.saveWorkflow(ctx, workflow); err != nil {
				return err
			}
			result = workflow
			return nil
		}
		if err := workflow.Cancel(now); err != nil {
			return store.ErrInvalidState
		}
		if err := tx.cancelPendingWorkflowDeliveries(ctx, workflow); err != nil {
			return err
		}
		if err := tx.saveWorkflow(ctx, workflow); err != nil {
			return err
		}
		result = workflow
		return nil
	})
	return result, err
}

func (repository *Repository) loadWorkflow(ctx context.Context, hubID, circleID, ownerAgentID, workflowID string) (hub.Workflow, error) {
	return repository.loadWorkflowQuery(ctx, `SELECT workflow_json FROM agent_workflow WHERE hub_id=? AND circle_id=? AND owner_agent_id=? AND workflow_id=?`, hubID, circleID, ownerAgentID, workflowID)
}

func (repository *Repository) loadWorkflowByScope(ctx context.Context, hubID, circleID, workflowID string) (hub.Workflow, error) {
	return repository.loadWorkflowQuery(ctx, `SELECT workflow_json FROM agent_workflow WHERE hub_id=? AND circle_id=? AND workflow_id=?`, hubID, circleID, workflowID)
}

func (repository *Repository) loadWorkflowQuery(ctx context.Context, query string, args ...any) (hub.Workflow, error) {
	var encoded string
	if err := repository.executor().QueryRowContext(ctx, query, args...).Scan(&encoded); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return hub.Workflow{}, store.ErrNotFound
		}
		return hub.Workflow{}, err
	}
	var workflow hub.Workflow
	if err := json.Unmarshal([]byte(encoded), &workflow); err != nil {
		return hub.Workflow{}, fmt.Errorf("decode workflow: %w", err)
	}
	return workflow, nil
}

func (repository *Repository) saveWorkflow(ctx context.Context, workflow hub.Workflow) error {
	encoded, err := json.Marshal(workflow)
	if err != nil {
		return err
	}
	_, err = repository.executor().ExecContext(ctx, `UPDATE agent_workflow SET state=?, deadline=?, updated_at=?, workflow_json=? WHERE hub_id=? AND circle_id=? AND owner_agent_id=? AND workflow_id=?`, string(workflow.State), formatTime(workflow.Deadline), formatTime(workflow.UpdatedAt), string(encoded), workflow.HubID, workflow.CircleID, workflow.OwnerAgentID, workflow.WorkflowID)
	return err
}

const workflowDeliveryCancelBatchSize = 500

func (repository *Repository) cancelPendingWorkflowDeliveries(ctx context.Context, workflow hub.Workflow) error {
	taskIDs := make([]string, 0)
	seen := make(map[string]struct{})
	for _, step := range workflow.Steps {
		for _, attempt := range step.Attempts {
			if attempt.TaskID == "" {
				continue
			}
			if _, exists := seen[attempt.TaskID]; exists {
				continue
			}
			seen[attempt.TaskID] = struct{}{}
			taskIDs = append(taskIDs, attempt.TaskID)
		}
	}
	for start := 0; start < len(taskIDs); start += workflowDeliveryCancelBatchSize {
		end := start + workflowDeliveryCancelBatchSize
		if end > len(taskIDs) {
			end = len(taskIDs)
		}
		batch := taskIDs[start:end]
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
		query := `UPDATE inbox_item SET state='CANCELED', canceled_at=?, cancel_reason='workflow terminal' WHERE hub_id=? AND circle_id=? AND task_id IN (` + placeholders + `) AND state='PENDING'`
		args := make([]any, 0, 3+len(batch))
		args = append(args, formatTime(workflow.UpdatedAt), workflow.HubID, workflow.CircleID)
		for _, taskID := range batch {
			args = append(args, taskID)
		}
		if _, err := repository.executor().ExecContext(ctx, query, args...); err != nil {
			return err
		}
	}
	return nil
}

var _ store.WorkflowStore = (*Repository)(nil)
