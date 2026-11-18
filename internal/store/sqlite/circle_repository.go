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

func (repository *Repository) Circles() store.CircleStore { return repository }

func (repository *Repository) CreateCircle(ctx context.Context, circle hub.Circle) error {
	if strings.TrimSpace(circle.HubID) == "" || strings.TrimSpace(circle.CircleID) == "" {
		return errors.New("circle requires hub id and circle id")
	}
	if circle.State == "" {
		circle.State = hub.CircleStateActive
	}
	if circle.CreatedAt.IsZero() {
		circle.CreatedAt = time.Now().UTC()
	}
	_, err := repository.executor().ExecContext(ctx, `
INSERT INTO hub_circle (hub_id, circle_id, alias, state, active_key_version, created_at, disabled_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (hub_id, circle_id) DO NOTHING`,
		circle.HubID, circle.CircleID, circle.Alias, string(circle.State), circle.ActiveKeyVersion,
		formatTime(circle.CreatedAt), nullTimePtr(circle.DisabledAt))
	return err
}

func (repository *Repository) FindCircle(ctx context.Context, circleID string) (hub.Circle, error) {
	var circle hub.Circle
	var state string
	var createdAt string
	var disabledAt sql.NullString
	err := repository.executor().QueryRowContext(ctx, `
SELECT hub_id, circle_id, alias, state, active_key_version, created_at, disabled_at
FROM hub_circle WHERE circle_id = ?`, circleID).Scan(
		&circle.HubID, &circle.CircleID, &circle.Alias, &state, &circle.ActiveKeyVersion,
		&createdAt, &disabledAt)
	if errors.Is(err, sql.ErrNoRows) {
		return hub.Circle{}, store.ErrNotFound
	}
	if err != nil {
		return hub.Circle{}, err
	}
	circle.State = hub.CircleState(state)
	if circle.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
		return hub.Circle{}, fmt.Errorf("parse circle created_at: %w", err)
	}
	if circle.DisabledAt, err = parseNullableTimePtr(disabledAt); err != nil {
		return hub.Circle{}, err
	}
	return circle, nil
}

func (repository *Repository) ListCircles(ctx context.Context) ([]hub.Circle, error) {
	rows, err := repository.executor().QueryContext(ctx, `
SELECT hub_id, circle_id, alias, state, active_key_version, created_at, disabled_at
FROM hub_circle ORDER BY created_at, circle_id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	circles := make([]hub.Circle, 0)
	for rows.Next() {
		var circle hub.Circle
		var state, createdAt string
		var disabledAt sql.NullString
		if err := rows.Scan(&circle.HubID, &circle.CircleID, &circle.Alias, &state, &circle.ActiveKeyVersion, &createdAt, &disabledAt); err != nil {
			return nil, err
		}
		circle.State = hub.CircleState(state)
		if circle.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
			return nil, err
		}
		if circle.DisabledAt, err = parseNullableTimePtr(disabledAt); err != nil {
			return nil, err
		}
		circles = append(circles, circle)
	}
	return circles, rows.Err()
}

func (repository *Repository) CountDynamicCircles(ctx context.Context, protectedIDs []string) (int, error) {
	protected := make(map[string]struct{}, len(protectedIDs)+1)
	protected["public"] = struct{}{}
	for _, id := range protectedIDs {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			protected[trimmed] = struct{}{}
		}
	}
	rows, err := repository.executor().QueryContext(ctx, `
SELECT circle_id FROM hub_circle
WHERE circle_id <> 'public' AND alias = ''`)
	if err != nil {
		return 0, err
	}
	defer func() { _ = rows.Close() }()
	count := 0
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return 0, err
		}
		if _, skip := protected[id]; !skip {
			count++
		}
	}
	return count, rows.Err()
}

func (repository *Repository) PruneEmptyCircles(ctx context.Context, cutoff time.Time, protectedIDs []string) (int64, error) {
	protected := make(map[string]struct{}, len(protectedIDs)+1)
	protected["public"] = struct{}{}
	for _, id := range protectedIDs {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			protected[trimmed] = struct{}{}
		}
	}
	cutoffStr := formatTime(cutoff)
	var pruned int64
	err := repository.withTransaction(ctx, func(tx *Repository) error {
		rows, err := tx.executor().QueryContext(ctx, `
SELECT c.hub_id, c.circle_id
FROM hub_circle c
WHERE c.circle_id <> 'public'
  AND c.alias = ''
  AND c.state = 'ACTIVE'
  AND c.created_at <= ?
  AND NOT EXISTS (SELECT 1 FROM hub_circle_key k WHERE k.hub_id = c.hub_id AND k.circle_id = c.circle_id AND (k.version > 1 OR k.state = 'REVOKED'))
  AND NOT EXISTS (SELECT 1 FROM agent a WHERE a.hub_id = c.hub_id AND a.circle_id = c.circle_id)
  AND NOT EXISTS (SELECT 1 FROM agent_group g WHERE g.hub_id = c.hub_id AND g.circle_id = c.circle_id AND g.created_at > ?)
  AND NOT EXISTS (SELECT 1 FROM inbox_item i WHERE i.hub_id = c.hub_id AND i.circle_id = c.circle_id AND i.created_at > ?)
  AND NOT EXISTS (SELECT 1 FROM a2a_task t WHERE t.hub_id = c.hub_id AND t.circle_id = c.circle_id AND t.updated_at > ?)
  AND NOT EXISTS (SELECT 1 FROM event_log e WHERE e.hub_id = c.hub_id AND e.circle_id = c.circle_id AND e.event_type NOT IN ('circle.skill_proposed', 'circle.skill_approved', 'circle.skill_rejected', 'circle.skill_updated', 'circle.skill_deleted', 'circle.skill_expired') AND e.created_at > ?)`,
			cutoffStr, cutoffStr, cutoffStr, cutoffStr, cutoffStr)
		if err != nil {
			return err
		}
		type targetCircle struct {
			hubID    string
			circleID string
		}
		var targets []targetCircle
		for rows.Next() {
			var item targetCircle
			if err := rows.Scan(&item.hubID, &item.circleID); err != nil {
				_ = rows.Close()
				return err
			}
			if _, skip := protected[item.circleID]; !skip {
				targets = append(targets, item)
			}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}

		for _, item := range targets {
			if _, err := tx.executor().ExecContext(ctx, `DELETE FROM a2a_task_update WHERE hub_id = ? AND task_id IN (SELECT task_id FROM a2a_task WHERE hub_id = ? AND circle_id = ?)`, item.hubID, item.hubID, item.circleID); err != nil {
				return err
			}
			for _, stmt := range []string{
				`DELETE FROM a2a_task_event WHERE hub_id = ? AND circle_id = ?`,
				`DELETE FROM a2a_group_task_member WHERE hub_id = ? AND circle_id = ?`,
				`DELETE FROM a2a_task WHERE hub_id = ? AND circle_id = ?`,
				`DELETE FROM agent_workflow WHERE hub_id = ? AND circle_id = ?`,
				`DELETE FROM group_charter_revision WHERE hub_id = ? AND circle_id = ?`,
				`DELETE FROM group_secretary WHERE hub_id = ? AND circle_id = ?`,
				`DELETE FROM meeting_session WHERE hub_id = ? AND circle_id = ?`,
				`DELETE FROM group_delivery WHERE hub_id = ? AND circle_id = ?`,
				`DELETE FROM group_message WHERE hub_id = ? AND circle_id = ?`,
				`DELETE FROM group_invitation WHERE hub_id = ? AND circle_id = ?`,
				`DELETE FROM group_member WHERE hub_id = ? AND circle_id = ?`,
				`DELETE FROM agent_group WHERE hub_id = ? AND circle_id = ?`,
				`DELETE FROM inbox_item WHERE hub_id = ? AND circle_id = ?`,
				`DELETE FROM event_log WHERE hub_id = ? AND circle_id = ?`,
				`DELETE FROM circle_skill_idempotency WHERE hub_id = ? AND circle_id = ?`,
				`DELETE FROM circle_skill_revisions WHERE hub_id = ? AND circle_id = ?`,
				`DELETE FROM circle_skill_proposals WHERE hub_id = ? AND circle_id = ?`,
				`DELETE FROM circle_skills WHERE hub_id = ? AND circle_id = ?`,
				`DELETE FROM circle_skill_collections WHERE hub_id = ? AND circle_id = ?`,
				`DELETE FROM hub_circle_key WHERE hub_id = ? AND circle_id = ?`,
				`DELETE FROM hub_circle WHERE hub_id = ? AND circle_id = ?`,
			} {
				if _, err := tx.executor().ExecContext(ctx, stmt, item.hubID, item.circleID); err != nil {
					return err
				}
			}
			pruned++
		}
		return nil
	})
	return pruned, err
}

func (repository *Repository) SetCircleState(ctx context.Context, circleID string, state hub.CircleState, disabledAt *time.Time) error {
	result, err := repository.executor().ExecContext(ctx, `
UPDATE hub_circle SET state = ?, disabled_at = ? WHERE circle_id = ?`,
		string(state), nullTimePtr(disabledAt), circleID)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return store.ErrNotFound
	}
	return nil
}

func (repository *Repository) CreateCircleKey(ctx context.Context, key hub.CircleKey) error {
	if strings.TrimSpace(key.HubID) == "" || strings.TrimSpace(key.CircleID) == "" || key.Version < 1 || strings.TrimSpace(key.KeyDigest) == "" {
		return errors.New("circle key requires hub id, circle id, version, and digest")
	}
	if key.State == "" {
		key.State = "ACTIVE"
	}
	if key.CreatedAt.IsZero() {
		key.CreatedAt = time.Now().UTC()
	}
	_, err := repository.executor().ExecContext(ctx, `
INSERT INTO hub_circle_key (hub_id, circle_id, version, key_digest, state, created_at, grace_until, revoked_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (hub_id, circle_id, version) DO UPDATE SET
    key_digest = excluded.key_digest, state = excluded.state,
    grace_until = excluded.grace_until, revoked_at = excluded.revoked_at`,
		key.HubID, key.CircleID, key.Version, key.KeyDigest, key.State, formatTime(key.CreatedAt),
		nullTimePtr(key.GraceUntil), nullTimePtr(key.RevokedAt))
	return err
}

func (repository *Repository) RotateCircleKey(ctx context.Context, circleID string, key hub.CircleKey, graceUntil *time.Time) error {
	if key.Version < 1 || strings.TrimSpace(key.KeyDigest) == "" {
		return errors.New("rotated circle key requires version and digest")
	}
	return repository.withTransaction(ctx, func(tx *Repository) error {
		if _, err := tx.executor().ExecContext(ctx, `
UPDATE hub_circle_key SET state = 'GRACE', grace_until = ?
WHERE circle_id = ? AND state = 'ACTIVE'`, nullTimePtr(graceUntil), circleID); err != nil {
			return err
		}
		key.State = "ACTIVE"
		if key.CreatedAt.IsZero() {
			key.CreatedAt = time.Now().UTC()
		}
		if _, err := tx.executor().ExecContext(ctx, `
INSERT INTO hub_circle_key (hub_id, circle_id, version, key_digest, state, created_at, grace_until, revoked_at)
VALUES (?, ?, ?, ?, 'ACTIVE', ?, NULL, NULL)`, key.HubID, key.CircleID, key.Version,
			key.KeyDigest, formatTime(key.CreatedAt)); err != nil {
			return err
		}
		_, err := tx.executor().ExecContext(ctx, `
UPDATE hub_circle SET active_key_version = ? WHERE hub_id = ? AND circle_id = ?`, key.Version, key.HubID, key.CircleID)
		return err
	})
}

func (repository *Repository) FindActiveCircleKey(ctx context.Context, circleID, digest string, now time.Time) (hub.CircleKey, error) {
	var key hub.CircleKey
	var state, createdAt string
	var graceUntil, revokedAt sql.NullString
	err := repository.executor().QueryRowContext(ctx, `
SELECT hub_id, circle_id, version, key_digest, state, created_at, grace_until, revoked_at
FROM hub_circle_key
WHERE circle_id = ? AND key_digest = ?
  AND (state = 'ACTIVE' OR (state = 'GRACE' AND grace_until > ?))
ORDER BY version DESC LIMIT 1`, circleID, digest, formatTime(now)).Scan(
		&key.HubID, &key.CircleID, &key.Version, &key.KeyDigest, &state, &createdAt, &graceUntil, &revokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return hub.CircleKey{}, store.ErrNotFound
	}
	if err != nil {
		return hub.CircleKey{}, err
	}
	key.State = state
	if key.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
		return hub.CircleKey{}, err
	}
	if key.GraceUntil, err = parseNullableTimePtr(graceUntil); err != nil {
		return hub.CircleKey{}, err
	}
	if key.RevokedAt, err = parseNullableTimePtr(revokedAt); err != nil {
		return hub.CircleKey{}, err
	}
	return key, nil
}

func (repository *Repository) FindCircleByKeyDigest(ctx context.Context, digest string, now time.Time) (hub.Circle, error) {
	var circle hub.Circle
	var state string
	var createdAt string
	var disabledAt sql.NullString
	err := repository.executor().QueryRowContext(ctx, `
SELECT c.hub_id, c.circle_id, c.alias, c.state, c.active_key_version, c.created_at, c.disabled_at
FROM hub_circle c
JOIN hub_circle_key k ON k.hub_id = c.hub_id AND k.circle_id = c.circle_id
WHERE k.key_digest = ?
  AND (k.state = 'ACTIVE' OR (k.state = 'GRACE' AND k.grace_until > ?))
ORDER BY k.version DESC LIMIT 1`, digest, formatTime(now)).Scan(
		&circle.HubID, &circle.CircleID, &circle.Alias, &state, &circle.ActiveKeyVersion, &createdAt, &disabledAt)
	if errors.Is(err, sql.ErrNoRows) {
		return hub.Circle{}, store.ErrNotFound
	}
	if err != nil {
		return hub.Circle{}, err
	}
	circle.State = hub.CircleState(state)
	if circle.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
		return hub.Circle{}, err
	}
	if circle.DisabledAt, err = parseNullableTimePtr(disabledAt); err != nil {
		return hub.Circle{}, err
	}
	return circle, nil
}

func (repository *Repository) ListCircleKeys(ctx context.Context, circleID string) ([]hub.CircleKey, error) {
	rows, err := repository.executor().QueryContext(ctx, `
SELECT hub_id, circle_id, version, key_digest, state, created_at, grace_until, revoked_at
FROM hub_circle_key WHERE circle_id = ? ORDER BY version`, circleID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	keys := make([]hub.CircleKey, 0)
	for rows.Next() {
		var key hub.CircleKey
		var state, createdAt string
		var graceUntil, revokedAt sql.NullString
		if err := rows.Scan(&key.HubID, &key.CircleID, &key.Version, &key.KeyDigest, &state, &createdAt, &graceUntil, &revokedAt); err != nil {
			return nil, err
		}
		key.State = state
		if key.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
			return nil, err
		}
		if key.GraceUntil, err = parseNullableTimePtr(graceUntil); err != nil {
			return nil, err
		}
		if key.RevokedAt, err = parseNullableTimePtr(revokedAt); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

func (repository *Repository) RevokeCircleKey(ctx context.Context, circleID string, version int, revokedAt time.Time) error {
	result, err := repository.executor().ExecContext(ctx, `
UPDATE hub_circle_key SET state = 'REVOKED', revoked_at = ?, grace_until = NULL
WHERE circle_id = ? AND version = ? AND state <> 'REVOKED'`, formatTime(revokedAt), circleID, version)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return store.ErrNotFound
	}
	return nil
}
