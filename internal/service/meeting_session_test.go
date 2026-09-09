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

func TestMeetingSessionTriggerAuthorizationAndIdempotency(t *testing.T) {
	ctx := context.Background()
	database, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer func() { _ = database.Close() }()

	cfg := config.Config{
		HubID:                 "public",
		ListenAddr:            ":0",
		DatabasePath:          filepath.Join(t.TempDir(), "unused.db"),
		PublicBaseURL:         "https://hub.example",
		RegistrationEnabled:   true,
		RegistrationTTL:       time.Hour,
		PeerLease:             time.Minute,
		MaxRegisteredAgents:   10,
		MaxTasksPerMinute:     50,
		MaxConcurrentTasks:    4,
		MaxPayloadBytes:       1 << 20,
		MaxGroupMembers:       4,
		MaxGroupFanout:        4,
		MaxGroupHistoryPage:   10,
		RegistrationPerMinute: 20,
	}
	handler := NewHTTPServer(New(sqlite.NewRepository(database), cfg)).Handler()

	register := func(name, key string, tags ...string) registeredTestAgent {
		payload := map[string]any{
			"displayName":                name,
			"providerFamily":             "test",
			"transportId":                "http-json",
			"capabilities":               []string{"text/plain", a2a.ExecutionCapability},
			"registrationIdempotencyKey": key,
		}
		if len(tags) > 0 {
			payload["tags"] = tags
		}
		response := doJSON(t, handler, http.MethodPost, "/hub/v1/agents/register", "", "", payload)
		if response.Code != http.StatusCreated {
			t.Fatalf("register %s = %d/%s", name, response.Code, response.Body.String())
		}
		var body struct {
			Identity hub.AgentIdentity `json:"identity"`
		}
		decodeResponse(t, response, &body)
		return registeredTestAgent{ID: body.Identity.AgentID, Token: body.Identity.AgentToken}
	}

	owner := register("owner", "sess-owner")
	secAgent := register("secretary", "sess-secretary")
	memberAgent := register("member", "sess-member")

	// 1. Create group and add secretary and member
	created := doJSON(t, handler, http.MethodPost, "/hub/v1/groups", owner.ID, owner.Token, map[string]string{"name": "GovGroup"})
	var group hub.Group
	decodeResponse(t, created, &group)

	for _, m := range []registeredTestAgent{secAgent, memberAgent} {
		invited := doJSON(t, handler, http.MethodPost, "/hub/v1/groups/"+group.GroupID+"/invitations", owner.ID, owner.Token, map[string]string{"agentId": m.ID})
		var invitation hub.GroupInvitation
		decodeResponse(t, invited, &invitation)
		accepted := doJSON(t, handler, http.MethodPost, "/hub/v1/groups/"+group.GroupID+"/accept", m.ID, m.Token, nil)
		if accepted.Code != http.StatusOK {
			t.Fatalf("accept %s = %d/%s", m.ID, accepted.Code, accepted.Body.String())
		}
	}

	// 2. Appoint secretary
	secPath := "/hub/v1/groups/" + group.GroupID + "/secretary"
	appointed := doJSON(t, handler, http.MethodPut, secPath, owner.ID, owner.Token, map[string]any{
		"agentId":       secAgent.ID,
		"expectedEpoch": 0,
		"leaseSeconds":  3600,
	})
	if appointed.Code != http.StatusCreated {
		t.Fatalf("appoint secretary failed: %d/%s", appointed.Code, appointed.Body.String())
	}

	// 3. Normal member sends /minutes -> does NOT trigger meeting session (untrusted text cannot invoke commands)
	msgRes := doJSON(t, handler, http.MethodPost, "/hub/v1/groups/"+group.GroupID+"/messages", memberAgent.ID, memberAgent.Token, map[string]string{
		"message": "/minutes",
	})
	if msgRes.Code != http.StatusAccepted && msgRes.Code != http.StatusOK {
		t.Fatalf("member send message: %d/%s", msgRes.Code, msgRes.Body.String())
	}

	// Verify no session was created
	sessionsRes := doJSON(t, handler, http.MethodGet, "/hub/v1/groups/"+group.GroupID+"/sessions", owner.ID, owner.Token, nil)
	var sessionsList struct {
		Sessions []hub.MeetingSession `json:"sessions"`
	}
	decodeResponse(t, sessionsRes, &sessionsList)
	if len(sessionsList.Sessions) != 0 {
		t.Fatalf("expected 0 sessions after untrusted member /minutes, got %d", len(sessionsList.Sessions))
	}

	// 4. Owner explicitly calls trigger session via API
	triggerRes := doJSON(t, handler, http.MethodPost, "/hub/v1/groups/"+group.GroupID+"/sessions", owner.ID, owner.Token, map[string]string{
		"command":          "/summary",
		"triggerMessageId": "msg-123",
	})
	if triggerRes.Code != http.StatusAccepted {
		t.Fatalf("trigger session by owner: %d/%s", triggerRes.Code, triggerRes.Body.String())
	}
	var session hub.MeetingSession
	decodeResponse(t, triggerRes, &session)
	if session.State != hub.MeetingSessionSynthesizing {
		t.Fatalf("expected state SYNTHESIZING, got %s", session.State)
	}

	// 5. Idempotency test: repeating the trigger with the same triggerMessageId returns the existing session
	repeatRes := doJSON(t, handler, http.MethodPost, "/hub/v1/groups/"+group.GroupID+"/sessions", owner.ID, owner.Token, map[string]string{
		"command":          "/summary",
		"triggerMessageId": "msg-123",
	})
	if repeatRes.Code != http.StatusAccepted {
		t.Fatalf("repeat trigger failed: %d/%s", repeatRes.Code, repeatRes.Body.String())
	}
	var repeatSession hub.MeetingSession
	decodeResponse(t, repeatRes, &repeatSession)
	if repeatSession.SessionID != session.SessionID {
		t.Fatalf("idempotency violation: expected %s, got %s", session.SessionID, repeatSession.SessionID)
	}

	// 6. Check that Secretary agent received the synthesis task in their inbox
	inboxRes := doJSON(t, handler, http.MethodGet, "/hub/v1/agents/"+secAgent.ID+"/inbox", secAgent.ID, secAgent.Token, nil)
	var inboxBody struct {
		Items []hub.InboxItem `json:"items"`
	}
	decodeResponse(t, inboxRes, &inboxBody)
	var foundTask bool
	for _, item := range inboxBody.Items {
		if strings.Contains(item.Message, "MEETING_SYNTHESIS") && strings.Contains(item.Message, session.SessionID) {
			foundTask = true
			break
		}
	}
	if !foundTask {
		t.Fatalf("secretary inbox did not receive MEETING_SYNTHESIS task for session %s", session.SessionID)
	}

	// 7. Member tries to conclude session -> Forbidden
	concludePath := "/hub/v1/groups/" + group.GroupID + "/sessions/" + session.SessionID + "/conclude"
	badConclude := doJSON(t, handler, http.MethodPost, concludePath, memberAgent.ID, memberAgent.Token, map[string]any{"decisions": 1, "actions": 1})
	if badConclude.Code != http.StatusForbidden {
		t.Fatalf("unauthorized conclude should return 403, got %d", badConclude.Code)
	}

	// 8. Secretary concludes session -> OK
	goodConclude := doJSON(t, handler, http.MethodPost, concludePath, secAgent.ID, secAgent.Token, map[string]any{"decisions": 2, "actions": 3})
	if goodConclude.Code != http.StatusOK {
		t.Fatalf("secretary conclude session: %d/%s", goodConclude.Code, goodConclude.Body.String())
	}
	var concludedSession hub.MeetingSession
	decodeResponse(t, goodConclude, &concludedSession)
	if concludedSession.State != hub.MeetingSessionConcluded {
		t.Fatalf("expected state CONCLUDED, got %s", concludedSession.State)
	}
	if concludedSession.ConcludedAt == nil {
		t.Fatalf("expected non-nil ConcludedAt")
	}
}
