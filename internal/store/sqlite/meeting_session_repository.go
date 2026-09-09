package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/tbdavid2019/888a2a-lite/internal/hub"
	"github.com/tbdavid2019/888a2a-lite/internal/store"
)

func (repository *Repository) CreateMeetingSession(ctx context.Context, session hub.MeetingSession) (hub.MeetingSession, bool, error) {
	if strings.TrimSpace(session.HubID) == "" || strings.TrimSpace(session.GroupID) == "" || strings.TrimSpace(session.SessionID) == "" {
		return hub.MeetingSession{}, false, errors.New("meeting session requires hub id, group id, and session id")
	}
	if session.State == "" {
		session.State = hub.MeetingSessionOpen
	}
	if session.CreatedAt.IsZero() {
		session.CreatedAt = time.Now().UTC()
	}
	var isNew bool
	err := repository.withTransaction(ctx, func(tx *Repository) error {
		if session.TriggerMessageID != "" {
			existing, err := tx.FindMeetingSessionByTrigger(ctx, session.HubID, session.CircleID, session.GroupID, session.TriggerMessageID)
			if err == nil {
				session = existing
				isNew = false
				return nil
			} else if !errors.Is(err, store.ErrNotFound) {
				return err
			}
		}
		if session.SynthesisJobID != "" {
			existing, err := tx.findMeetingSessionByJob(ctx, session.HubID, session.CircleID, session.SynthesisJobID)
			if err == nil {
				session = existing
				isNew = false
				return nil
			} else if !errors.Is(err, store.ErrNotFound) {
				return err
			}
		}

		_, err := tx.executor().ExecContext(ctx, `
INSERT INTO meeting_session (hub_id, circle_id, group_id, session_id, start_revision, cutoff_revision,
    trigger_message_id, triggered_by, charter_version, state, synthesis_job_id, created_at, concluded_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			session.HubID, session.CircleID, session.GroupID, session.SessionID, session.StartRevision, session.CutoffRevision,
			session.TriggerMessageID, session.TriggeredBy, session.CharterVersion, string(session.State),
			session.SynthesisJobID, formatTime(session.CreatedAt), nullTimePtr(session.ConcludedAt))
		if err != nil {
			return err
		}
		isNew = true
		return nil
	})
	return session, isNew, err
}

func (repository *Repository) GetMeetingSession(ctx context.Context, hubID, circleID, sessionID string) (hub.MeetingSession, error) {
	return scanMeetingSession(repository.executor().QueryRowContext(ctx, `
SELECT hub_id, circle_id, group_id, session_id, start_revision, cutoff_revision,
       trigger_message_id, triggered_by, charter_version, state, synthesis_job_id, created_at, concluded_at
FROM meeting_session WHERE hub_id = ? AND circle_id = ? AND session_id = ?`, hubID, circleID, sessionID))
}

func (repository *Repository) FindMeetingSessionByTrigger(ctx context.Context, hubID, circleID, groupID, triggerMessageID string) (hub.MeetingSession, error) {
	return scanMeetingSession(repository.executor().QueryRowContext(ctx, `
SELECT hub_id, circle_id, group_id, session_id, start_revision, cutoff_revision,
       trigger_message_id, triggered_by, charter_version, state, synthesis_job_id, created_at, concluded_at
FROM meeting_session WHERE hub_id = ? AND circle_id = ? AND group_id = ? AND trigger_message_id = ?`, hubID, circleID, groupID, triggerMessageID))
}

func (repository *Repository) findMeetingSessionByJob(ctx context.Context, hubID, circleID, jobID string) (hub.MeetingSession, error) {
	return scanMeetingSession(repository.executor().QueryRowContext(ctx, `
SELECT hub_id, circle_id, group_id, session_id, start_revision, cutoff_revision,
       trigger_message_id, triggered_by, charter_version, state, synthesis_job_id, created_at, concluded_at
FROM meeting_session WHERE hub_id = ? AND circle_id = ? AND synthesis_job_id = ?`, hubID, circleID, jobID))
}

func (repository *Repository) FindLatestMeetingSession(ctx context.Context, hubID, circleID, groupID string) (hub.MeetingSession, error) {
	return scanMeetingSession(repository.executor().QueryRowContext(ctx, `
SELECT hub_id, circle_id, group_id, session_id, start_revision, cutoff_revision,
       trigger_message_id, triggered_by, charter_version, state, synthesis_job_id, created_at, concluded_at
FROM meeting_session WHERE hub_id = ? AND circle_id = ? AND group_id = ?
ORDER BY created_at DESC, cutoff_revision DESC LIMIT 1`, hubID, circleID, groupID))
}

func (repository *Repository) UpdateMeetingSessionState(ctx context.Context, hubID, circleID, sessionID string, expectedState, newState hub.MeetingSessionState, concludedAt *time.Time) error {
	result, err := repository.executor().ExecContext(ctx, `
UPDATE meeting_session SET state = ?, concluded_at = ?
WHERE hub_id = ? AND circle_id = ? AND session_id = ? AND state = ?`,
		string(newState), nullTimePtr(concludedAt), hubID, circleID, sessionID, string(expectedState))
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return store.ErrConflict
	}
	return nil
}

func (repository *Repository) ListMeetingSessions(ctx context.Context, hubID, circleID, groupID string, limit int) ([]hub.MeetingSession, error) {
	if limit < 1 || limit > 100 {
		limit = 20
	}
	rows, err := repository.executor().QueryContext(ctx, `
SELECT hub_id, circle_id, group_id, session_id, start_revision, cutoff_revision,
       trigger_message_id, triggered_by, charter_version, state, synthesis_job_id, created_at, concluded_at
FROM meeting_session WHERE hub_id = ? AND circle_id = ? AND group_id = ?
ORDER BY created_at DESC LIMIT ?`, hubID, circleID, groupID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	sessions := make([]hub.MeetingSession, 0, limit)
	for rows.Next() {
		session, err := scanMeetingSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

func scanMeetingSession(row scanner) (hub.MeetingSession, error) {
	var session hub.MeetingSession
	var state, created string
	var triggerMsg, triggeredBy, synthJob, concluded sql.NullString
	if err := row.Scan(&session.HubID, &session.CircleID, &session.GroupID, &session.SessionID,
		&session.StartRevision, &session.CutoffRevision, &triggerMsg, &triggeredBy,
		&session.CharterVersion, &state, &synthJob, &created, &concluded); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return hub.MeetingSession{}, ErrNotFound
		}
		return hub.MeetingSession{}, err
	}
	session.TriggerMessageID = triggerMsg.String
	session.TriggeredBy = triggeredBy.String
	session.SynthesisJobID = synthJob.String
	session.State = hub.MeetingSessionState(state)
	var err error
	if session.CreatedAt, err = parseRequiredTime(sql.NullString{String: created, Valid: true}); err != nil {
		return hub.MeetingSession{}, fmt.Errorf("parse meeting session created_at: %w", err)
	}
	if session.ConcludedAt, err = parseNullableTimePtr(concluded); err != nil {
		return hub.MeetingSession{}, fmt.Errorf("parse meeting session concluded_at: %w", err)
	}
	return session, nil
}
