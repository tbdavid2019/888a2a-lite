package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/tbdavid2019/888a2a-lite/internal/a2a"
	"github.com/tbdavid2019/888a2a-lite/internal/hub"
	"github.com/tbdavid2019/888a2a-lite/internal/store"
)

func (repository *Repository) CreateTaskWithDelivery(ctx context.Context, task a2a.TaskRecord, item hub.InboxItem) (a2a.TaskRecord, bool, error) {
	var result a2a.TaskRecord
	duplicate := false
	err := repository.withTransaction(ctx, func(tx *Repository) error {
		existing, findErr := tx.findTaskByDedupe(ctx, task.HubID, task.CircleID, task.RequesterAgentID, task.TargetAgentID, task.MessageID)
		if findErr == nil {
			if existing.ContentDigest != task.ContentDigest {
				return store.ErrConflict
			}
			result, duplicate = existing, true
			return nil
		}
		if !errors.Is(findErr, store.ErrNotFound) {
			return findErr
		}
		if task.CreatedAt.IsZero() {
			task.CreatedAt = time.Now().UTC()
		}
		if task.UpdatedAt.IsZero() {
			task.UpdatedAt = task.CreatedAt
		}
		if task.Revision == 0 {
			task.Revision = 1
		}
		if task.State == "" {
			task.State = a2a.TaskStateSubmitted
		}
		if len(task.History) == 0 {
			task.History = []a2a.Message{task.Message}
		}
		messageJSON, err := json.Marshal(task.Message)
		if err != nil {
			return err
		}
		historyJSON, err := json.Marshal(task.History)
		if err != nil {
			return err
		}
		artifactsJSON, err := json.Marshal(task.Artifacts)
		if err != nil {
			return err
		}
		resultJSON := ""
		if task.ResultMessage != nil {
			encoded, marshalErr := json.Marshal(task.ResultMessage)
			if marshalErr != nil {
				return marshalErr
			}
			resultJSON = string(encoded)
		}
		_, err = tx.executor().ExecContext(ctx, `
INSERT INTO a2a_task (
    hub_id, task_id, circle_id, requester_agent_id, target_agent_id, context_id,
    message_id, turn_id, revision, state, message_json, history_json,
    result_message_json, artifacts_json, mailbox_sequence, content_digest,
    execution_deadline, retry_budget, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?, ?, ?)`,
			task.HubID, task.ID, task.CircleID, task.RequesterAgentID, task.TargetAgentID,
			task.ContextID, task.MessageID, task.TurnID, task.Revision, string(task.State),
			string(messageJSON), string(historyJSON), resultJSON, string(artifactsJSON), task.ContentDigest,
			nullTime(task.ExecutionDeadline), task.RetryBudget, formatTime(task.CreatedAt), formatTime(task.UpdatedAt))
		if err != nil {
			return err
		}

		item.TaskID = task.ID
		item.ContextID = task.ContextID
		item.TargetAgentID = task.TargetAgentID
		item.RequesterAgentID = task.RequesterAgentID
		item.HubID = task.HubID
		item.CircleID = task.CircleID
		stored, itemDuplicate, enqueueErr := tx.Enqueue(ctx, item)
		if enqueueErr != nil {
			return enqueueErr
		}
		if itemDuplicate {
			return store.ErrConflict
		}
		task.MailboxSequence = stored.Sequence
		if _, err := tx.executor().ExecContext(ctx, "UPDATE a2a_task SET mailbox_sequence = ? WHERE hub_id = ? AND task_id = ?", stored.Sequence, task.HubID, task.ID); err != nil {
			return err
		}
		if err := tx.appendTaskEvent(ctx, task, "created"); err != nil {
			return err
		}
		result = task
		return nil
	})
	return result, duplicate, err
}

func (repository *Repository) FindTask(ctx context.Context, hubID, circleID, requesterID, taskID string) (a2a.TaskRecord, error) {
	return repository.findTask(ctx, `SELECT `+standardTaskColumns+` FROM a2a_task WHERE hub_id = ? AND circle_id = ? AND requester_agent_id = ? AND task_id = ?`, hubID, circleID, requesterID, taskID)
}

func (repository *Repository) ResumeTaskWithDelivery(ctx context.Context, task a2a.TaskRecord, expectedRevision int64, item hub.InboxItem) (a2a.TaskRecord, bool, error) {
	var result a2a.TaskRecord
	duplicate := false
	err := repository.withTransaction(ctx, func(tx *Repository) error {
		existing, err := tx.FindTask(ctx, task.HubID, task.CircleID, task.RequesterAgentID, task.ID)
		if err != nil {
			return err
		}
		if existing.Revision != expectedRevision {
			return store.ErrConflict
		}
		if existing.State != a2a.TaskStateInputRequired && existing.State != a2a.TaskStateAuthRequired {
			return store.ErrInvalidState
		}
		if task.CreatedAt.IsZero() {
			task.CreatedAt = existing.CreatedAt
		}
		if task.UpdatedAt.IsZero() {
			task.UpdatedAt = time.Now().UTC()
		}
		if len(task.History) == 0 {
			task.History = append(append([]a2a.Message(nil), existing.History...), task.Message)
		}
		messageJSON, err := json.Marshal(task.Message)
		if err != nil {
			return err
		}
		historyJSON, err := json.Marshal(task.History)
		if err != nil {
			return err
		}
		resultJSON := ""
		artifactsJSON, err := json.Marshal(task.Artifacts)
		if err != nil {
			return err
		}
		updateResult, err := tx.executor().ExecContext(ctx, `UPDATE a2a_task SET message_id = ?, turn_id = ?, revision = ?, state = ?, message_json = ?, history_json = ?, result_message_json = ?, artifacts_json = ?, mailbox_sequence = 0, content_digest = ?, execution_deadline = ?, retry_budget = ?, updated_at = ? WHERE hub_id = ? AND task_id = ? AND revision = ?`, task.MessageID, task.TurnID, expectedRevision+1, string(a2a.TaskStateSubmitted), string(messageJSON), string(historyJSON), resultJSON, string(artifactsJSON), task.ContentDigest, nullTime(task.ExecutionDeadline), task.RetryBudget, formatTime(task.UpdatedAt), task.HubID, task.ID, expectedRevision)
		if err != nil {
			return err
		}
		if affected, _ := updateResult.RowsAffected(); affected != 1 {
			return store.ErrConflict
		}
		item.TaskID, item.ContextID, item.TargetAgentID, item.RequesterAgentID, item.HubID, item.CircleID = task.ID, task.ContextID, task.TargetAgentID, task.RequesterAgentID, task.HubID, task.CircleID
		stored, itemDuplicate, err := tx.Enqueue(ctx, item)
		if err != nil {
			return err
		}
		if itemDuplicate {
			duplicate = true
			result = existing
			return nil
		}
		task.Revision = expectedRevision + 1
		task.State = a2a.TaskStateSubmitted
		task.MailboxSequence = stored.Sequence
		if _, err := tx.executor().ExecContext(ctx, "UPDATE a2a_task SET mailbox_sequence = ? WHERE hub_id = ? AND task_id = ?", stored.Sequence, task.HubID, task.ID); err != nil {
			return err
		}
		if err := tx.appendTaskEvent(ctx, task, "turn-created"); err != nil {
			return err
		}
		result = task
		return nil
	})
	return result, duplicate, err
}

func (repository *Repository) FindTaskByMessage(ctx context.Context, hubID, circleID, requesterID, targetID, messageID string) (a2a.TaskRecord, error) {
	return repository.findTask(ctx, `SELECT `+standardTaskColumns+` FROM a2a_task WHERE hub_id = ? AND circle_id = ? AND requester_agent_id = ? AND target_agent_id = ? AND message_id = ?`, hubID, circleID, requesterID, targetID, messageID)
}

func (repository *Repository) findTaskByDedupe(ctx context.Context, hubID, circleID, requesterID, targetID, messageID string) (a2a.TaskRecord, error) {
	return repository.findTask(ctx, `SELECT `+standardTaskColumns+` FROM a2a_task WHERE hub_id = ? AND circle_id = ? AND requester_agent_id = ? AND target_agent_id = ? AND message_id = ?`, hubID, circleID, requesterID, targetID, messageID)
}

const standardTaskColumns = `hub_id, task_id, circle_id, requester_agent_id, target_agent_id, context_id,
message_id, turn_id, revision, state, message_json, history_json, result_message_json,
artifacts_json, mailbox_sequence, content_digest, execution_deadline, retry_budget, created_at, updated_at`

func (repository *Repository) findTask(ctx context.Context, query string, args ...any) (a2a.TaskRecord, error) {
	var task a2a.TaskRecord
	var state, messageJSON, historyJSON, resultJSON, artifactsJSON, created, updated string
	var deadline sql.NullString
	err := repository.executor().QueryRowContext(ctx, query, args...).Scan(
		&task.HubID, &task.ID, &task.CircleID, &task.RequesterAgentID, &task.TargetAgentID,
		&task.ContextID, &task.MessageID, &task.TurnID, &task.Revision, &state,
		&messageJSON, &historyJSON, &resultJSON, &artifactsJSON, &task.MailboxSequence,
		&task.ContentDigest, &deadline, &task.RetryBudget, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return a2a.TaskRecord{}, store.ErrNotFound
	}
	if err != nil {
		return a2a.TaskRecord{}, err
	}
	task.State = a2a.TaskState(state)
	if err := json.Unmarshal([]byte(messageJSON), &task.Message); err != nil {
		return a2a.TaskRecord{}, err
	}
	if err := json.Unmarshal([]byte(historyJSON), &task.History); err != nil {
		return a2a.TaskRecord{}, err
	}
	if strings.TrimSpace(resultJSON) != "" {
		task.ResultMessage = &a2a.Message{}
		if err := json.Unmarshal([]byte(resultJSON), task.ResultMessage); err != nil {
			return a2a.TaskRecord{}, err
		}
	}
	if err := json.Unmarshal([]byte(artifactsJSON), &task.Artifacts); err != nil {
		return a2a.TaskRecord{}, err
	}
	var parseErr error
	if deadline.Valid && deadline.String != "" {
		task.ExecutionDeadline, parseErr = time.Parse(time.RFC3339Nano, deadline.String)
		if parseErr != nil {
			return a2a.TaskRecord{}, parseErr
		}
	}
	if task.CreatedAt, parseErr = time.Parse(time.RFC3339Nano, created); parseErr != nil {
		return a2a.TaskRecord{}, parseErr
	}
	if task.UpdatedAt, parseErr = time.Parse(time.RFC3339Nano, updated); parseErr != nil {
		return a2a.TaskRecord{}, parseErr
	}
	return task, nil
}

func (repository *Repository) ListTasks(ctx context.Context, filter a2a.TaskFilter) ([]a2a.TaskRecord, int, error) {
	pageSize := filter.PageSize
	if pageSize < 1 || pageSize > 100 {
		pageSize = 50
	}
	where := []string{"hub_id = ?", "circle_id = ?", "requester_agent_id = ?"}
	args := []any{filter.HubID, filter.CircleID, filter.RequesterAgentID}
	if filter.ContextID != "" {
		where = append(where, "context_id = ?")
		args = append(args, filter.ContextID)
	}
	if filter.TargetAgentID != "" {
		where = append(where, "target_agent_id = ?")
		args = append(args, filter.TargetAgentID)
	}
	if filter.State != "" && filter.State != a2a.TaskStateUnspecified {
		where = append(where, "state = ?")
		args = append(args, string(filter.State))
	}
	whereSQL := strings.Join(where, " AND ")
	var total int
	if err := repository.executor().QueryRowContext(ctx, "SELECT COUNT(*) FROM a2a_task WHERE "+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := repository.executor().QueryContext(ctx, "SELECT task_id FROM a2a_task WHERE "+whereSQL+" ORDER BY updated_at DESC, task_id LIMIT ? OFFSET ?", append(args, pageSize, filter.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	ids := make([]string, 0, pageSize)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return nil, 0, err
	}
	items := make([]a2a.TaskRecord, 0, len(ids))
	for _, id := range ids {
		item, findErr := repository.findTask(ctx, "SELECT "+standardTaskColumns+" FROM a2a_task WHERE hub_id = ? AND task_id = ?", filter.HubID, id)
		if findErr != nil {
			return nil, 0, findErr
		}
		items = append(items, item)
	}
	return items, total, nil
}

func (repository *Repository) ListTaskEvents(ctx context.Context, hubID, circleID, requesterID, taskID string, afterRevision int64) ([]a2a.TaskEvent, error) {
	rows, err := repository.executor().QueryContext(ctx, `SELECT revision, event_type, payload_json, created_at FROM a2a_task_event WHERE hub_id = ? AND circle_id = ? AND task_id = ? AND revision > ? AND EXISTS (SELECT 1 FROM a2a_task WHERE hub_id = a2a_task_event.hub_id AND task_id = a2a_task_event.task_id AND circle_id = ? AND requester_agent_id = ?) ORDER BY revision`, hubID, circleID, taskID, afterRevision, circleID, requesterID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	events := make([]a2a.TaskEvent, 0)
	for rows.Next() {
		var event a2a.TaskEvent
		var payload, created string
		if err := rows.Scan(&event.Revision, &event.EventType, &payload, &created); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(payload), &event.Task); err != nil {
			return nil, err
		}
		var parseErr error
		event.CreatedAt, parseErr = time.Parse(time.RFC3339Nano, created)
		if parseErr != nil {
			return nil, parseErr
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

func (repository *Repository) ApplyUpdate(ctx context.Context, update a2a.TaskUpdate) (a2a.TaskRecord, bool, error) {
	var result a2a.TaskRecord
	duplicate := false
	err := repository.withTransaction(ctx, func(tx *Repository) error {
		task, err := tx.findTaskForUpdate(ctx, update.HubID, update.TaskID, update.TargetAgentID)
		if err != nil {
			return err
		}
		digestBytes, marshalErr := json.Marshal(update.Digestable())
		if marshalErr != nil {
			return marshalErr
		}
		digestSum := sha256.Sum256(digestBytes)
		digest := hex.EncodeToString(digestSum[:])
		var storedDigest, storedTaskJSON string
		lookupErr := tx.executor().QueryRowContext(ctx, "SELECT content_digest, task_json FROM a2a_task_update WHERE hub_id = ? AND task_id = ? AND update_id = ?", update.HubID, update.TaskID, update.UpdateID).Scan(&storedDigest, &storedTaskJSON)
		if lookupErr == nil {
			if storedDigest != digest {
				return store.ErrConflict
			}
			if err := json.Unmarshal([]byte(storedTaskJSON), &result); err != nil {
				return err
			}
			duplicate = true
			return nil
		}
		if !errors.Is(lookupErr, sql.ErrNoRows) {
			return lookupErr
		}
		if update.ExpectedRevision != task.Revision || update.TurnID != task.TurnID {
			return store.ErrConflict
		}
		if !validTaskTransition(task.State, update.State) {
			return store.ErrInvalidState
		}
		now := time.Now().UTC()
		task.State = update.State
		task.Revision++
		task.UpdatedAt = now
		if update.Message != nil {
			task.ResultMessage = update.Message
			task.History = append(task.History, *update.Message)
		}
		if update.Artifacts != nil {
			task.Artifacts = append([]a2a.Artifact(nil), update.Artifacts...)
		}
		if err := tx.saveTask(ctx, task); err != nil {
			return err
		}
		if err := tx.appendTaskEvent(ctx, task, "update"); err != nil {
			return err
		}
		taskJSON, marshalErr := json.Marshal(task)
		if marshalErr != nil {
			return marshalErr
		}
		if _, err := tx.executor().ExecContext(ctx, `INSERT INTO a2a_task_update (hub_id, task_id, update_id, content_digest, task_json, created_at) VALUES (?, ?, ?, ?, ?, ?)`, update.HubID, update.TaskID, update.UpdateID, digest, string(taskJSON), formatTime(now)); err != nil {
			return err
		}
		result = task
		return nil
	})
	return result, duplicate, err
}

func (repository *Repository) markStandardDeliveryAcknowledged(ctx context.Context, targetAgentID, taskID string, acknowledgedAt time.Time) error {
	task, err := repository.findTaskForUpdate(ctx, "", taskID, targetAgentID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if task.State != a2a.TaskStateSubmitted {
		return nil
	}
	task.State = a2a.TaskStateWorking
	task.Revision++
	task.UpdatedAt = acknowledgedAt
	if err := repository.saveTask(ctx, task); err != nil {
		return err
	}
	return repository.appendTaskEvent(ctx, task, "delivery-acknowledged")
}

func (repository *Repository) findTaskForUpdate(ctx context.Context, hubID, taskID, targetID string) (a2a.TaskRecord, error) {
	query := "SELECT " + standardTaskColumns + " FROM a2a_task WHERE task_id = ? AND target_agent_id = ?"
	args := []any{taskID, targetID}
	if hubID != "" {
		query = "SELECT " + standardTaskColumns + " FROM a2a_task WHERE hub_id = ? AND task_id = ? AND target_agent_id = ?"
		args = []any{hubID, taskID, targetID}
	}
	return repository.findTask(ctx, query, args...)
}

func (repository *Repository) saveTask(ctx context.Context, task a2a.TaskRecord) error {
	messageJSON, err := json.Marshal(task.Message)
	if err != nil {
		return err
	}
	historyJSON, err := json.Marshal(task.History)
	if err != nil {
		return err
	}
	artifactsJSON, err := json.Marshal(task.Artifacts)
	if err != nil {
		return err
	}
	resultJSON := ""
	if task.ResultMessage != nil {
		encoded, marshalErr := json.Marshal(task.ResultMessage)
		if marshalErr != nil {
			return marshalErr
		}
		resultJSON = string(encoded)
	}
	result, err := repository.executor().ExecContext(ctx, `UPDATE a2a_task SET revision = ?, state = ?, history_json = ?, result_message_json = ?, artifacts_json = ?, updated_at = ? WHERE hub_id = ? AND task_id = ? AND revision = ?`, task.Revision, string(task.State), string(historyJSON), resultJSON, string(artifactsJSON), formatTime(task.UpdatedAt), task.HubID, task.ID, task.Revision-1)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return store.ErrConflict
	}
	return nil
}

func (repository *Repository) appendTaskEvent(ctx context.Context, task a2a.TaskRecord, eventType string) error {
	encoded, err := json.Marshal(task.PublicTask(-1, true))
	if err != nil {
		return err
	}
	_, err = repository.executor().ExecContext(ctx, `INSERT INTO a2a_task_event (hub_id, task_id, circle_id, revision, event_type, payload_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, task.HubID, task.ID, task.CircleID, task.Revision, eventType, string(encoded), formatTime(task.UpdatedAt))
	return err
}

func validTaskTransition(from, to a2a.TaskState) bool {
	if from == to && from == a2a.TaskStateWorking {
		return true
	}
	if from == a2a.TaskStateSubmitted {
		return to == a2a.TaskStateWorking || to == a2a.TaskStateCompleted || to == a2a.TaskStateCanceled || to == a2a.TaskStateFailed || to == a2a.TaskStateRejected || to == a2a.TaskStateInputRequired || to == a2a.TaskStateAuthRequired
	}
	if from == a2a.TaskStateWorking {
		return to == a2a.TaskStateWorking || to == a2a.TaskStateCompleted || to == a2a.TaskStateFailed || to == a2a.TaskStateRejected || to == a2a.TaskStateInputRequired || to == a2a.TaskStateAuthRequired
	}
	return false
}

func (repository *Repository) CancelStandardTask(ctx context.Context, hubID, circleID, requesterID, taskID string, canceledAt time.Time) (a2a.TaskRecord, error) {
	var result a2a.TaskRecord
	err := repository.withTransaction(ctx, func(tx *Repository) error {
		task, err := tx.FindTask(ctx, hubID, circleID, requesterID, taskID)
		if err != nil {
			return err
		}
		if task.State == a2a.TaskStateCanceled {
			result = task
			return nil
		}
		if task.State != a2a.TaskStateSubmitted {
			return store.ErrInvalidState
		}
		task.State = a2a.TaskStateCanceled
		task.Revision++
		task.UpdatedAt = canceledAt
		if err := tx.saveTask(ctx, task); err != nil {
			return err
		}
		if _, err := tx.executor().ExecContext(ctx, `UPDATE inbox_item SET state = 'CANCELED', canceled_at = ?, cancel_reason = 'standard task canceled' WHERE hub_id = ? AND task_id = ? AND state = 'PENDING'`, formatTime(canceledAt), hubID, taskID); err != nil {
			return err
		}
		if err := tx.appendTaskEvent(ctx, task, "canceled"); err != nil {
			return err
		}
		result = task
		return nil
	})
	return result, err
}
