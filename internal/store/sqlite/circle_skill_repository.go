package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/tbdavid2019/888a2a-lite/internal/hub"
	"github.com/tbdavid2019/888a2a-lite/internal/store"
)

func migrateCircleSkills(ctx context.Context, database *sql.DB) error {
	var applied int
	if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version = 11").Scan(&applied); err != nil {
		return err
	}
	if applied > 0 {
		return nil
	}
	statements := []string{
		`CREATE TABLE circle_skill_collections (
    hub_id TEXT NOT NULL,
    circle_id TEXT NOT NULL,
    collection_version INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (hub_id, circle_id)
)`,
		`CREATE TABLE circle_skills (
    hub_id TEXT NOT NULL,
    circle_id TEXT NOT NULL,
    slug TEXT NOT NULL,
    title TEXT NOT NULL,
    summary TEXT NOT NULL,
    content TEXT NOT NULL,
    version INTEGER NOT NULL,
    content_hash TEXT NOT NULL,
    byte_size INTEGER NOT NULL,
    ttl_seconds INTEGER,
    expires_at TEXT,
    expires_at_unix INTEGER NOT NULL DEFAULT 0,
    proposed_by TEXT NOT NULL DEFAULT '',
    approved_by TEXT NOT NULL DEFAULT '',
    updated_by TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    deleted_at TEXT,
    PRIMARY KEY (hub_id, circle_id, slug)
)`,
		`CREATE INDEX idx_circle_skills_active ON circle_skills (hub_id, circle_id, deleted_at, expires_at_unix)`,
		`CREATE INDEX idx_circle_skills_expiry ON circle_skills (expires_at_unix) WHERE deleted_at IS NULL AND expires_at_unix > 0`,
		`CREATE TABLE circle_skill_proposals (
    hub_id TEXT NOT NULL,
    circle_id TEXT NOT NULL,
    slug TEXT NOT NULL,
    base_version INTEGER NOT NULL DEFAULT 0,
    title TEXT NOT NULL,
    summary TEXT NOT NULL,
    content TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    byte_size INTEGER NOT NULL,
    requested_ttl_seconds INTEGER,
    proposed_by TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('PENDING_REVIEW', 'APPROVED', 'REJECTED')),
    rejection_reason TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    reviewed_by TEXT NOT NULL DEFAULT '',
    reviewed_at TEXT,
    PRIMARY KEY (hub_id, circle_id, slug)
)`,
		`CREATE INDEX idx_circle_skill_proposals_status ON circle_skill_proposals (hub_id, circle_id, status, updated_at DESC)`,
		`CREATE TABLE circle_skill_revisions (
    hub_id TEXT NOT NULL,
    circle_id TEXT NOT NULL,
    slug TEXT NOT NULL,
    version INTEGER NOT NULL,
    content_hash TEXT NOT NULL,
    byte_size INTEGER NOT NULL,
    content TEXT NOT NULL,
    action TEXT NOT NULL,
    expires_at TEXT,
    updated_by TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    PRIMARY KEY (hub_id, circle_id, slug, version)
)`,
		`CREATE TABLE circle_skill_idempotency (
    hub_id TEXT NOT NULL,
    circle_id TEXT NOT NULL,
    method TEXT NOT NULL,
    slug TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    response_json TEXT NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY (hub_id, circle_id, method, slug, idempotency_key)
)`,
		`INSERT INTO schema_migrations (version, applied_at) VALUES (11, CURRENT_TIMESTAMP)`,
	}
	for _, statement := range statements {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("migrate circle skills schema: %w", err)
		}
	}
	return nil
}

type cachedSkillIdempotencyPayload struct {
	Skill             hub.CircleSkill         `json:"skill,omitempty"`
	Proposal          hub.CircleSkillProposal `json:"proposal,omitempty"`
	Version           int64                   `json:"version,omitempty"`
	CollectionVersion int64                   `json:"collectionVersion"`
	IsUpdate          bool                    `json:"isUpdate,omitempty"`
}

func computeRequestHash(parts ...any) (string, error) {
	encoded, err := json.Marshal(parts)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func (repository *Repository) getCircleCollectionVersionTx(ctx context.Context, hubID, circleID string) (int64, error) {
	var colVersion int64
	err := repository.executor().QueryRowContext(ctx,
		`SELECT collection_version FROM circle_skill_collections WHERE hub_id = ? AND circle_id = ?`,
		hubID, circleID,
	).Scan(&colVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return colVersion, err
}

func (repository *Repository) bumpCircleCollectionVersionTx(ctx context.Context, hubID, circleID string, now time.Time) (int64, error) {
	nowStr := formatTime(now)
	if _, err := repository.executor().ExecContext(ctx, `
INSERT INTO circle_skill_collections (hub_id, circle_id, collection_version, updated_at)
VALUES (?, ?, 1, ?)
ON CONFLICT (hub_id, circle_id) DO UPDATE SET
    collection_version = circle_skill_collections.collection_version + 1,
    updated_at = excluded.updated_at`,
		hubID, circleID, nowStr,
	); err != nil {
		return 0, err
	}
	return repository.getCircleCollectionVersionTx(ctx, hubID, circleID)
}

func (repository *Repository) lookupSkillIdempotencyTx(ctx context.Context, hubID, circleID, method, slug, idempotencyKey, requestHash string) (*cachedSkillIdempotencyPayload, error) {
	var storedHash, rawResp string
	err := repository.executor().QueryRowContext(ctx, `
SELECT request_hash, response_json
FROM circle_skill_idempotency
WHERE hub_id = ? AND circle_id = ? AND method = ? AND slug = ? AND idempotency_key = ?`,
		hubID, circleID, method, slug, idempotencyKey,
	).Scan(&storedHash, &rawResp)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if storedHash != requestHash {
		return nil, store.ErrConflict
	}
	var cached cachedSkillIdempotencyPayload
	if err := json.Unmarshal([]byte(rawResp), &cached); err != nil {
		return nil, err
	}
	return &cached, nil
}

func (repository *Repository) saveSkillIdempotencyTx(ctx context.Context, hubID, circleID, method, slug, idempotencyKey, requestHash string, payload cachedSkillIdempotencyPayload, now time.Time) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = repository.executor().ExecContext(ctx, `
INSERT INTO circle_skill_idempotency (hub_id, circle_id, method, slug, idempotency_key, request_hash, response_json, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		hubID, circleID, method, slug, idempotencyKey, requestHash, string(encoded), formatTime(now),
	)
	return err
}

func (repository *Repository) pruneSkillRevisionsTx(ctx context.Context, hubID, circleID, slug string) error {
	_, err := repository.executor().ExecContext(ctx, `
DELETE FROM circle_skill_revisions
WHERE hub_id = ? AND circle_id = ? AND slug = ?
  AND version NOT IN (
    SELECT version FROM circle_skill_revisions
    WHERE hub_id = ? AND circle_id = ? AND slug = ?
    ORDER BY version DESC
    LIMIT ?
  )`,
		hubID, circleID, slug, hubID, circleID, slug, hub.MaxCircleSkillRevisionsPerSlug,
	)
	return err
}

func (repository *Repository) ListCircleSkillManifest(ctx context.Context, hubID, circleID string, now time.Time) (hub.CircleSkillManifest, error) {
	colVersion, err := repository.getCircleCollectionVersionTx(ctx, hubID, circleID)
	if err != nil {
		return hub.CircleSkillManifest{}, err
	}
	nowUnix := now.UTC().UnixNano()
	rows, err := repository.executor().QueryContext(ctx, `
SELECT slug, title, summary, version, content_hash, byte_size, ttl_seconds, expires_at, proposed_by, approved_by, updated_by, updated_at
FROM circle_skills
WHERE hub_id = ? AND circle_id = ?
  AND deleted_at IS NULL
  AND (expires_at_unix = 0 OR expires_at_unix > ?)
ORDER BY slug ASC`,
		hubID, circleID, nowUnix,
	)
	if err != nil {
		return hub.CircleSkillManifest{}, err
	}
	defer func() { _ = rows.Close() }()

	items := make([]hub.CircleSkillManifestItem, 0)
	for rows.Next() {
		var item hub.CircleSkillManifestItem
		var ttlVal sql.NullInt64
		var expVal sql.NullString
		if err := rows.Scan(
			&item.Slug, &item.Title, &item.Summary, &item.Version, &item.ContentHash,
			&item.ByteSize, &ttlVal, &expVal, &item.ProposedBy, &item.ApprovedBy,
			&item.UpdatedBy, &item.UpdatedAt,
		); err != nil {
			return hub.CircleSkillManifest{}, err
		}
		if ttlVal.Valid {
			v := ttlVal.Int64
			item.TTLSeconds = &v
		}
		if expVal.Valid && expVal.String != "" {
			s := expVal.String
			item.ExpiresAt = &s
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return hub.CircleSkillManifest{}, err
	}
	return hub.CircleSkillManifest{
		HubID:             hubID,
		CircleID:          circleID,
		CollectionVersion: colVersion,
		SkillCount:        len(items),
		ETag:              hub.CircleSkillManifestETag(circleID, colVersion),
		Skills:            items,
	}, nil
}

func (repository *Repository) GetCircleSkill(ctx context.Context, hubID, circleID, slug string, now time.Time) (hub.CircleSkill, error) {
	nowUnix := now.UTC().UnixNano()
	var skill hub.CircleSkill
	var ttlVal sql.NullInt64
	var expVal sql.NullString
	var delVal sql.NullString
	err := repository.executor().QueryRowContext(ctx, `
SELECT hub_id, circle_id, slug, title, summary, content, version, content_hash, byte_size, ttl_seconds, expires_at, proposed_by, approved_by, updated_by, created_at, updated_at, deleted_at
FROM circle_skills
WHERE hub_id = ? AND circle_id = ? AND slug = ?
  AND deleted_at IS NULL
  AND (expires_at_unix = 0 OR expires_at_unix > ?)`,
		hubID, circleID, slug, nowUnix,
	).Scan(
		&skill.HubID, &skill.CircleID, &skill.Slug, &skill.Title, &skill.Summary, &skill.Content,
		&skill.Version, &skill.ContentHash, &skill.ByteSize, &ttlVal, &expVal,
		&skill.ProposedBy, &skill.ApprovedBy, &skill.UpdatedBy, &skill.CreatedAt, &skill.UpdatedAt, &delVal,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return hub.CircleSkill{}, store.ErrNotFound
	}
	if err != nil {
		return hub.CircleSkill{}, err
	}
	if ttlVal.Valid {
		v := ttlVal.Int64
		skill.TTLSeconds = &v
	}
	if expVal.Valid && expVal.String != "" {
		s := expVal.String
		skill.ExpiresAt = &s
	}
	if delVal.Valid && delVal.String != "" {
		s := delVal.String
		skill.DeletedAt = &s
	}
	return skill, nil
}

func (repository *Repository) putCircleSkillTx(ctx context.Context, skill hub.CircleSkill, expectedVersion int64, now time.Time) (hub.CircleSkill, int64, error) {
	nowUTC := now.UTC()
	nowStr := formatTime(nowUTC)
	nowUnix := nowUTC.UnixNano()

	var currentVersion int64
	var deletedAt sql.NullString
	var expUnix int64
	var createdAt string
	rowExists := true
	err := repository.executor().QueryRowContext(ctx, `
SELECT version, deleted_at, expires_at_unix, created_at
FROM circle_skills
WHERE hub_id = ? AND circle_id = ? AND slug = ?`,
		skill.HubID, skill.CircleID, skill.Slug,
	).Scan(&currentVersion, &deletedAt, &expUnix, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		rowExists = false
		currentVersion = 0
	} else if err != nil {
		return hub.CircleSkill{}, 0, err
	}

	isActive := rowExists && !deletedAt.Valid && (expUnix == 0 || expUnix > nowUnix)
	if isActive {
		if expectedVersion != currentVersion {
			return hub.CircleSkill{}, 0, store.ErrConflict
		}
	} else {
		if expectedVersion != 0 && expectedVersion != currentVersion {
			return hub.CircleSkill{}, 0, store.ErrConflict
		}
	}

	var activeCount int
	var activeBytes int
	if err := repository.executor().QueryRowContext(ctx, `
SELECT COUNT(*), COALESCE(SUM(byte_size), 0)
FROM circle_skills
WHERE hub_id = ? AND circle_id = ? AND slug <> ?
  AND deleted_at IS NULL
  AND (expires_at_unix = 0 OR expires_at_unix > ?)`,
		skill.HubID, skill.CircleID, skill.Slug, nowUnix,
	).Scan(&activeCount, &activeBytes); err != nil {
		return hub.CircleSkill{}, 0, err
	}
	if activeCount >= hub.MaxCircleSkillsPerCircle {
		return hub.CircleSkill{}, 0, store.ErrLimitReached
	}
	if activeBytes+len(skill.Content) > hub.MaxCircleSkillTotalBytes {
		return hub.CircleSkill{}, 0, store.ErrLimitReached
	}

	nextVersion := currentVersion + 1
	if createdAt == "" {
		createdAt = nowStr
	}

	var ttlArg sql.NullInt64
	var expArg sql.NullString
	var expUnixArg int64
	var ttlCopy *int64
	var expCopy *string
	if skill.TTLSeconds != nil && *skill.TTLSeconds > 0 {
		v := *skill.TTLSeconds
		expTime := nowUTC.Add(time.Duration(v) * time.Second)
		expStr := formatTime(expTime)
		ttlArg = sql.NullInt64{Int64: v, Valid: true}
		expArg = sql.NullString{String: expStr, Valid: true}
		expUnixArg = expTime.UnixNano()
		ttlCopy = &v
		expCopy = &expStr
	}

	if _, err := repository.executor().ExecContext(ctx, `
INSERT INTO circle_skills (
    hub_id, circle_id, slug, title, summary, content, version, content_hash, byte_size,
    ttl_seconds, expires_at, expires_at_unix, proposed_by, approved_by, updated_by,
    created_at, updated_at, deleted_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL)
ON CONFLICT (hub_id, circle_id, slug) DO UPDATE SET
    title = excluded.title,
    summary = excluded.summary,
    content = excluded.content,
    version = excluded.version,
    content_hash = excluded.content_hash,
    byte_size = excluded.byte_size,
    ttl_seconds = excluded.ttl_seconds,
    expires_at = excluded.expires_at,
    expires_at_unix = excluded.expires_at_unix,
    proposed_by = excluded.proposed_by,
    approved_by = excluded.approved_by,
    updated_by = excluded.updated_by,
    updated_at = excluded.updated_at,
    deleted_at = NULL`,
		skill.HubID, skill.CircleID, skill.Slug, skill.Title, skill.Summary, skill.Content,
		nextVersion, skill.ContentHash, len(skill.Content),
		ttlArg, expArg, expUnixArg, skill.ProposedBy, skill.ApprovedBy, skill.UpdatedBy,
		createdAt, nowStr,
	); err != nil {
		return hub.CircleSkill{}, 0, err
	}

	if _, err := repository.executor().ExecContext(ctx, `
INSERT INTO circle_skill_revisions (
    hub_id, circle_id, slug, version, content_hash, byte_size, content, action, expires_at, updated_by, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		skill.HubID, skill.CircleID, skill.Slug, nextVersion, skill.ContentHash, len(skill.Content),
		skill.Content, hub.SkillEventActionUpdated, expArg, skill.UpdatedBy, nowStr,
	); err != nil {
		return hub.CircleSkill{}, 0, err
	}
	if err := repository.pruneSkillRevisionsTx(ctx, skill.HubID, skill.CircleID, skill.Slug); err != nil {
		return hub.CircleSkill{}, 0, err
	}

	colVersion, err := repository.bumpCircleCollectionVersionTx(ctx, skill.HubID, skill.CircleID, nowUTC)
	if err != nil {
		return hub.CircleSkill{}, 0, err
	}

	saved := skill
	saved.Version = nextVersion
	saved.ByteSize = len(skill.Content)
	saved.TTLSeconds = ttlCopy
	saved.ExpiresAt = expCopy
	saved.CreatedAt = createdAt
	saved.UpdatedAt = nowStr
	saved.DeletedAt = nil
	return saved, colVersion, nil
}

func (repository *Repository) PutCircleSkill(ctx context.Context, skill hub.CircleSkill, expectedVersion int64, idempotencyKey string, now time.Time) (hub.CircleSkill, int64, bool, error) {
	reqHash, err := computeRequestHash("PUT_SKILL", skill.Slug, skill.Title, skill.Summary, skill.ContentHash, skill.TTLSeconds, expectedVersion)
	if err != nil {
		return hub.CircleSkill{}, 0, false, err
	}
	var saved hub.CircleSkill
	var colVersion int64
	var duplicate bool
	err = repository.withTransaction(ctx, func(tx *Repository) error {
		cached, err := tx.lookupSkillIdempotencyTx(ctx, skill.HubID, skill.CircleID, "PUT_SKILL", skill.Slug, idempotencyKey, reqHash)
		if err != nil {
			return err
		}
		if cached != nil {
			saved = cached.Skill
			colVersion = cached.CollectionVersion
			duplicate = true
			return nil
		}
		saved, colVersion, err = tx.putCircleSkillTx(ctx, skill, expectedVersion, now)
		if err != nil {
			return err
		}
		return tx.saveSkillIdempotencyTx(ctx, skill.HubID, skill.CircleID, "PUT_SKILL", skill.Slug, idempotencyKey, reqHash, cachedSkillIdempotencyPayload{
			Skill:             saved,
			CollectionVersion: colVersion,
		}, now)
	})
	return saved, colVersion, duplicate, err
}

func (repository *Repository) DeleteCircleSkill(ctx context.Context, hubID, circleID, slug, actor string, expectedVersion int64, idempotencyKey string, now time.Time) (int64, int64, bool, error) {
	reqHash, err := computeRequestHash("DELETE_SKILL", slug, expectedVersion)
	if err != nil {
		return 0, 0, false, err
	}
	var deletedVersion, colVersion int64
	var duplicate bool
	err = repository.withTransaction(ctx, func(tx *Repository) error {
		cached, err := tx.lookupSkillIdempotencyTx(ctx, hubID, circleID, "DELETE_SKILL", slug, idempotencyKey, reqHash)
		if err != nil {
			return err
		}
		if cached != nil {
			deletedVersion = cached.Version
			colVersion = cached.CollectionVersion
			duplicate = true
			return nil
		}
		nowUTC := now.UTC()
		nowStr := formatTime(nowUTC)
		nowUnix := nowUTC.UnixNano()

		var currentVersion int64
		var deletedAt sql.NullString
		var expUnix int64
		var prevHash string
		qErr := tx.executor().QueryRowContext(ctx, `
SELECT version, deleted_at, expires_at_unix, content_hash
FROM circle_skills
WHERE hub_id = ? AND circle_id = ? AND slug = ?`,
			hubID, circleID, slug,
		).Scan(&currentVersion, &deletedAt, &expUnix, &prevHash)
		if errors.Is(qErr, sql.ErrNoRows) {
			return store.ErrNotFound
		}
		if qErr != nil {
			return qErr
		}
		if deletedAt.Valid || (expUnix > 0 && expUnix <= nowUnix) {
			return store.ErrNotFound
		}
		if expectedVersion != currentVersion {
			return store.ErrConflict
		}

		nextVersion := currentVersion + 1
		if _, err := tx.executor().ExecContext(ctx, `
UPDATE circle_skills
SET version = ?, content = '', byte_size = 0, deleted_at = ?, updated_by = ?, updated_at = ?
WHERE hub_id = ? AND circle_id = ? AND slug = ?`,
			nextVersion, nowStr, actor, nowStr, hubID, circleID, slug,
		); err != nil {
			return err
		}
		if _, err := tx.executor().ExecContext(ctx, `
INSERT INTO circle_skill_revisions (
    hub_id, circle_id, slug, version, content_hash, byte_size, content, action, expires_at, updated_by, created_at
) VALUES (?, ?, ?, ?, ?, 0, '', ?, NULL, ?, ?)`,
			hubID, circleID, slug, nextVersion, prevHash, hub.SkillEventActionDeleted, actor, nowStr,
		); err != nil {
			return err
		}
		if err := tx.pruneSkillRevisionsTx(ctx, hubID, circleID, slug); err != nil {
			return err
		}
		colVersion, err = tx.bumpCircleCollectionVersionTx(ctx, hubID, circleID, nowUTC)
		if err != nil {
			return err
		}
		deletedVersion = nextVersion
		return tx.saveSkillIdempotencyTx(ctx, hubID, circleID, "DELETE_SKILL", slug, idempotencyKey, reqHash, cachedSkillIdempotencyPayload{
			Version:           deletedVersion,
			CollectionVersion: colVersion,
		}, nowUTC)
	})
	return deletedVersion, colVersion, duplicate, err
}

func (repository *Repository) ListCircleSkillRevisions(ctx context.Context, hubID, circleID, slug string) ([]hub.CircleSkillRevision, error) {
	rows, err := repository.executor().QueryContext(ctx, `
SELECT hub_id, circle_id, slug, version, content_hash, byte_size, content, action, expires_at, updated_by, created_at
FROM circle_skill_revisions
WHERE hub_id = ? AND circle_id = ? AND slug = ?
ORDER BY version DESC`,
		hubID, circleID, slug,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	revisions := make([]hub.CircleSkillRevision, 0)
	for rows.Next() {
		var rev hub.CircleSkillRevision
		var expVal sql.NullString
		if err := rows.Scan(
			&rev.HubID, &rev.CircleID, &rev.Slug, &rev.Version, &rev.ContentHash,
			&rev.ByteSize, &rev.Content, &rev.Action, &expVal, &rev.UpdatedBy, &rev.CreatedAt,
		); err != nil {
			return nil, err
		}
		if expVal.Valid && expVal.String != "" {
			s := expVal.String
			rev.ExpiresAt = &s
		}
		revisions = append(revisions, rev)
	}
	return revisions, rows.Err()
}

func (repository *Repository) SubmitCircleSkillProposal(ctx context.Context, proposal hub.CircleSkillProposal, idempotencyKey string, now time.Time) (hub.CircleSkillProposal, int64, bool, bool, error) {
	reqHash, err := computeRequestHash("SUBMIT_PROPOSAL", proposal.Slug, proposal.BaseVersion, proposal.Title, proposal.Summary, proposal.ContentHash, proposal.RequestedTTLSeconds)
	if err != nil {
		return hub.CircleSkillProposal{}, 0, false, false, err
	}
	var saved hub.CircleSkillProposal
	var colVersion int64
	var isUpdate, duplicate bool
	err = repository.withTransaction(ctx, func(tx *Repository) error {
		cached, err := tx.lookupSkillIdempotencyTx(ctx, proposal.HubID, proposal.CircleID, "SUBMIT_PROPOSAL", proposal.Slug, idempotencyKey, reqHash)
		if err != nil {
			return err
		}
		if cached != nil {
			saved = cached.Proposal
			colVersion = cached.CollectionVersion
			isUpdate = cached.IsUpdate
			duplicate = true
			return nil
		}

		nowUTC := now.UTC()
		nowStr := formatTime(nowUTC)

		var existingStatus string
		var existingCreatedAt string
		qErr := tx.executor().QueryRowContext(ctx, `
SELECT status, created_at FROM circle_skill_proposals
WHERE hub_id = ? AND circle_id = ? AND slug = ?`,
			proposal.HubID, proposal.CircleID, proposal.Slug,
		).Scan(&existingStatus, &existingCreatedAt)
		if qErr != nil && !errors.Is(qErr, sql.ErrNoRows) {
			return qErr
		}
		if qErr == nil && existingStatus == hub.SkillProposalStatusPending {
			isUpdate = true
		} else {
			var pendingCount int
			if err := tx.executor().QueryRowContext(ctx, `
SELECT COUNT(*) FROM circle_skill_proposals
WHERE hub_id = ? AND circle_id = ? AND status = ?`,
				proposal.HubID, proposal.CircleID, hub.SkillProposalStatusPending,
			).Scan(&pendingCount); err != nil {
				return err
			}
			if pendingCount >= hub.MaxCircleSkillProposalsPerCircle {
				return store.ErrLimitReached
			}
		}

		createdAt := nowStr
		if existingCreatedAt != "" && isUpdate {
			createdAt = existingCreatedAt
		}
		var reqTTL sql.NullInt64
		var reqTTLCopy *int64
		if proposal.RequestedTTLSeconds != nil && *proposal.RequestedTTLSeconds > 0 {
			v := *proposal.RequestedTTLSeconds
			reqTTL = sql.NullInt64{Int64: v, Valid: true}
			reqTTLCopy = &v
		}

		if _, err := tx.executor().ExecContext(ctx, `
INSERT INTO circle_skill_proposals (
    hub_id, circle_id, slug, base_version, title, summary, content, content_hash, byte_size,
    requested_ttl_seconds, proposed_by, status, rejection_reason, created_at, updated_at, reviewed_by, reviewed_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', ?, ?, '', NULL)
ON CONFLICT (hub_id, circle_id, slug) DO UPDATE SET
    base_version = excluded.base_version,
    title = excluded.title,
    summary = excluded.summary,
    content = excluded.content,
    content_hash = excluded.content_hash,
    byte_size = excluded.byte_size,
    requested_ttl_seconds = excluded.requested_ttl_seconds,
    proposed_by = excluded.proposed_by,
    status = excluded.status,
    rejection_reason = '',
    created_at = excluded.created_at,
    updated_at = excluded.updated_at,
    reviewed_by = '',
    reviewed_at = NULL`,
			proposal.HubID, proposal.CircleID, proposal.Slug, proposal.BaseVersion,
			proposal.Title, proposal.Summary, proposal.Content, proposal.ContentHash, len(proposal.Content),
			reqTTL, proposal.ProposedBy, hub.SkillProposalStatusPending, createdAt, nowStr,
		); err != nil {
			return err
		}

		colVersion, err = tx.getCircleCollectionVersionTx(ctx, proposal.HubID, proposal.CircleID)
		if err != nil {
			return err
		}
		saved = proposal
		saved.ByteSize = len(proposal.Content)
		saved.RequestedTTLSeconds = reqTTLCopy
		saved.Status = hub.SkillProposalStatusPending
		saved.RejectionReason = ""
		saved.CreatedAt = createdAt
		saved.UpdatedAt = nowStr
		saved.ReviewedBy = ""
		saved.ReviewedAt = nil

		return tx.saveSkillIdempotencyTx(ctx, proposal.HubID, proposal.CircleID, "SUBMIT_PROPOSAL", proposal.Slug, idempotencyKey, reqHash, cachedSkillIdempotencyPayload{
			Proposal:          saved,
			CollectionVersion: colVersion,
			IsUpdate:          isUpdate,
		}, nowUTC)
	})
	return saved, colVersion, isUpdate, duplicate, err
}

func (repository *Repository) ListCircleSkillProposals(ctx context.Context, hubID, circleID string) ([]hub.CircleSkillProposal, error) {
	rows, err := repository.executor().QueryContext(ctx, `
SELECT hub_id, circle_id, slug, base_version, title, summary, content, content_hash, byte_size, requested_ttl_seconds, proposed_by, status, rejection_reason, created_at, updated_at, reviewed_by, reviewed_at
FROM circle_skill_proposals
WHERE hub_id = ? AND circle_id = ?
ORDER BY CASE WHEN status = 'PENDING_REVIEW' THEN 0 ELSE 1 END, updated_at DESC, slug ASC`,
		hubID, circleID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	proposals := make([]hub.CircleSkillProposal, 0)
	for rows.Next() {
		p, err := scanCircleSkillProposal(rows)
		if err != nil {
			return nil, err
		}
		proposals = append(proposals, p)
	}
	return proposals, rows.Err()
}

func (repository *Repository) GetCircleSkillProposal(ctx context.Context, hubID, circleID, slug string) (hub.CircleSkillProposal, error) {
	row := repository.executor().QueryRowContext(ctx, `
SELECT hub_id, circle_id, slug, base_version, title, summary, content, content_hash, byte_size, requested_ttl_seconds, proposed_by, status, rejection_reason, created_at, updated_at, reviewed_by, reviewed_at
FROM circle_skill_proposals
WHERE hub_id = ? AND circle_id = ? AND slug = ?`,
		hubID, circleID, slug,
	)
	p, err := scanCircleSkillProposal(row)
	if errors.Is(err, sql.ErrNoRows) {
		return hub.CircleSkillProposal{}, store.ErrNotFound
	}
	return p, err
}

func scanCircleSkillProposal(scanner interface{ Scan(...any) error }) (hub.CircleSkillProposal, error) {
	var p hub.CircleSkillProposal
	var reqTTL sql.NullInt64
	var reviewedAt sql.NullString
	if err := scanner.Scan(
		&p.HubID, &p.CircleID, &p.Slug, &p.BaseVersion, &p.Title, &p.Summary,
		&p.Content, &p.ContentHash, &p.ByteSize, &reqTTL, &p.ProposedBy,
		&p.Status, &p.RejectionReason, &p.CreatedAt, &p.UpdatedAt, &p.ReviewedBy, &reviewedAt,
	); err != nil {
		return hub.CircleSkillProposal{}, err
	}
	if reqTTL.Valid {
		v := reqTTL.Int64
		p.RequestedTTLSeconds = &v
	}
	if reviewedAt.Valid && reviewedAt.String != "" {
		s := reviewedAt.String
		p.ReviewedAt = &s
	}
	return p, nil
}

func (repository *Repository) ApproveCircleSkillProposal(ctx context.Context, hubID, circleID, slug, reviewer string, decision hub.CircleSkillDecisionInput, now time.Time) (hub.CircleSkill, int64, bool, error) {
	reqHash, err := computeRequestHash("APPROVE_PROPOSAL", slug, decision.ExpectedVersion, decision.TTLSeconds)
	if err != nil {
		return hub.CircleSkill{}, 0, false, err
	}
	var saved hub.CircleSkill
	var colVersion int64
	var duplicate bool
	err = repository.withTransaction(ctx, func(tx *Repository) error {
		cached, err := tx.lookupSkillIdempotencyTx(ctx, hubID, circleID, "APPROVE_PROPOSAL", slug, decision.IdempotencyKey, reqHash)
		if err != nil {
			return err
		}
		if cached != nil {
			saved = cached.Skill
			colVersion = cached.CollectionVersion
			duplicate = true
			return nil
		}
		proposal, err := tx.GetCircleSkillProposal(ctx, hubID, circleID, slug)
		if err != nil {
			return err
		}
		if proposal.Status != hub.SkillProposalStatusPending {
			return store.ErrConflict
		}
		ttl := proposal.RequestedTTLSeconds
		if decision.TTLSeconds != nil {
			ttl = decision.TTLSeconds
		}
		saved, colVersion, err = tx.putCircleSkillTx(ctx, hub.CircleSkill{
			HubID:       hubID,
			CircleID:    circleID,
			Slug:        slug,
			Title:       proposal.Title,
			Summary:     proposal.Summary,
			Content:     proposal.Content,
			ContentHash: proposal.ContentHash,
			TTLSeconds:  ttl,
			ProposedBy:  proposal.ProposedBy,
			ApprovedBy:  reviewer,
			UpdatedBy:   reviewer,
		}, decision.ExpectedVersion, now)
		if err != nil {
			return err
		}

		nowStr := formatTime(now.UTC())
		if _, err := tx.executor().ExecContext(ctx, `
UPDATE circle_skill_proposals
SET status = ?, reviewed_by = ?, reviewed_at = ?, updated_at = ?
WHERE hub_id = ? AND circle_id = ? AND slug = ?`,
			hub.SkillProposalStatusApproved, reviewer, nowStr, nowStr, hubID, circleID, slug,
		); err != nil {
			return err
		}
		return tx.saveSkillIdempotencyTx(ctx, hubID, circleID, "APPROVE_PROPOSAL", slug, decision.IdempotencyKey, reqHash, cachedSkillIdempotencyPayload{
			Skill:             saved,
			CollectionVersion: colVersion,
		}, now)
	})
	return saved, colVersion, duplicate, err
}

func (repository *Repository) RejectCircleSkillProposal(ctx context.Context, hubID, circleID, slug, reviewer string, decision hub.CircleSkillDecisionInput, now time.Time) (hub.CircleSkillProposal, int64, bool, error) {
	reqHash, err := computeRequestHash("REJECT_PROPOSAL", slug, decision.Reason)
	if err != nil {
		return hub.CircleSkillProposal{}, 0, false, err
	}
	var updated hub.CircleSkillProposal
	var colVersion int64
	var duplicate bool
	err = repository.withTransaction(ctx, func(tx *Repository) error {
		cached, err := tx.lookupSkillIdempotencyTx(ctx, hubID, circleID, "REJECT_PROPOSAL", slug, decision.IdempotencyKey, reqHash)
		if err != nil {
			return err
		}
		if cached != nil {
			updated = cached.Proposal
			colVersion = cached.CollectionVersion
			duplicate = true
			return nil
		}
		proposal, err := tx.GetCircleSkillProposal(ctx, hubID, circleID, slug)
		if err != nil {
			return err
		}
		if proposal.Status != hub.SkillProposalStatusPending {
			return store.ErrConflict
		}
		nowStr := formatTime(now.UTC())
		if _, err := tx.executor().ExecContext(ctx, `
UPDATE circle_skill_proposals
SET status = ?, rejection_reason = ?, reviewed_by = ?, reviewed_at = ?, updated_at = ?
WHERE hub_id = ? AND circle_id = ? AND slug = ?`,
			hub.SkillProposalStatusRejected, decision.Reason, reviewer, nowStr, nowStr, hubID, circleID, slug,
		); err != nil {
			return err
		}
		colVersion, err = tx.getCircleCollectionVersionTx(ctx, hubID, circleID)
		if err != nil {
			return err
		}
		updated = proposal
		updated.Status = hub.SkillProposalStatusRejected
		updated.RejectionReason = decision.Reason
		updated.ReviewedBy = reviewer
		updated.ReviewedAt = &nowStr
		updated.UpdatedAt = nowStr

		return tx.saveSkillIdempotencyTx(ctx, hubID, circleID, "REJECT_PROPOSAL", slug, decision.IdempotencyKey, reqHash, cachedSkillIdempotencyPayload{
			Proposal:          updated,
			CollectionVersion: colVersion,
		}, now)
	})
	return updated, colVersion, duplicate, err
}

func (repository *Repository) SweepExpiredCircleSkills(ctx context.Context, now time.Time) ([]hub.CircleSkillEvent, error) {
	nowUTC := now.UTC()
	nowUnix := nowUTC.UnixNano()
	nowStr := formatTime(nowUTC)

	type expiredRow struct {
		hubID    string
		circleID string
		slug     string
		version  int64
		hash     string
	}
	var events []hub.CircleSkillEvent

	err := repository.withTransaction(ctx, func(tx *Repository) error {
		rows, err := tx.executor().QueryContext(ctx, `
SELECT hub_id, circle_id, slug, version, content_hash
FROM circle_skills
WHERE deleted_at IS NULL AND expires_at_unix > 0 AND expires_at_unix <= ?`,
			nowUnix,
		)
		if err != nil {
			return err
		}
		var expired []expiredRow
		for rows.Next() {
			var r expiredRow
			if err := rows.Scan(&r.hubID, &r.circleID, &r.slug, &r.version, &r.hash); err != nil {
				_ = rows.Close()
				return err
			}
			expired = append(expired, r)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}

		for _, item := range expired {
			nextVersion := item.version + 1
			if _, err := tx.executor().ExecContext(ctx, `
UPDATE circle_skills
SET version = ?, content = '', byte_size = 0, deleted_at = ?, updated_by = 'system:ttl', updated_at = ?
WHERE hub_id = ? AND circle_id = ? AND slug = ?`,
				nextVersion, nowStr, nowStr, item.hubID, item.circleID, item.slug,
			); err != nil {
				return err
			}
			if _, err := tx.executor().ExecContext(ctx, `
INSERT INTO circle_skill_revisions (
    hub_id, circle_id, slug, version, content_hash, byte_size, content, action, expires_at, updated_by, created_at
) VALUES (?, ?, ?, ?, ?, 0, '', ?, NULL, 'system:ttl', ?)`,
				item.hubID, item.circleID, item.slug, nextVersion, item.hash, hub.SkillEventActionExpired, nowStr,
			); err != nil {
				return err
			}
			if err := tx.pruneSkillRevisionsTx(ctx, item.hubID, item.circleID, item.slug); err != nil {
				return err
			}
			colVersion, err := tx.bumpCircleCollectionVersionTx(ctx, item.hubID, item.circleID, nowUTC)
			if err != nil {
				return err
			}
			events = append(events, hub.CircleSkillEvent{
				Action:            hub.SkillEventActionExpired,
				HubID:             item.hubID,
				CircleID:          item.circleID,
				Slug:              item.slug,
				Version:           nextVersion,
				CollectionVersion: colVersion,
				ContentHash:       item.hash,
				UpdatedBy:         "system:ttl",
				UpdatedAt:         nowStr,
			})
		}
		return nil
	})
	return events, err
}
