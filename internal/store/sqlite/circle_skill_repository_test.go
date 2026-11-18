package sqlite

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tbdavid2019/888a2a-lite/internal/hub"
	"github.com/tbdavid2019/888a2a-lite/internal/store"
)

func TestCircleSkillRepositoryLifecycleProposalsExpiryAndPrune(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "hub.db")
	database, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	repo := NewRepository(database)
	now := time.Now().UTC()

	// Create two dynamic circles: one empty expired target, one key-rotated protected circle.
	for _, cid := range []string{"circle-empty", "circle-protected"} {
		if err := repo.Circles().CreateCircle(ctx, hub.Circle{
			HubID:     "hub",
			CircleID:  cid,
			State:     hub.CircleStateActive,
			CreatedAt: now.Add(-48 * time.Hour),
		}); err != nil {
			t.Fatalf("CreateCircle(%s): %v", cid, err)
		}
	}
	if err := repo.Circles().CreateCircleKey(ctx, hub.CircleKey{
		HubID: "hub", CircleID: "circle-protected", Version: 2, KeyDigest: "digest-2", State: "ACTIVE", CreatedAt: now.Add(-48 * time.Hour),
	}); err != nil {
		t.Fatalf("CreateCircleKey: %v", err)
	}

	// 1. Bot submits a draft proposal; active manifest must remain empty.
	ttl60 := int64(60)
	proposal, _, isUpdate, dup, err := repo.CircleSkills().SubmitCircleSkillProposal(ctx, hub.CircleSkillProposal{
		HubID:               "hub",
		CircleID:            "circle-empty",
		Slug:                "ops-check",
		Title:               "Ops Check",
		Summary:             "Checklist",
		Content:             "# Ops Check\nRun safely.",
		ContentHash:         hub.CircleSkillContentHash("# Ops Check\nRun safely."),
		RequestedTTLSeconds: &ttl60,
		ProposedBy:          "bot-hermes",
	}, "prop-1", now.Add(-30*time.Hour))
	if err != nil || isUpdate || dup || proposal.Status != hub.SkillProposalStatusPending {
		t.Fatalf("SubmitCircleSkillProposal failed: %+v isUpdate=%v dup=%v err=%v", proposal, isUpdate, dup, err)
	}
	manifest, err := repo.CircleSkills().ListCircleSkillManifest(ctx, "hub", "circle-empty", now)
	if err != nil || manifest.SkillCount != 0 || manifest.CollectionVersion != 0 {
		t.Fatalf("pending proposal leaked into active manifest: %+v err=%v", manifest, err)
	}

	// 2. Human approves proposal with CAS & idempotency.
	approved, col1, dup, err := repo.CircleSkills().ApproveCircleSkillProposal(ctx, "hub", "circle-empty", "ops-check", "human-ui", hub.CircleSkillDecisionInput{
		ExpectedVersion: 0,
		IdempotencyKey:  "app-1",
	}, now)
	if err != nil || dup || approved.Version != 1 || col1 != 1 {
		t.Fatalf("ApproveCircleSkillProposal failed: %+v col=%d dup=%v err=%v", approved, col1, dup, err)
	}
	// Duplicate idempotent retry returns same outcome.
	approvedDup, colDup, dupFlag, err := repo.CircleSkills().ApproveCircleSkillProposal(ctx, "hub", "circle-empty", "ops-check", "human-ui", hub.CircleSkillDecisionInput{
		ExpectedVersion: 0,
		IdempotencyKey:  "app-1",
	}, now)
	if err != nil || !dupFlag || colDup != col1 || approvedDup.Version != approved.Version {
		t.Fatalf("idempotent approval replay mismatch: %+v col=%d dup=%v err=%v", approvedDup, colDup, dupFlag, err)
	}

	// 3. Lower-version skill update & delete advance collectionVersion monotonically.
	_, col2, _, err := repo.CircleSkills().PutCircleSkill(ctx, hub.CircleSkill{
		HubID: "hub", CircleID: "circle-empty", Slug: "second-skill", Title: "Second", Summary: "S2",
		Content: "# Second\nBody", ContentHash: hub.CircleSkillContentHash("# Second\nBody"), UpdatedBy: "human-ui",
	}, 0, "put-2", now)
	if err != nil || col2 != 2 {
		t.Fatalf("second skill put failed: col=%d err=%v", col2, err)
	}
	// Conflict check on wrong expectedVersion.
	if _, _, _, err := repo.CircleSkills().PutCircleSkill(ctx, hub.CircleSkill{
		HubID: "hub", CircleID: "circle-empty", Slug: "second-skill", Title: "Second", Summary: "S2",
		Content: "# Second\nEdited", ContentHash: hub.CircleSkillContentHash("# Second\nEdited"), UpdatedBy: "human-ui",
	}, 0, "put-conflict", now); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
	_, col3, _, err := repo.CircleSkills().DeleteCircleSkill(ctx, "hub", "circle-empty", "second-skill", "human-ui", 1, "del-2", now)
	if err != nil || col3 != 3 {
		t.Fatalf("DeleteCircleSkill failed: col=%d err=%v", col3, err)
	}

	// 4. TTL expiration hides skill on read and sweeps cleanly.
	expiredEvents, err := repo.CircleSkills().SweepExpiredCircleSkills(ctx, now.Add(61*time.Second))
	if err != nil || len(expiredEvents) != 1 || expiredEvents[0].Slug != "ops-check" || expiredEvents[0].Action != hub.SkillEventActionExpired {
		t.Fatalf("SweepExpiredCircleSkills failed: %+v err=%v", expiredEvents, err)
	}
	if _, err := repo.CircleSkills().GetCircleSkill(ctx, "hub", "circle-empty", "ops-check", now.Add(61*time.Second)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected expired skill ErrNotFound, got %v", err)
	}

	// 5. Protected circle skill stays across PruneEmptyCircles while dynamic empty circle + skill audit event is pruned.
	if _, _, _, err := repo.CircleSkills().PutCircleSkill(ctx, hub.CircleSkill{
		HubID: "hub", CircleID: "circle-protected", Slug: "keep-me", Title: "Keep", Summary: "K",
		Content: "# Keep", ContentHash: hub.CircleSkillContentHash("# Keep"), UpdatedBy: "op",
	}, 0, "put-prot", now.Add(-30*time.Hour)); err != nil {
		t.Fatalf("put protected skill: %v", err)
	}
	if err := repo.Events().AppendEvent(ctx, hub.Event{
		HubID: "hub", CircleID: "circle-empty", Type: hub.EventCircleSkillExpired, CreatedAt: now,
	}); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	pruned, err := repo.Circles().PruneEmptyCircles(ctx, now.Add(-24*time.Hour), nil)
	if err != nil || pruned != 1 {
		t.Fatalf("PruneEmptyCircles = %d, err=%v, want 1", pruned, err)
	}
	if got, err := repo.CircleSkills().ListCircleSkillManifest(ctx, "hub", "circle-protected", now); err != nil || got.SkillCount != 1 {
		t.Fatalf("protected circle skill unexpectedly missing: %+v err=%v", got, err)
	}

	// 6. Restart persistence check & revision cap check.
	if err := database.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}
	reopened, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("reopen db: %v", err)
	}
	defer func() { _ = reopened.Close() }()
	repo2 := NewRepository(reopened)
	m, err := repo2.CircleSkills().ListCircleSkillManifest(ctx, "hub", "circle-protected", now)
	if err != nil || m.SkillCount != 1 {
		t.Fatalf("reopened manifest mismatch: %+v err=%v", m, err)
	}

	// Push 25 revisions to verify bounded retention (20).
	for i := 2; i <= 25; i++ {
		content := fmt.Sprintf("# Keep\nRev %d", i)
		if _, _, _, err := repo2.CircleSkills().PutCircleSkill(ctx, hub.CircleSkill{
			HubID: "hub", CircleID: "circle-protected", Slug: "keep-me", Title: "Keep", Summary: "K",
			Content: content, ContentHash: hub.CircleSkillContentHash(content), UpdatedBy: "op",
		}, int64(i-1), fmt.Sprintf("rev-%d", i), now); err != nil {
			t.Fatalf("PutCircleSkill rev %d: %v", i, err)
		}
	}
	revs, err := repo2.CircleSkills().ListCircleSkillRevisions(ctx, "hub", "circle-protected", "keep-me")
	if err != nil || len(revs) != hub.MaxCircleSkillRevisionsPerSlug {
		t.Fatalf("revision count = %d, want %d (err=%v)", len(revs), hub.MaxCircleSkillRevisionsPerSlug, err)
	}

	// Verify active skill count quota (32).
	for i := 2; i <= hub.MaxCircleSkillsPerCircle; i++ {
		slug := fmt.Sprintf("s-%02d", i)
		c := "# " + slug
		if _, _, _, err := repo2.CircleSkills().PutCircleSkill(ctx, hub.CircleSkill{
			HubID: "hub", CircleID: "circle-protected", Slug: slug, Title: slug, Summary: "S",
			Content: c, ContentHash: hub.CircleSkillContentHash(c), UpdatedBy: "op",
		}, 0, "quota-"+slug, now); err != nil {
			t.Fatalf("quota put %s: %v", slug, err)
		}
	}
	if _, _, _, err := repo2.CircleSkills().PutCircleSkill(ctx, hub.CircleSkill{
		HubID: "hub", CircleID: "circle-protected", Slug: "overflow-33", Title: "Overflow", Summary: "O",
		Content: strings.Repeat("x", 16), ContentHash: hub.CircleSkillContentHash(strings.Repeat("x", 16)), UpdatedBy: "op",
	}, 0, "quota-overflow", now); !errors.Is(err, store.ErrLimitReached) {
		t.Fatalf("expected ErrLimitReached on 33rd skill, got %v", err)
	}
}
