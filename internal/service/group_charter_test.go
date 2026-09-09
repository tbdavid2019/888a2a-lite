package service

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tbdavid2019/888a2a-lite/internal/a2a"
	"github.com/tbdavid2019/888a2a-lite/internal/config"
	"github.com/tbdavid2019/888a2a-lite/internal/hub"
	"github.com/tbdavid2019/888a2a-lite/internal/store/sqlite"
)

func TestGroupCharterLifecycleCASAndScope(t *testing.T) {
	ctx := context.Background()
	database, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer func() { _ = database.Close() }()
	cfg := config.Config{HubID: "public", ListenAddr: ":0", DatabasePath: filepath.Join(t.TempDir(), "unused.db"), PublicBaseURL: "https://hub.example", RegistrationEnabled: true, RegistrationTTL: time.Hour, PeerLease: time.Minute, MaxRegisteredAgents: 10, MaxTasksPerMinute: 50, MaxConcurrentTasks: 4, MaxPayloadBytes: 1 << 20, MaxGroupMembers: 4, MaxGroupFanout: 4, MaxGroupHistoryPage: 10, RegistrationPerMinute: 20, StandardGatewayEnabled: true, GroupExtensionEnabled: true}
	handler := NewHTTPServer(New(sqlite.NewRepository(database), cfg)).Handler()
	register := func(name, key string) registeredTestAgent {
		response := doJSON(t, handler, http.MethodPost, "/hub/v1/agents/register", "", "", map[string]any{"displayName": name, "providerFamily": "test", "transportId": "http-json", "capabilities": []string{"text/plain", a2a.ExecutionCapability}, "registrationIdempotencyKey": key})
		if response.Code != http.StatusCreated {
			t.Fatalf("register %s = %d/%s", name, response.Code, response.Body.String())
		}
		var body struct {
			Identity hub.AgentIdentity `json:"identity"`
		}
		decodeResponse(t, response, &body)
		return registeredTestAgent{ID: body.Identity.AgentID, Token: body.Identity.AgentToken}
	}
	owner, member, nonMember := register("owner", "charter-owner"), register("member", "charter-member"), register("outside", "charter-outside")
	created := doJSON(t, handler, http.MethodPost, "/hub/v1/groups", owner.ID, owner.Token, map[string]string{"name": "Governance"})
	if created.Code != http.StatusCreated {
		t.Fatalf("create group = %d/%s", created.Code, created.Body.String())
	}
	var group hub.Group
	decodeResponse(t, created, &group)
	invited := doJSON(t, handler, http.MethodPost, "/hub/v1/groups/"+group.GroupID+"/invitations", owner.ID, owner.Token, map[string]string{"agentId": member.ID})
	if invited.Code != http.StatusCreated {
		t.Fatalf("invite member = %d/%s", invited.Code, invited.Body.String())
	}
	accepted := doJSON(t, handler, http.MethodPost, "/hub/v1/groups/"+group.GroupID+"/accept", member.ID, member.Token, nil)
	if accepted.Code != http.StatusOK {
		t.Fatalf("accept member = %d/%s", accepted.Code, accepted.Body.String())
	}
	charterPath := "/hub/v1/groups/" + group.GroupID + "/charter"
	empty := doJSON(t, handler, http.MethodGet, charterPath, member.ID, member.Token, nil)
	if empty.Code != http.StatusOK || empty.Header().Get("Cache-Control") != "private, no-store" || !strings.Contains(empty.Body.String(), `"charterVersion":0`) || !strings.Contains(empty.Body.String(), `"hasCharter":false`) {
		t.Fatalf("empty charter = %d/%s", empty.Code, empty.Body.String())
	}
	content := "# Governance\n\n- Owner approves decisions.\n- Actions remain drafts until approved."
	put := doJSON(t, handler, http.MethodPut, charterPath, owner.ID, owner.Token, map[string]any{"content": content, "expectedVersion": 0, "idempotencyKey": "charter-v1"})
	if put.Code != http.StatusCreated || !strings.Contains(put.Body.String(), `"charterVersion":1`) || !strings.Contains(put.Body.String(), hub.GroupCharterContentHash(content)) {
		t.Fatalf("put charter = %d/%s", put.Code, put.Body.String())
	}
	retry := doJSON(t, handler, http.MethodPut, charterPath, owner.ID, owner.Token, map[string]any{"content": content, "expectedVersion": 0, "idempotencyKey": "charter-v1"})
	if retry.Code != http.StatusOK || !strings.Contains(retry.Body.String(), `"charterVersion":1`) {
		t.Fatalf("idempotent charter retry = %d/%s", retry.Code, retry.Body.String())
	}
	conflict := doJSON(t, handler, http.MethodPut, charterPath, owner.ID, owner.Token, map[string]any{"content": "# newer", "expectedVersion": 0, "idempotencyKey": "charter-conflict"})
	if conflict.Code != http.StatusConflict {
		t.Fatalf("stale charter update = %d/%s", conflict.Code, conflict.Body.String())
	}
	unsafe := doJSON(t, handler, http.MethodPut, charterPath, owner.ID, owner.Token, map[string]any{"content": "<script>alert(1)</script>", "expectedVersion": 1, "idempotencyKey": "charter-unsafe"})
	if unsafe.Code != http.StatusBadRequest {
		t.Fatalf("unsafe charter = %d/%s", unsafe.Code, unsafe.Body.String())
	}
	nonMemberRead := doJSON(t, handler, http.MethodGet, charterPath, nonMember.ID, nonMember.Token, nil)
	if nonMemberRead.Code != http.StatusForbidden && nonMemberRead.Code != http.StatusNotFound {
		t.Fatalf("non-member charter read = %d/%s", nonMemberRead.Code, nonMemberRead.Body.String())
	}
	second := doJSON(t, handler, http.MethodPut, charterPath, owner.ID, owner.Token, map[string]any{"content": content + "\n- Review quarterly.", "expectedVersion": 1, "idempotencyKey": "charter-v2"})
	if second.Code != http.StatusCreated {
		t.Fatalf("second charter = %d/%s", second.Code, second.Body.String())
	}
	rollback := doJSON(t, handler, http.MethodPost, "/hub/v1/groups/"+group.GroupID+"/charter/rollback", owner.ID, owner.Token, map[string]any{"targetVersion": 1, "expectedVersion": 2, "idempotencyKey": "charter-rollback-v1"})
	if rollback.Code != http.StatusCreated || !strings.Contains(rollback.Body.String(), `"charterVersion":3`) {
		t.Fatalf("rollback = %d/%s", rollback.Code, rollback.Body.String())
	}
	revisions := doJSON(t, handler, http.MethodGet, "/hub/v1/groups/"+group.GroupID+"/charter/revisions", member.ID, member.Token, nil)
	if revisions.Code != http.StatusOK || strings.Count(revisions.Body.String(), `"charterVersion"`) != 3 {
		t.Fatalf("charter revisions = %d/%s", revisions.Code, revisions.Body.String())
	}
	card := doGroupA2ARequest(t, handler, http.MethodGet, "/a2a/v1/groups/"+group.GroupID+"/card", member.Token, "", nil)
	if card.Code != http.StatusOK || !strings.Contains(card.Body.String(), `"hasCharter":true`) || !strings.Contains(card.Body.String(), `"charterVersion":3`) || strings.Contains(card.Body.String(), content) {
		t.Fatalf("charter metadata card = %d/%s", card.Code, card.Body.String())
	}
}
