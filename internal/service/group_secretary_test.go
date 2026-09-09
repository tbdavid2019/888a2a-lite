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

func TestGroupSecretaryAppointmentEpochAndLease(t *testing.T) {
	ctx := context.Background()
	database, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer func() { _ = database.Close() }()
	cfg := config.Config{HubID: "public", ListenAddr: ":0", DatabasePath: filepath.Join(t.TempDir(), "unused.db"), PublicBaseURL: "https://hub.example", RegistrationEnabled: true, RegistrationTTL: time.Hour, PeerLease: time.Minute, MaxRegisteredAgents: 10, MaxTasksPerMinute: 50, MaxConcurrentTasks: 4, MaxPayloadBytes: 1 << 20, MaxGroupMembers: 4, MaxGroupFanout: 4, MaxGroupHistoryPage: 10, RegistrationPerMinute: 20}
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
	owner, member := register("owner", "secretary-owner"), register("member", "secretary-member")
	created := doJSON(t, handler, http.MethodPost, "/hub/v1/groups", owner.ID, owner.Token, map[string]string{"name": "Secretary"})
	var group hub.Group
	decodeResponse(t, created, &group)
	invited := doJSON(t, handler, http.MethodPost, "/hub/v1/groups/"+group.GroupID+"/invitations", owner.ID, owner.Token, map[string]string{"agentId": member.ID})
	var invitation hub.GroupInvitation
	decodeResponse(t, invited, &invitation)
	accepted := doJSON(t, handler, http.MethodPost, "/hub/v1/groups/"+group.GroupID+"/accept", member.ID, member.Token, nil)
	if accepted.Code != http.StatusOK {
		t.Fatalf("accept member = %d/%s", accepted.Code, accepted.Body.String())
	}
	path := "/hub/v1/groups/" + group.GroupID + "/secretary"
	appointed := doJSON(t, handler, http.MethodPut, path, owner.ID, owner.Token, map[string]any{"agentId": member.ID, "expectedEpoch": 0, "leaseSeconds": 60})
	if appointed.Code != http.StatusCreated || !strings.Contains(appointed.Body.String(), `"epoch":1`) || !strings.Contains(appointed.Body.String(), `"state":"ACTIVE"`) {
		t.Fatalf("appoint = %d/%s", appointed.Code, appointed.Body.String())
	}
	oldEpoch := doJSON(t, handler, http.MethodPut, path, owner.ID, owner.Token, map[string]any{"agentId": owner.ID, "expectedEpoch": 0, "leaseSeconds": 60})
	if oldEpoch.Code != http.StatusConflict {
		t.Fatalf("stale appointment = %d/%s", oldEpoch.Code, oldEpoch.Body.String())
	}
	replaced := doJSON(t, handler, http.MethodPut, path, owner.ID, owner.Token, map[string]any{"agentId": owner.ID, "expectedEpoch": 1, "leaseSeconds": 60})
	if replaced.Code != http.StatusCreated || !strings.Contains(replaced.Body.String(), `"epoch":2`) {
		t.Fatalf("replace secretary = %d/%s", replaced.Code, replaced.Body.String())
	}
	oldRenew := doJSON(t, handler, http.MethodPost, path+"/renew", member.ID, member.Token, map[string]any{"epoch": 1, "leaseSeconds": 60})
	if oldRenew.Code != http.StatusConflict {
		t.Fatalf("old secretary renew = %d/%s", oldRenew.Code, oldRenew.Body.String())
	}
	newRenew := doJSON(t, handler, http.MethodPost, path+"/renew", owner.ID, owner.Token, map[string]any{"epoch": 2, "leaseSeconds": 60})
	if newRenew.Code != http.StatusOK || !strings.Contains(newRenew.Body.String(), `"epoch":2`) {
		t.Fatalf("new secretary renew = %d/%s", newRenew.Code, newRenew.Body.String())
	}
	revoked := doJSON(t, handler, http.MethodPost, path+"/revoke", owner.ID, owner.Token, map[string]any{"epoch": 2})
	if revoked.Code != http.StatusOK || !strings.Contains(revoked.Body.String(), `"state":"REVOKED"`) {
		t.Fatalf("revoke secretary = %d/%s", revoked.Code, revoked.Body.String())
	}
	memberCannotAppoint := doJSON(t, handler, http.MethodPut, path, member.ID, member.Token, map[string]any{"agentId": member.ID, "expectedEpoch": 2, "leaseSeconds": 60})
	if memberCannotAppoint.Code != http.StatusForbidden {
		t.Fatalf("member appointment = %d/%s", memberCannotAppoint.Code, memberCannotAppoint.Body.String())
	}
}
