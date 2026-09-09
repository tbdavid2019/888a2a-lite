package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/tbdavid2019/888a2a-lite/internal/a2a"
	"github.com/tbdavid2019/888a2a-lite/internal/hub"
	"github.com/tbdavid2019/888a2a-lite/internal/store"
)

func (repository *Repository) CreateGroupTask(ctx context.Context, parent a2a.TaskRecord, members []a2a.TaskRecord, items []hub.InboxItem, links []a2a.GroupTaskMember, maxPending, maxFanout int) (a2a.TaskRecord, bool, error) {
	var result a2a.TaskRecord
	duplicate := false
	err := repository.withTransaction(ctx, func(tx *Repository) error {
		existing, findErr := tx.findTaskByDedupe(ctx, parent.HubID, parent.CircleID, parent.RequesterAgentID, parent.TargetAgentID, parent.MessageID)
		if findErr == nil {
			if existing.ContentDigest != parent.ContentDigest {
				return store.ErrConflict
			}
			result, duplicate = existing, true
			return nil
		}
		if !errors.Is(findErr, store.ErrNotFound) {
			return findErr
		}
		if err := tx.validateGroupFanoutPreflight(ctx, parent, links, maxPending, maxFanout); err != nil {
			return err
		}
		if err := tx.insertStandardTaskRow(ctx, parent); err != nil {
			return err
		}
		if len(members) != len(items) || len(members) != len(links) {
			return errors.New("group task member and delivery counts do not match")
		}
		for index := range members {
			member := members[index]
			if err := tx.insertStandardTaskRow(ctx, member); err != nil {
				return err
			}
			stored, itemDuplicate, err := tx.Enqueue(ctx, items[index])
			if err != nil {
				return err
			}
			if itemDuplicate {
				return store.ErrConflict
			}
			member.MailboxSequence = stored.Sequence
			if _, err := tx.executor().ExecContext(ctx, "UPDATE a2a_task SET mailbox_sequence = ? WHERE hub_id = ? AND task_id = ?", stored.Sequence, member.HubID, member.ID); err != nil {
				return err
			}
			links[index].ParentTaskID = parent.ID
			links[index].MemberTaskID = member.ID
			mentions, marshalErr := json.Marshal(links[index].Mentions)
			if marshalErr != nil {
				return marshalErr
			}
			if _, err := tx.executor().ExecContext(ctx, `INSERT INTO a2a_group_task_member (hub_id, circle_id, group_id, parent_task_id, member_task_id, target_agent_id, reply_policy, mentions_json, ordinal, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, parent.HubID, parent.CircleID, links[index].GroupID, parent.ID, member.ID, member.TargetAgentID, links[index].ReplyPolicy, string(mentions), links[index].Ordinal, formatTime(links[index].CreatedAt)); err != nil {
				return err
			}
		}
		if err := tx.appendTaskEvent(ctx, parent, "group-created"); err != nil {
			return err
		}
		result = parent
		return nil
	})
	return result, duplicate, err
}

func (repository *Repository) validateGroupFanoutPreflight(ctx context.Context, parent a2a.TaskRecord, links []a2a.GroupTaskMember, maxPending, maxFanout int) error {
	if len(links) == 0 || (maxFanout > 0 && len(links) > maxFanout) {
		return store.ErrInvalidState
	}
	groupID := strings.TrimPrefix(parent.TargetAgentID, "group:")
	var groupState, groupCircle string
	if err := repository.executor().QueryRowContext(ctx, "SELECT state, circle_id FROM agent_group WHERE hub_id = ? AND group_id = ?", parent.HubID, groupID).Scan(&groupState, &groupCircle); err != nil {
		return store.ErrInvalidState
	}
	if groupState != "ACTIVE" || groupCircle != parent.CircleID {
		return store.ErrInvalidState
	}
	seen := make(map[string]struct{}, len(links))
	for _, link := range links {
		if link.GroupID != groupID || link.CircleID != parent.CircleID {
			return store.ErrInvalidState
		}
		if _, exists := seen[link.TargetAgentID]; exists {
			return store.ErrConflict
		}
		seen[link.TargetAgentID] = struct{}{}
		var memberState, memberCircle string
		if err := repository.executor().QueryRowContext(ctx, "SELECT state, circle_id FROM group_member WHERE hub_id = ? AND group_id = ? AND agent_id = ?", parent.HubID, groupID, link.TargetAgentID).Scan(&memberState, &memberCircle); err != nil || memberState != "ACTIVE" || memberCircle != parent.CircleID {
			return store.ErrInvalidState
		}
		agent, err := repository.FindAgent(ctx, link.TargetAgentID)
		if err != nil || agent.CircleID != parent.CircleID || !supportsStoredExecution(agent.Capabilities) {
			return store.ErrInvalidState
		}
		state := agent.StateAt(time.Now().UTC())
		if state == hub.AgentStateExpired || state == hub.AgentStateRevoked {
			return store.ErrInvalidState
		}
		var pending int
		if err := repository.executor().QueryRowContext(ctx, "SELECT COUNT(*) FROM inbox_item WHERE hub_id = ? AND target_agent_id = ? AND state = 'PENDING'", parent.HubID, link.TargetAgentID).Scan(&pending); err != nil {
			return err
		}
		if maxPending > 0 && pending >= maxPending {
			return store.ErrConflict
		}
	}
	return nil
}

func supportsStoredExecution(capabilities []string) bool {
	for _, capability := range capabilities {
		if strings.EqualFold(strings.TrimSpace(capability), a2a.ExecutionCapability) {
			return true
		}
	}
	return false
}

func (repository *Repository) insertStandardTaskRow(ctx context.Context, task a2a.TaskRecord) error {
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
	_, err = repository.executor().ExecContext(ctx, `INSERT INTO a2a_task (hub_id, task_id, circle_id, requester_agent_id, target_agent_id, context_id, message_id, turn_id, revision, state, message_json, history_json, result_message_json, artifacts_json, mailbox_sequence, content_digest, execution_deadline, retry_budget, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', ?, 0, ?, ?, ?, ?, ?)`, task.HubID, task.ID, task.CircleID, task.RequesterAgentID, task.TargetAgentID, task.ContextID, task.MessageID, task.TurnID, task.Revision, string(task.State), string(messageJSON), string(historyJSON), string(artifactsJSON), task.ContentDigest, nullTime(task.ExecutionDeadline), task.RetryBudget, formatTime(task.CreatedAt), formatTime(task.UpdatedAt))
	return err
}

func (repository *Repository) ListGroupTaskMembers(ctx context.Context, hubID, circleID, requesterID, parentTaskID string) ([]a2a.TaskRecord, error) {
	rows, err := repository.executor().QueryContext(ctx, `SELECT member_task_id FROM a2a_group_task_member WHERE hub_id = ? AND circle_id = ? AND parent_task_id = ? AND EXISTS (SELECT 1 FROM a2a_task WHERE hub_id = a2a_group_task_member.hub_id AND task_id = a2a_group_task_member.parent_task_id AND requester_agent_id = ?) ORDER BY ordinal, target_agent_id`, hubID, circleID, parentTaskID, requesterID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	members := make([]a2a.TaskRecord, 0, len(ids))
	for _, id := range ids {
		task, err := repository.findTask(ctx, "SELECT "+standardTaskColumns+" FROM a2a_task WHERE hub_id = ? AND task_id = ?", hubID, id)
		if err != nil {
			return nil, err
		}
		members = append(members, task)
	}
	return members, nil
}

func (repository *Repository) aggregateGroupParent(ctx context.Context, parentTaskID string) error {
	parent, err := repository.findTask(ctx, "SELECT "+standardTaskColumns+" FROM a2a_task WHERE task_id = ? AND target_agent_id LIKE 'group:%'", parentTaskID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if isTerminalTaskState(parent.State) {
		return nil
	}
	rows, err := repository.executor().QueryContext(ctx, "SELECT member_task_id FROM a2a_group_task_member WHERE hub_id = ? AND parent_task_id = ? ORDER BY ordinal, target_agent_id", parent.HubID, parent.ID)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	var memberIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		memberIDs = append(memberIDs, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(memberIDs) == 0 {
		return nil
	}
	members := make([]a2a.TaskRecord, 0, len(memberIDs))
	for _, memberID := range memberIDs {
		member, findErr := repository.findTask(ctx, "SELECT "+standardTaskColumns+" FROM a2a_task WHERE hub_id = ? AND task_id = ?", parent.HubID, memberID)
		if findErr != nil {
			return findErr
		}
		members = append(members, member)
	}
	desired := a2a.TaskStateSubmitted
	allCompleted := true
	anyWorking := false
	anyFailure := false
	for _, member := range members {
		switch member.State {
		case a2a.TaskStateFailed, a2a.TaskStateRejected:
			anyFailure = true
		case a2a.TaskStateWorking:
			anyWorking = true
			allCompleted = false
		case a2a.TaskStateCompleted:
		default:
			allCompleted = false
		}
	}
	if anyFailure {
		desired = a2a.TaskStateFailed
	} else if allCompleted {
		desired = a2a.TaskStateCompleted
	} else if anyWorking {
		desired = a2a.TaskStateWorking
	}
	if desired == a2a.TaskStateSubmitted && parent.State == a2a.TaskStateSubmitted {
		return nil
	}
	if parent.State == a2a.TaskStateSubmitted && desired == a2a.TaskStateSubmitted {
		return nil
	}
	parent.State = desired
	parent.Revision++
	parent.UpdatedAt = time.Now().UTC()
	parent.Artifacts = aggregateMemberArtifacts(members)
	if err := repository.saveTask(ctx, parent); err != nil {
		return err
	}
	return repository.appendTaskEvent(ctx, parent, "group-aggregate")
}

func aggregateMemberArtifacts(members []a2a.TaskRecord) []a2a.Artifact {
	artifacts := make([]a2a.Artifact, 0)
	for _, member := range members {
		parts := []a2a.Part(nil)
		if member.ResultMessage != nil {
			parts = append(parts, member.ResultMessage.Parts...)
		}
		if len(parts) == 0 {
			parts = append(parts, artifactParts(member.Artifacts)...)
		}
		if len(parts) == 0 && (member.State == a2a.TaskStateFailed || member.State == a2a.TaskStateRejected) {
			parts = []a2a.Part{a2a.TextPart(string(member.State))}
		}
		if len(parts) == 0 {
			continue
		}
		artifacts = append(artifacts, a2a.Artifact{ArtifactID: "member-" + member.TargetAgentID, Name: member.TargetAgentID, Parts: parts, Metadata: map[string]any{"memberAgentId": member.TargetAgentID, "state": member.State}})
	}
	return artifacts
}

func artifactParts(artifacts []a2a.Artifact) []a2a.Part {
	parts := make([]a2a.Part, 0)
	for _, artifact := range artifacts {
		parts = append(parts, artifact.Parts...)
	}
	return parts
}

func isTerminalTaskState(state a2a.TaskState) bool {
	return state == a2a.TaskStateCompleted || state == a2a.TaskStateFailed || state == a2a.TaskStateCanceled || state == a2a.TaskStateRejected
}

func groupParentID(ctx context.Context, executor interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, hubID, memberTaskID string) (string, error) {
	var parentID string
	err := executor.QueryRowContext(ctx, "SELECT parent_task_id FROM a2a_group_task_member WHERE hub_id = ? AND member_task_id = ?", hubID, memberTaskID).Scan(&parentID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", store.ErrNotFound
	}
	return parentID, err
}
