package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/tbdavid2019/888a2a-lite/internal/hub"
	"github.com/tbdavid2019/888a2a-lite/internal/store"
)

func migrateCircleResources(ctx context.Context, database *sql.DB) error {
	var applied int
	if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version = 12").Scan(&applied); err != nil {
		return err
	}
	if applied > 0 {
		return nil
	}
	statements := []string{
		`CREATE TABLE circle_resource_collections (
    hub_id TEXT NOT NULL,
    circle_id TEXT NOT NULL,
    collection_version INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (hub_id, circle_id)
)`,
		`CREATE TABLE circle_resources (
    hub_id TEXT NOT NULL,
    circle_id TEXT NOT NULL,
    slug TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('doc', 'asset')),
    title TEXT NOT NULL,
    summary TEXT NOT NULL,
    content TEXT NOT NULL DEFAULT '',
    url TEXT NOT NULL DEFAULT '',
    media_type TEXT NOT NULL DEFAULT '',
    filename TEXT NOT NULL DEFAULT '',
    size_bytes INTEGER NOT NULL DEFAULT 0,
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
		`CREATE INDEX idx_circle_resources_active ON circle_resources (hub_id, circle_id, deleted_at, expires_at_unix)`,
		`CREATE INDEX idx_circle_resources_expiry ON circle_resources (expires_at_unix) WHERE deleted_at IS NULL AND expires_at_unix > 0`,
		`CREATE TABLE circle_resource_proposals (
    hub_id TEXT NOT NULL,
    circle_id TEXT NOT NULL,
    slug TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('doc', 'asset')),
    base_version INTEGER NOT NULL DEFAULT 0,
    title TEXT NOT NULL,
    summary TEXT NOT NULL,
    content TEXT NOT NULL DEFAULT '',
    url TEXT NOT NULL DEFAULT '',
    media_type TEXT NOT NULL DEFAULT '',
    filename TEXT NOT NULL DEFAULT '',
    size_bytes INTEGER NOT NULL DEFAULT 0,
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
		`CREATE INDEX idx_circle_resource_proposals_status ON circle_resource_proposals (hub_id, circle_id, status, updated_at DESC)`,
		`CREATE TABLE circle_resource_idempotency (
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
		`INSERT INTO schema_migrations (version, applied_at) VALUES (12, CURRENT_TIMESTAMP)`,
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("migrate circle resources schema: %w", err)
		}
	}
	return tx.Commit()
}

func migrateCircleResourcesV13(ctx context.Context, database *sql.DB) error {
	var applied int
	if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version = 13").Scan(&applied); err != nil {
		return err
	}
	if applied > 0 {
		return nil
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, `PRAGMA table_info(circle_resource_proposals)`)
	if err != nil {
		return err
	}
	hasRevision := false
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull int
		var dflt sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
			_ = rows.Close()
			return err
		}
		if name == "revision" {
			hasRevision = true
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if !hasRevision {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE circle_resource_proposals ADD COLUMN revision INTEGER NOT NULL DEFAULT 1`); err != nil {
			return fmt.Errorf("migrate circle resource proposals revision column: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version, applied_at) VALUES (13, CURRENT_TIMESTAMP)`); err != nil {
		return err
	}
	return tx.Commit()
}

type cachedResourceIdempotencyPayload struct {
	Resource          hub.CircleResource         `json:"resource,omitempty"`
	Proposal          hub.CircleResourceProposal `json:"proposal,omitempty"`
	Version           int64                      `json:"version,omitempty"`
	CollectionVersion int64                      `json:"collectionVersion"`
	IsUpdate          bool                       `json:"isUpdate,omitempty"`
}

func (repository *Repository) getCircleResourceCollectionVersionTx(ctx context.Context, hubID, circleID string) (int64, error) {
	var colVersion int64
	err := repository.executor().QueryRowContext(ctx,
		`SELECT collection_version FROM circle_resource_collections WHERE hub_id = ? AND circle_id = ?`,
		hubID, circleID,
	).Scan(&colVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return colVersion, err
}

func (repository *Repository) bumpCircleResourceCollectionVersionTx(ctx context.Context, hubID, circleID string, now time.Time) (int64, error) {
	nowStr := formatTime(now)
	if _, err := repository.executor().ExecContext(ctx, `
INSERT INTO circle_resource_collections (hub_id, circle_id, collection_version, updated_at)
VALUES (?, ?, 1, ?)
ON CONFLICT (hub_id, circle_id) DO UPDATE SET
    collection_version = circle_resource_collections.collection_version + 1,
    updated_at = excluded.updated_at`,
		hubID, circleID, nowStr,
	); err != nil {
		return 0, err
	}
	return repository.getCircleResourceCollectionVersionTx(ctx, hubID, circleID)
}

func (repository *Repository) lookupResourceIdempotencyTx(ctx context.Context, hubID, circleID, method, slug, idempotencyKey, requestHash string) (*cachedResourceIdempotencyPayload, error) {
	var storedHash, rawResp string
	err := repository.executor().QueryRowContext(ctx, `
SELECT request_hash, response_json
FROM circle_resource_idempotency
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
	var cached cachedResourceIdempotencyPayload
	if err := json.Unmarshal([]byte(rawResp), &cached); err != nil {
		return nil, err
	}
	return &cached, nil
}

func (repository *Repository) saveResourceIdempotencyTx(ctx context.Context, hubID, circleID, method, slug, idempotencyKey, requestHash string, payload cachedResourceIdempotencyPayload, now time.Time) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = repository.executor().ExecContext(ctx, `
INSERT INTO circle_resource_idempotency (hub_id, circle_id, method, slug, idempotency_key, request_hash, response_json, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		hubID, circleID, method, slug, idempotencyKey, requestHash, string(encoded), formatTime(now),
	)
	return err
}

func (repository *Repository) ListCircleResourceManifest(ctx context.Context, hubID, circleID string, now time.Time) (hub.CircleResourceManifest, error) {
	nowUnix := now.UTC().UnixNano()
	var colVersion int64
	items := make([]hub.CircleResourceManifestItem, 0)
	totalDocBytes := 0
	err := repository.withTransaction(ctx, func(tx *Repository) error {
		var err error
		colVersion, err = tx.getCircleResourceCollectionVersionTx(ctx, hubID, circleID)
		if err != nil {
			return err
		}
		rows, err := tx.executor().QueryContext(ctx, `
SELECT slug, kind, title, summary, url, media_type, filename, size_bytes, version, content_hash, byte_size, ttl_seconds, expires_at, updated_by, updated_at
FROM circle_resources
WHERE hub_id = ? AND circle_id = ?
  AND deleted_at IS NULL
  AND (expires_at_unix = 0 OR expires_at_unix > ?)
ORDER BY slug ASC`,
			hubID, circleID, nowUnix,
		)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			var item hub.CircleResourceManifestItem
			var ttlVal sql.NullInt64
			var expVal sql.NullString
			if err := rows.Scan(
				&item.Slug, &item.Kind, &item.Title, &item.Summary, &item.URL, &item.MediaType,
				&item.Filename, &item.SizeBytes, &item.Version, &item.ContentHash, &item.ByteSize,
				&ttlVal, &expVal, &item.UpdatedBy, &item.UpdatedAt,
			); err != nil {
				return err
			}
			if ttlVal.Valid {
				v := ttlVal.Int64
				item.TTLSeconds = &v
			}
			if expVal.Valid && expVal.String != "" {
				s := expVal.String
				item.ExpiresAt = &s
			}
			if item.Kind == hub.ResourceKindDoc {
				totalDocBytes += item.ByteSize
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	if err != nil {
		return hub.CircleResourceManifest{}, err
	}
	return hub.CircleResourceManifest{
		HubID:             hubID,
		CircleID:          circleID,
		CollectionVersion: colVersion,
		ETag:              hub.CircleResourceManifestETagWithItems(circleID, colVersion, items),
		ResourceCount:     len(items),
		TotalDocBytes:     totalDocBytes,
		Resources:         items,
	}, nil
}

func (repository *Repository) GetCircleResource(ctx context.Context, hubID, circleID, slug string, now time.Time) (hub.CircleResource, error) {
	nowUnix := now.UTC().UnixNano()
	var res hub.CircleResource
	var ttlVal sql.NullInt64
	var expVal sql.NullString
	err := repository.executor().QueryRowContext(ctx, `
SELECT hub_id, circle_id, slug, kind, title, summary, content, url, media_type, filename, size_bytes, version, content_hash, byte_size, ttl_seconds, expires_at, proposed_by, approved_by, updated_by, created_at, updated_at
FROM circle_resources
WHERE hub_id = ? AND circle_id = ? AND slug = ?
  AND deleted_at IS NULL
  AND (expires_at_unix = 0 OR expires_at_unix > ?)`,
		hubID, circleID, slug, nowUnix,
	).Scan(
		&res.HubID, &res.CircleID, &res.Slug, &res.Kind, &res.Title, &res.Summary,
		&res.Content, &res.URL, &res.MediaType, &res.Filename, &res.SizeBytes,
		&res.Version, &res.ContentHash, &res.ByteSize, &ttlVal, &expVal,
		&res.ProposedBy, &res.ApprovedBy, &res.UpdatedBy, &res.CreatedAt, &res.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return hub.CircleResource{}, store.ErrNotFound
	}
	if err != nil {
		return hub.CircleResource{}, err
	}
	if ttlVal.Valid {
		v := ttlVal.Int64
		res.TTLSeconds = &v
	}
	if expVal.Valid && expVal.String != "" {
		s := expVal.String
		res.ExpiresAt = &s
	}
	return res, nil
}

func (repository *Repository) putCircleResourceTx(ctx context.Context, resource hub.CircleResource, expectedVersion int64, now time.Time) (hub.CircleResource, int64, error) {
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
FROM circle_resources
WHERE hub_id = ? AND circle_id = ? AND slug = ?`,
		resource.HubID, resource.CircleID, resource.Slug,
	).Scan(&currentVersion, &deletedAt, &expUnix, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		rowExists = false
		currentVersion = 0
	} else if err != nil {
		return hub.CircleResource{}, 0, err
	}

	isActive := rowExists && !deletedAt.Valid && (expUnix == 0 || expUnix > nowUnix)
	if isActive {
		if expectedVersion != currentVersion {
			return hub.CircleResource{}, 0, store.ErrConflict
		}
	} else if expectedVersion != 0 && expectedVersion != currentVersion {
		return hub.CircleResource{}, 0, store.ErrConflict
	}

	var activeCount int
	var activeDocBytes int
	if err := repository.executor().QueryRowContext(ctx, `
SELECT COUNT(*), COALESCE(SUM(CASE WHEN kind = 'doc' THEN byte_size ELSE 0 END), 0)
FROM circle_resources
WHERE hub_id = ? AND circle_id = ? AND slug <> ?
  AND deleted_at IS NULL
  AND (expires_at_unix = 0 OR expires_at_unix > ?)`,
		resource.HubID, resource.CircleID, resource.Slug, nowUnix,
	).Scan(&activeCount, &activeDocBytes); err != nil {
		return hub.CircleResource{}, 0, err
	}
	if activeCount >= hub.MaxCircleResourcesPerCircle {
		return hub.CircleResource{}, 0, store.ErrLimitReached
	}
	docBytes := 0
	if resource.Kind == hub.ResourceKindDoc {
		docBytes = len(resource.Content)
	}
	if activeDocBytes+docBytes > hub.MaxCircleResourceDocsTotalBytes {
		return hub.CircleResource{}, 0, store.ErrLimitReached
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
	if resource.TTLSeconds != nil && *resource.TTLSeconds > 0 {
		v := *resource.TTLSeconds
		expTime := nowUTC.Add(time.Duration(v) * time.Second)
		expStr := formatTime(expTime)
		ttlArg = sql.NullInt64{Int64: v, Valid: true}
		expArg = sql.NullString{String: expStr, Valid: true}
		expUnixArg = expTime.UnixNano()
		ttlCopy = &v
		expCopy = &expStr
	}

	if _, err := repository.executor().ExecContext(ctx, `
INSERT INTO circle_resources (
    hub_id, circle_id, slug, kind, title, summary, content, url, media_type, filename, size_bytes,
    version, content_hash, byte_size, ttl_seconds, expires_at, expires_at_unix,
    proposed_by, approved_by, updated_by, created_at, updated_at, deleted_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL)
ON CONFLICT (hub_id, circle_id, slug) DO UPDATE SET
    kind = excluded.kind,
    title = excluded.title,
    summary = excluded.summary,
    content = excluded.content,
    url = excluded.url,
    media_type = excluded.media_type,
    filename = excluded.filename,
    size_bytes = excluded.size_bytes,
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
		resource.HubID, resource.CircleID, resource.Slug, resource.Kind, resource.Title, resource.Summary,
		resource.Content, resource.URL, resource.MediaType, resource.Filename, resource.SizeBytes,
		nextVersion, resource.ContentHash, docBytes, ttlArg, expArg, expUnixArg,
		resource.ProposedBy, resource.ApprovedBy, resource.UpdatedBy, createdAt, nowStr,
	); err != nil {
		return hub.CircleResource{}, 0, err
	}

	colVersion, err := repository.bumpCircleResourceCollectionVersionTx(ctx, resource.HubID, resource.CircleID, nowUTC)
	if err != nil {
		return hub.CircleResource{}, 0, err
	}

	saved := resource
	saved.Version = nextVersion
	saved.ByteSize = docBytes
	saved.TTLSeconds = ttlCopy
	saved.ExpiresAt = expCopy
	saved.CreatedAt = createdAt
	saved.UpdatedAt = nowStr
	return saved, colVersion, nil
}

func (repository *Repository) PutCircleResource(ctx context.Context, resource hub.CircleResource, expectedVersion int64, idempotencyKey string, now time.Time) (hub.CircleResource, int64, bool, error) {
	reqHash, err := computeRequestHash(
		"PUT_RESOURCE", resource.Slug, resource.Kind, resource.Title, resource.Summary,
		resource.URL, resource.MediaType, resource.Filename, resource.SizeBytes,
		resource.ContentHash, resource.TTLSeconds, expectedVersion,
	)
	if err != nil {
		return hub.CircleResource{}, 0, false, err
	}
	var saved hub.CircleResource
	var colVersion int64
	var duplicate bool
	err = repository.withTransaction(ctx, func(tx *Repository) error {
		cached, err := tx.lookupResourceIdempotencyTx(ctx, resource.HubID, resource.CircleID, "PUT_RESOURCE", resource.Slug, idempotencyKey, reqHash)
		if err != nil {
			return err
		}
		if cached != nil {
			saved = cached.Resource
			colVersion = cached.CollectionVersion
			duplicate = true
			return nil
		}
		saved, colVersion, err = tx.putCircleResourceTx(ctx, resource, expectedVersion, now)
		if err != nil {
			return err
		}
		return tx.saveResourceIdempotencyTx(ctx, resource.HubID, resource.CircleID, "PUT_RESOURCE", resource.Slug, idempotencyKey, reqHash, cachedResourceIdempotencyPayload{
			Resource:          saved,
			CollectionVersion: colVersion,
		}, now)
	})
	return saved, colVersion, duplicate, err
}

func (repository *Repository) DeleteCircleResource(ctx context.Context, hubID, circleID, slug, actor string, expectedVersion int64, idempotencyKey string, now time.Time) (int64, int64, bool, error) {
	reqHash, err := computeRequestHash("DELETE_RESOURCE", slug, expectedVersion)
	if err != nil {
		return 0, 0, false, err
	}
	var deletedVersion, colVersion int64
	var duplicate bool
	err = repository.withTransaction(ctx, func(tx *Repository) error {
		cached, err := tx.lookupResourceIdempotencyTx(ctx, hubID, circleID, "DELETE_RESOURCE", slug, idempotencyKey, reqHash)
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
		qErr := tx.executor().QueryRowContext(ctx, `
SELECT version, deleted_at, expires_at_unix
FROM circle_resources
WHERE hub_id = ? AND circle_id = ? AND slug = ?`,
			hubID, circleID, slug,
		).Scan(&currentVersion, &deletedAt, &expUnix)
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
UPDATE circle_resources
SET version = ?, content = '', url = '', byte_size = 0, deleted_at = ?, updated_by = ?, updated_at = ?
WHERE hub_id = ? AND circle_id = ? AND slug = ?`,
			nextVersion, nowStr, actor, nowStr, hubID, circleID, slug,
		); err != nil {
			return err
		}
		colVersion, err = tx.bumpCircleResourceCollectionVersionTx(ctx, hubID, circleID, nowUTC)
		if err != nil {
			return err
		}
		deletedVersion = nextVersion
		return tx.saveResourceIdempotencyTx(ctx, hubID, circleID, "DELETE_RESOURCE", slug, idempotencyKey, reqHash, cachedResourceIdempotencyPayload{
			Version:           deletedVersion,
			CollectionVersion: colVersion,
		}, nowUTC)
	})
	return deletedVersion, colVersion, duplicate, err
}

func (repository *Repository) SubmitCircleResourceProposal(ctx context.Context, proposal hub.CircleResourceProposal, idempotencyKey string, now time.Time) (hub.CircleResourceProposal, int64, bool, bool, error) {
	reqHash, err := computeRequestHash(
		"SUBMIT_RESOURCE_PROPOSAL", proposal.Slug, proposal.Kind, proposal.BaseVersion,
		proposal.Title, proposal.Summary, proposal.URL, proposal.MediaType,
		proposal.Filename, proposal.SizeBytes, proposal.ContentHash, proposal.RequestedTTLSeconds,
	)
	if err != nil {
		return hub.CircleResourceProposal{}, 0, false, false, err
	}
	var saved hub.CircleResourceProposal
	var colVersion int64
	var isUpdate, duplicate bool
	err = repository.withTransaction(ctx, func(tx *Repository) error {
		cached, err := tx.lookupResourceIdempotencyTx(ctx, proposal.HubID, proposal.CircleID, "SUBMIT_RESOURCE_PROPOSAL", proposal.Slug, idempotencyKey, reqHash)
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
		var existingRevision int64
		qErr := tx.executor().QueryRowContext(ctx, `
SELECT status, created_at, revision FROM circle_resource_proposals
WHERE hub_id = ? AND circle_id = ? AND slug = ?`,
			proposal.HubID, proposal.CircleID, proposal.Slug,
		).Scan(&existingStatus, &existingCreatedAt, &existingRevision)
		if qErr != nil && !errors.Is(qErr, sql.ErrNoRows) {
			return qErr
		}
		if qErr == nil && existingStatus == hub.ResourceProposalStatusPending {
			isUpdate = true
		} else {
			var pendingCount int
			if err := tx.executor().QueryRowContext(ctx, `
SELECT COUNT(*) FROM circle_resource_proposals
WHERE hub_id = ? AND circle_id = ? AND status = ?`,
				proposal.HubID, proposal.CircleID, hub.ResourceProposalStatusPending,
			).Scan(&pendingCount); err != nil {
				return err
			}
			if pendingCount >= hub.MaxCircleResourceProposalsPerCirc {
				return store.ErrLimitReached
			}
		}

		nextRevision := existingRevision + 1
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
		docBytes := 0
		if proposal.Kind == hub.ResourceKindDoc {
			docBytes = len(proposal.Content)
		}

		if _, err := tx.executor().ExecContext(ctx, `
INSERT INTO circle_resource_proposals (
    hub_id, circle_id, slug, kind, revision, base_version, title, summary, content, url, media_type, filename, size_bytes,
    content_hash, byte_size, requested_ttl_seconds, proposed_by, status, rejection_reason,
    created_at, updated_at, reviewed_by, reviewed_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', ?, ?, '', NULL)
ON CONFLICT (hub_id, circle_id, slug) DO UPDATE SET
    kind = excluded.kind,
    revision = excluded.revision,
    base_version = excluded.base_version,
    title = excluded.title,
    summary = excluded.summary,
    content = excluded.content,
    url = excluded.url,
    media_type = excluded.media_type,
    filename = excluded.filename,
    size_bytes = excluded.size_bytes,
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
			proposal.HubID, proposal.CircleID, proposal.Slug, proposal.Kind, nextRevision, proposal.BaseVersion,
			proposal.Title, proposal.Summary, proposal.Content, proposal.URL, proposal.MediaType,
			proposal.Filename, proposal.SizeBytes, proposal.ContentHash, docBytes,
			reqTTL, proposal.ProposedBy, hub.ResourceProposalStatusPending, createdAt, nowStr,
		); err != nil {
			return err
		}

		colVersion, err = tx.getCircleResourceCollectionVersionTx(ctx, proposal.HubID, proposal.CircleID)
		if err != nil {
			return err
		}
		saved = proposal
		saved.Revision = nextRevision
		saved.ByteSize = docBytes
		saved.RequestedTTLSeconds = reqTTLCopy
		saved.Status = hub.ResourceProposalStatusPending
		saved.ReviewReason = ""
		saved.CreatedAt = createdAt
		saved.UpdatedAt = nowStr
		saved.ReviewedBy = ""
		saved.ReviewedAt = ""

		return tx.saveResourceIdempotencyTx(ctx, proposal.HubID, proposal.CircleID, "SUBMIT_RESOURCE_PROPOSAL", proposal.Slug, idempotencyKey, reqHash, cachedResourceIdempotencyPayload{
			Proposal:          saved,
			CollectionVersion: colVersion,
			IsUpdate:          isUpdate,
		}, nowUTC)
	})
	return saved, colVersion, isUpdate, duplicate, err
}

func (repository *Repository) ListCircleResourceProposals(ctx context.Context, hubID, circleID string) ([]hub.CircleResourceProposal, error) {
	rows, err := repository.executor().QueryContext(ctx, `
SELECT hub_id, circle_id, slug, kind, revision, base_version, title, summary, content, url, media_type, filename, size_bytes, content_hash, byte_size, requested_ttl_seconds, proposed_by, status, rejection_reason, created_at, updated_at, reviewed_by, reviewed_at
FROM circle_resource_proposals
WHERE hub_id = ? AND circle_id = ?
ORDER BY CASE WHEN status = 'PENDING_REVIEW' THEN 0 ELSE 1 END, updated_at DESC, slug ASC`,
		hubID, circleID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	proposals := make([]hub.CircleResourceProposal, 0)
	for rows.Next() {
		p, err := scanCircleResourceProposal(rows)
		if err != nil {
			return nil, err
		}
		proposals = append(proposals, p)
	}
	return proposals, rows.Err()
}

func (repository *Repository) GetCircleResourceProposal(ctx context.Context, hubID, circleID, slug string) (hub.CircleResourceProposal, error) {
	row := repository.executor().QueryRowContext(ctx, `
SELECT hub_id, circle_id, slug, kind, revision, base_version, title, summary, content, url, media_type, filename, size_bytes, content_hash, byte_size, requested_ttl_seconds, proposed_by, status, rejection_reason, created_at, updated_at, reviewed_by, reviewed_at
FROM circle_resource_proposals
WHERE hub_id = ? AND circle_id = ? AND slug = ?`,
		hubID, circleID, slug,
	)
	p, err := scanCircleResourceProposal(row)
	if errors.Is(err, sql.ErrNoRows) {
		return hub.CircleResourceProposal{}, store.ErrNotFound
	}
	return p, err
}

func scanCircleResourceProposal(scanner interface{ Scan(...any) error }) (hub.CircleResourceProposal, error) {
	var p hub.CircleResourceProposal
	var reqTTL sql.NullInt64
	var reviewedAt sql.NullString
	if err := scanner.Scan(
		&p.HubID, &p.CircleID, &p.Slug, &p.Kind, &p.Revision, &p.BaseVersion, &p.Title, &p.Summary,
		&p.Content, &p.URL, &p.MediaType, &p.Filename, &p.SizeBytes,
		&p.ContentHash, &p.ByteSize, &reqTTL, &p.ProposedBy,
		&p.Status, &p.ReviewReason, &p.CreatedAt, &p.UpdatedAt, &p.ReviewedBy, &reviewedAt,
	); err != nil {
		return hub.CircleResourceProposal{}, err
	}
	if reqTTL.Valid {
		v := reqTTL.Int64
		p.RequestedTTLSeconds = &v
	}
	if reviewedAt.Valid {
		p.ReviewedAt = reviewedAt.String
	}
	return p, nil
}

func (repository *Repository) ApproveCircleResourceProposal(ctx context.Context, hubID, circleID, slug, reviewer string, decision hub.CircleResourceDecisionInput, now time.Time) (hub.CircleResource, int64, bool, error) {
	reqHash, err := computeRequestHash("APPROVE_RESOURCE_PROPOSAL", slug, decision.ExpectedVersion, decision.ExpectedRevision, decision.TTLSeconds)
	if err != nil {
		return hub.CircleResource{}, 0, false, err
	}
	var saved hub.CircleResource
	var colVersion int64
	var duplicate bool
	err = repository.withTransaction(ctx, func(tx *Repository) error {
		cached, err := tx.lookupResourceIdempotencyTx(ctx, hubID, circleID, "APPROVE_RESOURCE_PROPOSAL", slug, decision.IdempotencyKey, reqHash)
		if err != nil {
			return err
		}
		if cached != nil {
			saved = cached.Resource
			colVersion = cached.CollectionVersion
			duplicate = true
			return nil
		}
		proposal, err := tx.GetCircleResourceProposal(ctx, hubID, circleID, slug)
		if err != nil {
			return err
		}
		if proposal.Status != hub.ResourceProposalStatusPending {
			return store.ErrConflict
		}
		if decision.ExpectedRevision > 0 && decision.ExpectedRevision != proposal.Revision {
			return store.ErrConflict
		}
		ttl := proposal.RequestedTTLSeconds
		if decision.TTLSeconds != nil {
			ttl = decision.TTLSeconds
		}
		saved, colVersion, err = tx.putCircleResourceTx(ctx, hub.CircleResource{
			HubID:       hubID,
			CircleID:    circleID,
			Slug:        slug,
			Kind:        proposal.Kind,
			Title:       proposal.Title,
			Summary:     proposal.Summary,
			Content:     proposal.Content,
			URL:         proposal.URL,
			MediaType:   proposal.MediaType,
			Filename:    proposal.Filename,
			SizeBytes:   proposal.SizeBytes,
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
UPDATE circle_resource_proposals
SET status = ?, reviewed_by = ?, reviewed_at = ?, updated_at = ?
WHERE hub_id = ? AND circle_id = ? AND slug = ?`,
			hub.ResourceProposalStatusApproved, reviewer, nowStr, nowStr, hubID, circleID, slug,
		); err != nil {
			return err
		}
		return tx.saveResourceIdempotencyTx(ctx, hubID, circleID, "APPROVE_RESOURCE_PROPOSAL", slug, decision.IdempotencyKey, reqHash, cachedResourceIdempotencyPayload{
			Resource:          saved,
			CollectionVersion: colVersion,
		}, now)
	})
	return saved, colVersion, duplicate, err
}

func (repository *Repository) RejectCircleResourceProposal(ctx context.Context, hubID, circleID, slug, reviewer string, decision hub.CircleResourceDecisionInput, now time.Time) (hub.CircleResourceProposal, int64, bool, error) {
	reqHash, err := computeRequestHash("REJECT_RESOURCE_PROPOSAL", slug, decision.ExpectedVersion, decision.ExpectedRevision, decision.Reason)
	if err != nil {
		return hub.CircleResourceProposal{}, 0, false, err
	}
	var updated hub.CircleResourceProposal
	var colVersion int64
	var duplicate bool
	err = repository.withTransaction(ctx, func(tx *Repository) error {
		cached, err := tx.lookupResourceIdempotencyTx(ctx, hubID, circleID, "REJECT_RESOURCE_PROPOSAL", slug, decision.IdempotencyKey, reqHash)
		if err != nil {
			return err
		}
		if cached != nil {
			updated = cached.Proposal
			colVersion = cached.CollectionVersion
			duplicate = true
			return nil
		}
		proposal, err := tx.GetCircleResourceProposal(ctx, hubID, circleID, slug)
		if err != nil {
			return err
		}
		if proposal.Status != hub.ResourceProposalStatusPending {
			return store.ErrConflict
		}
		if decision.ExpectedRevision > 0 && decision.ExpectedRevision != proposal.Revision {
			return store.ErrConflict
		}
		nowStr := formatTime(now.UTC())
		if _, err := tx.executor().ExecContext(ctx, `
UPDATE circle_resource_proposals
SET status = ?, rejection_reason = ?, reviewed_by = ?, reviewed_at = ?, updated_at = ?
WHERE hub_id = ? AND circle_id = ? AND slug = ?`,
			hub.ResourceProposalStatusRejected, decision.Reason, reviewer, nowStr, nowStr, hubID, circleID, slug,
		); err != nil {
			return err
		}
		colVersion, err = tx.getCircleResourceCollectionVersionTx(ctx, hubID, circleID)
		if err != nil {
			return err
		}
		updated = proposal
		updated.Status = hub.ResourceProposalStatusRejected
		updated.ReviewReason = decision.Reason
		updated.ReviewedBy = reviewer
		updated.ReviewedAt = nowStr
		updated.UpdatedAt = nowStr

		return tx.saveResourceIdempotencyTx(ctx, hubID, circleID, "REJECT_RESOURCE_PROPOSAL", slug, decision.IdempotencyKey, reqHash, cachedResourceIdempotencyPayload{
			Proposal:          updated,
			CollectionVersion: colVersion,
		}, now)
	})
	return updated, colVersion, duplicate, err
}

func (repository *Repository) SweepExpiredCircleResources(ctx context.Context, now time.Time) ([]hub.CircleResourceEvent, error) {
	nowUTC := now.UTC()
	nowUnix := nowUTC.UnixNano()
	nowStr := formatTime(nowUTC)

	type expiredRow struct {
		hubID    string
		circleID string
		slug     string
		kind     string
		version  int64
		hash     string
	}
	var events []hub.CircleResourceEvent

	err := repository.withTransaction(ctx, func(tx *Repository) error {
		rows, err := tx.executor().QueryContext(ctx, `
SELECT hub_id, circle_id, slug, kind, version, content_hash
FROM circle_resources
WHERE deleted_at IS NULL AND expires_at_unix > 0 AND expires_at_unix <= ?`,
			nowUnix,
		)
		if err != nil {
			return err
		}
		var expired []expiredRow
		for rows.Next() {
			var r expiredRow
			if err := rows.Scan(&r.hubID, &r.circleID, &r.slug, &r.kind, &r.version, &r.hash); err != nil {
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
UPDATE circle_resources
SET version = ?, content = '', url = '', byte_size = 0, deleted_at = ?, updated_by = 'system:ttl', updated_at = ?
WHERE hub_id = ? AND circle_id = ? AND slug = ?`,
				nextVersion, nowStr, nowStr, item.hubID, item.circleID, item.slug,
			); err != nil {
				return err
			}
			colVersion, err := tx.bumpCircleResourceCollectionVersionTx(ctx, item.hubID, item.circleID, nowUTC)
			if err != nil {
				return err
			}
			events = append(events, hub.CircleResourceEvent{
				Action:            hub.ResourceEventActionExpired,
				HubID:             item.hubID,
				CircleID:          item.circleID,
				Slug:              item.slug,
				Kind:              item.kind,
				Version:           nextVersion,
				CollectionVersion: colVersion,
				ContentHash:       item.hash,
				UpdatedBy:         "system:ttl",
				UpdatedAt:         nowStr,
			})
		}
		idempotencyCutoff := formatTime(nowUTC.Add(-7 * 24 * time.Hour))
		if _, err := tx.executor().ExecContext(ctx,
			`DELETE FROM circle_resource_idempotency WHERE created_at < ?`,
			idempotencyCutoff,
		); err != nil {
			return err
		}
		return nil
	})
	return events, err
}
