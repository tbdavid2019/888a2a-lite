package service

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tbdavid2019/888a2a-lite/internal/config"
	"github.com/tbdavid2019/888a2a-lite/internal/hub"
	"github.com/tbdavid2019/888a2a-lite/internal/store/sqlite"
)

func TestHTTPGroupLifecycleFanoutAndAuthorization(t *testing.T) {
	ctx := context.Background()
	database, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer func() { _ = database.Close() }()
	cfg := config.Config{
		HubID: "public", ListenAddr: ":0", DatabasePath: filepath.Join(t.TempDir(), "unused.db"),
		PublicBaseURL: "https://hub.example", RegistrationEnabled: true,
		RegistrationTTL: 24 * time.Hour, PeerLease: 90 * time.Second,
		MaxRegisteredAgents: 10, MaxTasksPerMinute: 20, MaxConcurrentTasks: 4,
		MaxPayloadBytes: 1 << 20, MaxGroupMembers: 4, MaxGroupFanout: 4,
		MaxGroupHistoryPage: 10, RegistrationPerMinute: 20, OperatorToken: "operator-fixture",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("config.Validate: %v", err)
	}
	handler := NewHTTPServer(New(sqlite.NewRepository(database), cfg)).Handler()
	agents := make([]registeredTestAgent, 0, 4)
	for i, name := range []string{"owner", "member", "observer", "outsider"} {
		response := doJSON(t, handler, http.MethodPost, "/hub/v1/agents/register", "", "", map[string]any{
			"displayName": name, "providerFamily": name, "transportId": "http-json",
			"capabilities": []string{"text/plain"}, "registrationIdempotencyKey": "group-install-" + string(rune('a'+i)),
		})
		if response.Code != http.StatusCreated {
			t.Fatalf("register %s = %d/%s", name, response.Code, response.Body.String())
		}
		var body struct {
			Identity hub.AgentIdentity `json:"identity"`
		}
		decodeResponse(t, response, &body)
		agents = append(agents, registeredTestAgent{ID: body.Identity.AgentID, Token: body.Identity.AgentToken})
	}

	created := doJSON(t, handler, http.MethodPost, "/hub/v1/groups", agents[0].ID, agents[0].Token, map[string]string{"name": "coordination"})
	if created.Code != http.StatusCreated {
		t.Fatalf("create group = %d/%s", created.Code, created.Body.String())
	}
	var group hub.Group
	decodeResponse(t, created, &group)
	if group.GroupID == "" || group.OwnerAgentID != agents[0].ID {
		t.Fatalf("created group = %+v", group)
	}

	invitationIDs := make([]uint64, 0, 2)
	for _, target := range agents[1:3] {
		invited := doJSON(t, handler, http.MethodPost, "/hub/v1/groups/"+group.GroupID+"/invitations", agents[0].ID, agents[0].Token, map[string]string{"agentId": target.ID})
		if invited.Code != http.StatusCreated {
			t.Fatalf("invite %s = %d/%s", target.ID, invited.Code, invited.Body.String())
		}
		var invitation hub.GroupInvitation
		decodeResponse(t, invited, &invitation)
		invitationIDs = append(invitationIDs, invitation.ID)
	}
	for i, invitationID := range invitationIDs {
		accepted := doJSON(t, handler, http.MethodPost, "/hub/v1/groups/invitations/"+itoa(invitationID)+"/accept", agents[i+1].ID, agents[i+1].Token, nil)
		if accepted.Code != http.StatusOK {
			t.Fatalf("accept invitation = %d/%s", accepted.Code, accepted.Body.String())
		}
	}

	roster := doJSON(t, handler, http.MethodGet, "/hub/v1/groups/"+group.GroupID+"/roster", agents[0].ID, agents[0].Token, nil)
	if roster.Code != http.StatusOK || strings.Contains(roster.Body.String(), agents[0].Token) || !strings.Contains(roster.Body.String(), agents[1].ID) || !strings.Contains(roster.Body.String(), agents[2].ID) {
		t.Fatalf("roster = %d/%s", roster.Code, roster.Body.String())
	}

	sent := doJSON(t, handler, http.MethodPost, "/hub/v1/groups/"+group.GroupID+"/messages", agents[0].ID, agents[0].Token, map[string]string{
		"contextId": "ctx-1", "idempotencyKey": "group-message-1", "message": "hello group",
	})
	if sent.Code != http.StatusAccepted || !strings.Contains(sent.Body.String(), "UNTRUSTED_DATA") || !strings.Contains(sent.Body.String(), agents[1].ID) || !strings.Contains(sent.Body.String(), agents[2].ID) {
		t.Fatalf("send group message = %d/%s", sent.Code, sent.Body.String())
	}
	duplicate := doJSON(t, handler, http.MethodPost, "/hub/v1/groups/"+group.GroupID+"/messages", agents[0].ID, agents[0].Token, map[string]string{
		"contextId": "ctx-1", "idempotencyKey": "group-message-1", "message": "hello group",
	})
	if duplicate.Code != http.StatusAccepted || !strings.Contains(duplicate.Body.String(), "DUPLICATE") {
		t.Fatalf("duplicate group message = %d/%s", duplicate.Code, duplicate.Body.String())
	}

	var inboxes [2][]hub.InboxItem
	for i, target := range agents[1:3] {
		inbox := doJSON(t, handler, http.MethodGet, "/hub/v1/agents/"+target.ID+"/inbox?afterSequence=0", target.ID, target.Token, nil)
		if inbox.Code != http.StatusOK || !strings.Contains(inbox.Body.String(), "hello group") {
			t.Fatalf("group inbox %d = %d/%s", i, inbox.Code, inbox.Body.String())
		}
		var body struct {
			Items []hub.InboxItem `json:"items"`
		}
		decodeResponse(t, inbox, &body)
		inboxes[i] = body.Items
		if len(body.Items) != 1 || body.Items[0].GroupMessageID == 0 {
			t.Fatalf("group inbox %d items = %+v", i, body.Items)
		}
	}
	ack := doJSON(t, handler, http.MethodPost, "/hub/v1/agents/"+agents[1].ID+"/inbox/"+itoa(inboxes[0][0].Sequence)+"/ack", agents[1].ID, agents[1].Token, nil)
	if ack.Code != http.StatusOK {
		t.Fatalf("ack group message = %d/%s", ack.Code, ack.Body.String())
	}
	history := doJSON(t, handler, http.MethodGet, "/hub/v1/groups/"+group.GroupID+"/history?afterId=0&limit=10", agents[1].ID, agents[1].Token, nil)
	if history.Code != http.StatusOK || !strings.Contains(history.Body.String(), "hello group") || !strings.Contains(history.Body.String(), "UNTRUSTED_DATA") {
		t.Fatalf("group history = %d/%s", history.Code, history.Body.String())
	}
	outsiderHistory := doJSON(t, handler, http.MethodGet, "/hub/v1/groups/"+group.GroupID+"/history?afterId=0", agents[3].ID, agents[3].Token, nil)
	if outsiderHistory.Code != http.StatusForbidden {
		t.Fatalf("outsider history = %d/%s", outsiderHistory.Code, outsiderHistory.Body.String())
	}

	second := doJSON(t, handler, http.MethodPost, "/hub/v1/groups/"+group.GroupID+"/messages", agents[0].ID, agents[0].Token, map[string]string{
		"contextId": "ctx-2", "idempotencyKey": "group-message-2", "message": "remove before poll",
	})
	if second.Code != http.StatusAccepted {
		t.Fatalf("second group message = %d/%s", second.Code, second.Body.String())
	}
	removed := doJSON(t, handler, http.MethodPost, "/hub/v1/groups/"+group.GroupID+"/members/"+agents[2].ID+"/remove", agents[0].ID, agents[0].Token, nil)
	if removed.Code != http.StatusOK {
		t.Fatalf("remove member = %d/%s", removed.Code, removed.Body.String())
	}
	removedInbox := doJSON(t, handler, http.MethodGet, "/hub/v1/agents/"+agents[2].ID+"/inbox?afterSequence=0", agents[2].ID, agents[2].Token, nil)
	if removedInbox.Code != http.StatusOK || strings.Contains(removedInbox.Body.String(), "remove before poll") {
		t.Fatalf("removed member inbox = %d/%s", removedInbox.Code, removedInbox.Body.String())
	}
	archived := doJSON(t, handler, http.MethodPost, "/hub/v1/groups/"+group.GroupID+"/archive", agents[0].ID, agents[0].Token, nil)
	if archived.Code != http.StatusOK {
		t.Fatalf("archive group = %d/%s", archived.Code, archived.Body.String())
	}
	blocked := doJSON(t, handler, http.MethodPost, "/hub/v1/groups/"+group.GroupID+"/messages", agents[0].ID, agents[0].Token, map[string]string{
		"contextId": "ctx-3", "idempotencyKey": "group-message-3", "message": "blocked",
	})
	if blocked.Code != http.StatusConflict || !strings.Contains(blocked.Body.String(), "GROUP_ARCHIVED") {
		t.Fatalf("archived send = %d/%s", blocked.Code, blocked.Body.String())
	}

	var card hub.HubSystemCard
	decodeResponse(t, doJSON(t, handler, http.MethodGet, "/hub/v1/system-card.json", "", "", nil), &card)
	cardJSON, _ := json.Marshal(card)
	if !strings.Contains(string(cardJSON), hub.GroupExtensionURI) || card.GroupBaseURL != "https://hub.example/hub/v1/groups" {
		t.Fatalf("system card group extension = %s", cardJSON)
	}
}

func TestExpiredGroupInvitationRenewalAndReinvitation(t *testing.T) {
	ctx := context.Background()
	database, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer func() { _ = database.Close() }()
	repo := sqlite.NewRepository(database)
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	for _, agentID := range []string{"owner-1", "member-1"} {
		if err := repo.CreateAgent(ctx, hub.RegisteredAgent{
			HubID: "public", AgentID: agentID, RegistrationKeyHash: hub.HashToken("reg-" + agentID),
			TokenHash: hub.HashToken("tok-" + agentID), DisplayName: agentID, ProviderFamily: "test",
			TransportID: "http-json", Capabilities: []string{"text/plain"}, State: hub.AgentStatePending,
			ExpiresAt: now.Add(24 * time.Hour), CreatedAt: now,
		}); err != nil {
			t.Fatalf("CreateAgent: %v", err)
		}
	}
	group, err := repo.Groups().CreateGroup(ctx, hub.Group{
		HubID: "public", GroupID: "group-renew", Name: "renew-test", State: hub.GroupStateActive,
		OwnerAgentID: "owner-1", CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	// Create an already-expired invitation
	expiredInv, err := repo.Groups().CreateInvitation(ctx, hub.GroupInvitation{
		HubID: "public", GroupID: group.GroupID, InviterAgentID: "owner-1", InviteeAgentID: "member-1",
		State: hub.InvitationPending, CreatedAt: now.Add(-48 * time.Hour), ExpiresAt: now.Add(-24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("CreateInvitation: %v", err)
	}
	if expiredInv.ID == 0 {
		t.Fatal("expired invitation missing ID")
	}

	// FindPendingInvitation should NOT return the expired invitation
	if _, err := repo.Groups().FindPendingInvitation(ctx, group.GroupID, "member-1"); err == nil {
		t.Fatal("FindPendingInvitation returned an expired invitation, expected not found")
	}

	// Create a new invitation (should mark the old one EXPIRED and succeed)
	freshInv, err := repo.Groups().CreateInvitation(ctx, hub.GroupInvitation{
		HubID: "public", GroupID: group.GroupID, InviterAgentID: "owner-1", InviteeAgentID: "member-1",
		State: hub.InvitationPending, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("CreateInvitation fresh: %v", err)
	}
	if freshInv.ID == expiredInv.ID {
		t.Fatalf("fresh invitation ID %d should differ from expired ID %d", freshInv.ID, expiredInv.ID)
	}

	// Member should now be able to accept the fresh invitation
	member, err := repo.Groups().AcceptInvitation(ctx, freshInv.ID, "member-1", now.Add(time.Hour))
	if err != nil {
		t.Fatalf("AcceptInvitation fresh: %v", err)
	}
	if member.AgentID != "member-1" || member.State != hub.MembershipActive {
		t.Fatalf("unexpected member state: %+v", member)
	}
}
