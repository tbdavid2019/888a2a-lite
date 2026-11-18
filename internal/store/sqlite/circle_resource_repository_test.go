package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/tbdavid2019/888a2a-lite/internal/hub"
	"github.com/tbdavid2019/888a2a-lite/internal/store"
)

func TestCircleResourceRepositoryLifecycleProposalsExpiryAndPrune(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "hub.db")
	database, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer func() { _ = database.Close() }()
	repo := NewRepository(database)
	now := time.Now().UTC()

	for _, cid := range []string{"circle-res-empty", "circle-res-protected"} {
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
		HubID: "hub", CircleID: "circle-res-protected", Version: 2, KeyDigest: "digest-2", State: "ACTIVE", CreatedAt: now.Add(-48 * time.Hour),
	}); err != nil {
		t.Fatalf("CreateCircleKey: %v", err)
	}

	// 1. Direct put doc + asset with CAS & idempotency
	docContent := "# Architecture\nKeep it minimal."
	docRes, colV1, dup, err := repo.CircleResources().PutCircleResource(ctx, hub.CircleResource{
		HubID:       "hub",
		CircleID:    "circle-res-empty",
		Slug:        "arch-notes",
		Kind:        hub.ResourceKindDoc,
		Title:       "Architecture",
		Summary:     "Keep it minimal.",
		Content:     docContent,
		ContentHash: hub.CircleSkillContentHash(docContent),
		UpdatedBy:   "human-admin",
	}, 0, "put-doc-1", now.Add(-30*time.Hour))
	if err != nil || dup || colV1 != 1 || docRes.Version != 1 || docRes.ByteSize != len(docContent) {
		t.Fatalf("PutCircleResource doc failed: res=%+v colV=%d dup=%v err=%v", docRes, colV1, dup, err)
	}

	// Idempotent replay
	docReplay, colVReplay, dupReplay, err := repo.CircleResources().PutCircleResource(ctx, hub.CircleResource{
		HubID:       "hub",
		CircleID:    "circle-res-empty",
		Slug:        "arch-notes",
		Kind:        hub.ResourceKindDoc,
		Title:       "Architecture",
		Summary:     "Keep it minimal.",
		Content:     docContent,
		ContentHash: hub.CircleSkillContentHash(docContent),
		UpdatedBy:   "human-admin",
	}, 0, "put-doc-1", now.Add(-29*time.Hour))
	if err != nil || !dupReplay || colVReplay != 1 || docReplay.Version != 1 {
		t.Fatalf("expected idempotent replay, got dup=%v colV=%d err=%v", dupReplay, colVReplay, err)
	}

	// CAS conflict
	_, _, _, err = repo.CircleResources().PutCircleResource(ctx, hub.CircleResource{
		HubID:       "hub",
		CircleID:    "circle-res-empty",
		Slug:        "arch-notes",
		Kind:        hub.ResourceKindDoc,
		Title:       "Architecture v2",
		Summary:     "Update",
		Content:     "# Architecture v2",
		ContentHash: hub.CircleSkillContentHash("# Architecture v2"),
		UpdatedBy:   "human-admin",
	}, 0, "put-doc-conflict", now)
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("expected ErrConflict on stale expectedVersion, got %v", err)
	}

	// 2. Submit & approve asset proposal with 60s TTL
	ttl60 := int64(60)
	proposal, _, isUpdate, dup, err := repo.CircleResources().SubmitCircleResourceProposal(ctx, hub.CircleResourceProposal{
		HubID:               "hub",
		CircleID:            "circle-res-empty",
		Slug:                "wireframe-png",
		Kind:                hub.ResourceKindAsset,
		Title:               "Wireframe",
		Summary:             "UI mockup",
		URL:                 "https://box.david888.com/f/wireframe.png",
		MediaType:           "image/png",
		Filename:            "wireframe.png",
		SizeBytes:           20480,
		ContentHash:         "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		RequestedTTLSeconds: &ttl60,
		ProposedBy:          "bot-hermes",
	}, "prop-asset-1", now.Add(-30*time.Hour))
	if err != nil || isUpdate || dup || proposal.Status != hub.ResourceProposalStatusPending {
		t.Fatalf("SubmitCircleResourceProposal failed: %+v isUpdate=%v dup=%v err=%v", proposal, isUpdate, dup, err)
	}

	approved, colV2, dup, err := repo.CircleResources().ApproveCircleResourceProposal(
		ctx, "hub", "circle-res-empty", "wireframe-png", "human-admin",
		hub.CircleResourceDecisionInput{ExpectedVersion: 0, IdempotencyKey: "app-asset-1"},
		now.Add(-2*time.Minute),
	)
	if err != nil || dup || colV2 != 2 || approved.Version != 1 || approved.Kind != hub.ResourceKindAsset || approved.ByteSize != 0 {
		t.Fatalf("ApproveCircleResourceProposal failed: res=%+v colV=%d dup=%v err=%v", approved, colV2, dup, err)
	}

	// Verify manifest before expiry includes doc (ByteSize) and asset (0 doc bytes)
	manifest, err := repo.CircleResources().ListCircleResourceManifest(ctx, "hub", "circle-res-empty", now.Add(-90*time.Second))
	if err != nil || manifest.ResourceCount != 2 || manifest.TotalDocBytes != len(docContent) {
		t.Fatalf("ListCircleResourceManifest failed: %+v err=%v", manifest, err)
	}

	// 3. Sweep expired resource after TTL elapsed
	expiredEvents, err := repo.CircleResources().SweepExpiredCircleResources(ctx, now)
	if err != nil || len(expiredEvents) != 1 || expiredEvents[0].Slug != "wireframe-png" {
		t.Fatalf("SweepExpiredCircleResources failed: events=%+v err=%v", expiredEvents, err)
	}
	if _, err := repo.CircleResources().GetCircleResource(ctx, "hub", "circle-res-empty", "wireframe-png", now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after TTL expiry, got %v", err)
	}

	// 4. Reject proposal flow
	_, _, _, _, err = repo.CircleResources().SubmitCircleResourceProposal(ctx, hub.CircleResourceProposal{
		HubID:       "hub",
		CircleID:    "circle-res-empty",
		Slug:        "bad-doc",
		Kind:        hub.ResourceKindDoc,
		Title:       "Bad",
		Summary:     "Bad",
		Content:     "Bad",
		ContentHash: hub.CircleSkillContentHash("Bad"),
		ProposedBy:  "bot-rogue",
	}, "prop-bad-1", now)
	if err != nil {
		t.Fatalf("SubmitCircleResourceProposal bad-doc failed: %v", err)
	}
	rejected, _, _, err := repo.CircleResources().RejectCircleResourceProposal(
		ctx, "hub", "circle-res-empty", "bad-doc", "human-admin",
		hub.CircleResourceDecisionInput{Reason: "off topic", IdempotencyKey: "rej-bad-1"},
		now,
	)
	if err != nil || rejected.Status != hub.ResourceProposalStatusRejected || rejected.ReviewReason != "off topic" {
		t.Fatalf("RejectCircleResourceProposal failed: %+v err=%v", rejected, err)
	}

	// 5. Log circle.resource_* event and verify PruneEmptyCircles cleans up dynamic empty circle + resource tables
	if err := repo.Events().AppendEvent(ctx, hub.Event{
		Type:      hub.EventCircleResourceApproved,
		HubID:     "hub",
		CircleID:  "circle-res-empty",
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	pruned, err := repo.Circles().PruneEmptyCircles(ctx, now.Add(-24*time.Hour), nil)
	if err != nil {
		t.Fatalf("PruneEmptyCircles failed: %v", err)
	}
	if pruned != 1 {
		t.Fatalf("expected 1 empty circle pruned despite recent circle.resource_* event, got %d", pruned)
	}
	remainingManifest, err := repo.CircleResources().ListCircleResourceManifest(ctx, "hub", "circle-res-empty", now)
	if err != nil || remainingManifest.ResourceCount != 0 || remainingManifest.CollectionVersion != 0 {
		t.Fatalf("expected pruned circle resources removed, got %+v err=%v", remainingManifest, err)
	}
}
