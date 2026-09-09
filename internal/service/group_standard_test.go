package service

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tbdavid2019/888a2a-lite/internal/a2a"
	"github.com/tbdavid2019/888a2a-lite/internal/config"
	"github.com/tbdavid2019/888a2a-lite/internal/hub"
	"github.com/tbdavid2019/888a2a-lite/internal/store/sqlite"
)

func TestStandardGroupDiscoveryFanoutAndAggregation(t *testing.T) {
	ctx := context.Background()
	database, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer func() { _ = database.Close() }()
	cfg := config.Config{HubID: "public", ListenAddr: ":0", DatabasePath: filepath.Join(t.TempDir(), "unused.db"), PublicBaseURL: "https://hub.example", RegistrationEnabled: true, RegistrationTTL: time.Hour, PeerLease: time.Minute, MaxRegisteredAgents: 10, MaxTasksPerMinute: 50, MaxConcurrentTasks: 4, MaxPayloadBytes: 1 << 20, MaxGroupMembers: 4, MaxGroupFanout: 4, MaxGroupHistoryPage: 10, RegistrationPerMinute: 20, StandardGatewayEnabled: true, GroupExtensionEnabled: true}
	handler := NewHTTPServer(New(sqlite.NewRepository(database), cfg)).Handler()
	agents := make([]registeredTestAgent, 0, 3)
	for i, name := range []string{"owner", "member-a", "member-b"} {
		response := doJSON(t, handler, http.MethodPost, "/hub/v1/agents/register", "", "", map[string]any{"displayName": name, "providerFamily": "test", "transportId": "http-json", "capabilities": []string{"text/plain", a2a.ExecutionCapability}, "registrationIdempotencyKey": "standard-group-" + string(rune('a'+i))})
		if response.Code != http.StatusCreated {
			t.Fatalf("register %s = %d/%s", name, response.Code, response.Body.String())
		}
		var body struct {
			Identity hub.AgentIdentity `json:"identity"`
		}
		decodeResponse(t, response, &body)
		agents = append(agents, registeredTestAgent{ID: body.Identity.AgentID, Token: body.Identity.AgentToken})
	}
	created := doJSON(t, handler, http.MethodPost, "/hub/v1/groups", agents[0].ID, agents[0].Token, map[string]string{"name": "standard coordination"})
	if created.Code != http.StatusCreated {
		t.Fatalf("create group = %d/%s", created.Code, created.Body.String())
	}
	var group hub.Group
	decodeResponse(t, created, &group)
	for _, target := range agents[1:] {
		invited := doJSON(t, handler, http.MethodPost, "/hub/v1/groups/"+group.GroupID+"/invitations", agents[0].ID, agents[0].Token, map[string]string{"agentId": target.ID})
		if invited.Code != http.StatusCreated {
			t.Fatalf("invite %s = %d/%s", target.ID, invited.Code, invited.Body.String())
		}
		var invitation hub.GroupInvitation
		decodeResponse(t, invited, &invitation)
		accepted := doJSON(t, handler, http.MethodPost, "/hub/v1/groups/"+group.GroupID+"/accept", target.ID, target.Token, nil)
		if accepted.Code != http.StatusOK {
			t.Fatalf("accept %s = %d/%s", target.ID, accepted.Code, accepted.Body.String())
		}
	}
	groups := doGroupA2ARequest(t, handler, http.MethodGet, "/a2a/v1/groups", agents[0].Token, "", nil)
	if groups.Code != http.StatusOK || !strings.Contains(groups.Body.String(), group.GroupID) || !strings.Contains(groups.Body.String(), `"memberCount":3`) {
		t.Fatalf("group discovery = %d/%s", groups.Code, groups.Body.String())
	}
	card := doGroupA2ARequest(t, handler, http.MethodGet, "/a2a/v1/groups/"+group.GroupID+"/card", agents[0].Token, "", nil)
	if card.Code != http.StatusOK || !strings.Contains(card.Body.String(), `"tenant":"group:`+group.GroupID+`"`) || !strings.Contains(card.Body.String(), a2a.GroupExtensionURI) || strings.Contains(card.Body.String(), agents[1].ID) {
		t.Fatalf("group card = %d/%s", card.Code, card.Body.String())
	}

	body := map[string]any{"tenant": "group:" + group.GroupID, "message": map[string]any{"messageId": "group-message-1", "contextId": "group-context-1", "role": "ROLE_USER", "parts": []any{map[string]any{"text": "coordinate quietly"}}, "metadata": map[string]any{a2a.GroupExtensionURI: map[string]any{"replyPolicy": "ACK_ONLY"}}}, "configuration": map[string]any{"returnImmediately": true}}
	withoutOptIn := doGroupA2ARequest(t, handler, http.MethodPost, "/a2a/v1/message:send", agents[0].Token, "", body)
	if withoutOptIn.Code != http.StatusBadRequest || !strings.Contains(withoutOptIn.Body.String(), `"reason":"EXTENSION_SUPPORT_REQUIRED"`) {
		t.Fatalf("missing extension opt-in = %d/%s", withoutOptIn.Code, withoutOptIn.Body.String())
	}
	sent := doGroupA2ARequest(t, handler, http.MethodPost, "/a2a/v1/message:send", agents[0].Token, a2a.GroupExtensionURI, body)
	if sent.Code != http.StatusOK || !strings.Contains(sent.Body.String(), string(a2a.TaskStateSubmitted)) {
		t.Fatalf("group send = %d/%s", sent.Code, sent.Body.String())
	}
	var envelope a2a.SendMessageResponse
	if err := json.NewDecoder(sent.Body).Decode(&envelope); err != nil || envelope.Task == nil {
		t.Fatalf("group envelope = %s", sent.Body.String())
	}
	parentID := envelope.Task.ID
	childTasks := make([]hub.InboxItem, 0, 2)
	for _, target := range agents[1:] {
		inbox := doJSON(t, handler, http.MethodGet, "/hub/v1/agents/"+target.ID+"/inbox?afterSequence=0", target.ID, target.Token, nil)
		if inbox.Code != http.StatusOK {
			t.Fatalf("child inbox = %d/%s", inbox.Code, inbox.Body.String())
		}
		var inboxBody struct {
			Items []hub.InboxItem `json:"items"`
		}
		decodeResponse(t, inbox, &inboxBody)
		for _, item := range inboxBody.Items {
			if item.ParentTaskID == parentID {
				childTasks = append(childTasks, item)
			}
		}
	}
	if len(childTasks) != 2 {
		t.Fatalf("member deliveries = %+v", childTasks)
	}
	for i, item := range childTasks {
		target := agents[1+i]
		ack := doJSON(t, handler, http.MethodPost, "/hub/v1/agents/"+target.ID+"/inbox/"+itoa(item.Sequence)+"/ack", target.ID, target.Token, nil)
		if ack.Code != http.StatusOK {
			t.Fatalf("child ACK = %d/%s", ack.Code, ack.Body.String())
		}
		update := map[string]any{"updateId": "group-update-" + target.ID, "turnId": item.TurnID, "expectedRevision": 2, "state": string(a2a.TaskStateCompleted), "artifacts": []any{}}
		updated := doGroupA2ARequest(t, handler, http.MethodPost, "/hub/v1/a2a/tasks/"+item.MemberTaskID+"/updates", target.Token, "", update)
		if updated.Code != http.StatusOK {
			t.Fatalf("child update = %d/%s", updated.Code, updated.Body.String())
		}
	}
	completed := doGroupA2ARequest(t, handler, http.MethodGet, "/a2a/v1/tasks/"+parentID, agents[0].Token, a2a.GroupExtensionURI, nil)
	if completed.Code != http.StatusOK || !strings.Contains(completed.Body.String(), string(a2a.TaskStateCompleted)) {
		t.Fatalf("parent completed = %d/%s", completed.Code, completed.Body.String())
	}
	retry := doGroupA2ARequest(t, handler, http.MethodPost, "/a2a/v1/message:send", agents[0].Token, a2a.GroupExtensionURI, body)
	if retry.Code != http.StatusOK || !strings.Contains(retry.Body.String(), parentID) {
		t.Fatalf("group retry = %d/%s", retry.Code, retry.Body.String())
	}
	changed := body
	changed["message"].(map[string]any)["parts"] = []any{map[string]any{"text": "different"}}
	conflict := doGroupA2ARequest(t, handler, http.MethodPost, "/a2a/v1/message:send", agents[0].Token, a2a.GroupExtensionURI, changed)
	if conflict.Code != http.StatusConflict {
		t.Fatalf("group changed messageId = %d/%s", conflict.Code, conflict.Body.String())
	}
}

func doGroupA2ARequest(t *testing.T, handler http.Handler, method, path, token, extension string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal group request: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}
	request := httptest.NewRequest(method, path, reader)
	if body != nil {
		request.Header.Set("Content-Type", a2a.MediaType)
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if extension != "" {
		request.Header.Set("A2A-Extensions", extension)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
