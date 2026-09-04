package service

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tbdavid2019/888a2a-lite/internal/config"
	"github.com/tbdavid2019/888a2a-lite/internal/hub"
	"github.com/tbdavid2019/888a2a-lite/internal/store/sqlite"
)

func TestHTTPThreeAgentDeliveryAndAuthorizationBoundaries(t *testing.T) {
	ctx := context.Background()
	database, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer func() {
		if err := database.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	}()
	repository := sqlite.NewRepository(database)
	cfg := config.Config{
		HubID:                 "public",
		ListenAddr:            ":0",
		DatabasePath:          filepath.Join(t.TempDir(), "unused.db"),
		PublicBaseURL:         "https://hub.example",
		RegistrationEnabled:   true,
		RegistrationTTL:       24 * time.Hour,
		PeerLease:             90 * time.Second,
		MaxRegisteredAgents:   10,
		MaxTasksPerMinute:     20,
		MaxConcurrentTasks:    4,
		MaxPayloadBytes:       1 << 20,
		RegistrationPerMinute: 20,
		OperatorToken:         "operator-fixture",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("config.Validate: %v", err)
	}
	if err := repository.Policy().SavePolicy(ctx, hub.HubPolicy{
		HubID:               cfg.HubID,
		RegistrationEnabled: cfg.RegistrationEnabled,
		RegistrationTTL:     cfg.RegistrationTTL,
		PeerLease:           cfg.PeerLease,
		MaxRegisteredAgents: cfg.MaxRegisteredAgents,
		MaxTasksPerMinute:   cfg.MaxTasksPerMinute,
		MaxConcurrentTasks:  cfg.MaxConcurrentTasks,
		MaxPayloadBytes:     cfg.MaxPayloadBytes,
	}); err != nil {
		t.Fatalf("save policy: %v", err)
	}
	handler := NewHTTPServer(New(repository, cfg)).Handler()

	agents := make([]registeredTestAgent, 0, 3)
	for i, name := range []string{"codex", "openclaw", "hermes"} {
		response := doJSON(t, handler, http.MethodPost, "/hub/v1/agents/register", "", "", map[string]any{
			"displayName":                name,
			"providerFamily":             name,
			"transportId":                "http-json",
			"capabilities":               []string{"text/plain"},
			"registrationIdempotencyKey": "installation-" + string(rune('a'+i)),
		})
		if response.Code != http.StatusCreated {
			t.Fatalf("register %s status = %d, body=%s", name, response.Code, response.Body.String())
		}
		var body struct {
			Identity hub.AgentIdentity `json:"identity"`
		}
		decodeResponse(t, response, &body)
		if body.Identity.AgentID == "" || body.Identity.AgentToken == "" {
			t.Fatalf("register %s identity = %+v", name, body.Identity)
		}
		agents = append(agents, registeredTestAgent{ID: body.Identity.AgentID, Token: body.Identity.AgentToken})
	}

	duplicate := doJSON(t, handler, http.MethodPost, "/hub/v1/agents/register", "", "", map[string]any{
		"displayName":                "codex",
		"providerFamily":             "codex",
		"transportId":                "http-json",
		"capabilities":               []string{"text/plain"},
		"registrationIdempotencyKey": "installation-a",
	})
	if duplicate.Code != http.StatusCreated || strings.Contains(duplicate.Body.String(), agents[0].Token) {
		t.Fatalf("duplicate registration leaked token or returned wrong status: %s", duplicate.Body.String())
	}

	peers := doJSON(t, handler, http.MethodGet, "/hub/v1/agents", agents[0].ID, agents[0].Token, nil)
	if peers.Code != http.StatusOK || strings.Contains(peers.Body.String(), agents[0].Token) || !strings.Contains(peers.Body.String(), agents[1].ID) {
		t.Fatalf("peer response status/body = %d/%s", peers.Code, peers.Body.String())
	}

	task := map[string]any{
		"contextId":      "context-1",
		"idempotencyKey": "task-1",
		"message":        "hello from codex",
		"taskId":         "task-1",
	}
	sent := doJSON(t, handler, http.MethodPost, "/hub/v1/agents/"+agents[1].ID+"/tasks", agents[0].ID, agents[0].Token, task)
	if sent.Code != http.StatusAccepted || !strings.Contains(sent.Body.String(), `"QUEUED"`) {
		t.Fatalf("send task status/body = %d/%s", sent.Code, sent.Body.String())
	}
	duplicateTask := doJSON(t, handler, http.MethodPost, "/hub/v1/agents/"+agents[1].ID+"/tasks", agents[0].ID, agents[0].Token, task)
	if duplicateTask.Code != http.StatusAccepted || !strings.Contains(duplicateTask.Body.String(), `"DUPLICATE"`) {
		t.Fatalf("duplicate task status/body = %d/%s", duplicateTask.Code, duplicateTask.Body.String())
	}

	inbox := doJSON(t, handler, http.MethodGet, "/hub/v1/agents/"+agents[1].ID+"/inbox?afterSequence=0", agents[1].ID, agents[1].Token, nil)
	if inbox.Code != http.StatusOK || !strings.Contains(inbox.Body.String(), "hello from codex") {
		t.Fatalf("inbox status/body = %d/%s", inbox.Code, inbox.Body.String())
	}
	var inboxBody struct {
		Items []hub.InboxItem `json:"items"`
	}
	decodeResponse(t, inbox, &inboxBody)
	if len(inboxBody.Items) != 1 {
		t.Fatalf("inbox items = %+v", inboxBody.Items)
	}
	ack := doJSON(t, handler, http.MethodPost, "/hub/v1/agents/"+agents[1].ID+"/inbox/"+itoa(inboxBody.Items[0].Sequence)+"/ack", agents[1].ID, agents[1].Token, nil)
	if ack.Code != http.StatusOK {
		t.Fatalf("ack status/body = %d/%s", ack.Code, ack.Body.String())
	}

	unauthorized := doJSON(t, handler, http.MethodGet, "/hub/v1/agents", agents[0].ID, "wrong-token", nil)
	if unauthorized.Code != http.StatusUnauthorized || !strings.Contains(unauthorized.Body.String(), "UNAUTHENTICATED") {
		t.Fatalf("unauthorized status/body = %d/%s", unauthorized.Code, unauthorized.Body.String())
	}

	revoke := doJSONWithBearer(t, handler, http.MethodPost, "/hub/v1/admin/agents/"+agents[2].ID+"/revoke", "operator-fixture", map[string]string{"reason": "test"})
	if revoke.Code != http.StatusOK {
		t.Fatalf("revoke status/body = %d/%s", revoke.Code, revoke.Body.String())
	}
	heartbeat := doJSON(t, handler, http.MethodPost, "/hub/v1/agents/"+agents[2].ID+"/heartbeat", agents[2].ID, agents[2].Token, nil)
	if heartbeat.Code != http.StatusUnauthorized {
		t.Fatalf("revoked heartbeat status/body = %d/%s", heartbeat.Code, heartbeat.Body.String())
	}
	peersAfterRevoke := doJSON(t, handler, http.MethodGet, "/hub/v1/agents", agents[0].ID, agents[0].Token, nil)
	if peersAfterRevoke.Code != http.StatusOK || strings.Contains(peersAfterRevoke.Body.String(), agents[2].ID) {
		t.Fatalf("peers after revoke still contained revoked agent: %s", peersAfterRevoke.Body.String())
	}
	onlinePeers := doJSON(t, handler, http.MethodGet, "/hub/v1/agents?state=online", agents[0].ID, agents[0].Token, nil)
	if onlinePeers.Code != http.StatusOK || !strings.Contains(onlinePeers.Body.String(), agents[1].ID) {
		t.Fatalf("online peers did not return active peer: %s", onlinePeers.Body.String())
	}

	registration := doJSONWithBearer(t, handler, http.MethodPost, "/hub/v1/admin/registration", "operator-fixture", map[string]bool{"enabled": false})
	if registration.Code != http.StatusOK {
		t.Fatalf("registration control status/body = %d/%s", registration.Code, registration.Body.String())
	}
	blocked := doJSON(t, handler, http.MethodPost, "/hub/v1/agents/register", "", "", map[string]any{
		"displayName":                "new-agent",
		"providerFamily":             "test",
		"transportId":                "http-json",
		"registrationIdempotencyKey": "installation-new",
	})
	if blocked.Code != http.StatusForbidden || !strings.Contains(blocked.Body.String(), "REGISTRATION_DISABLED") {
		t.Fatalf("disabled registration status/body = %d/%s", blocked.Code, blocked.Body.String())
	}
	events := doJSONWithBearer(t, handler, http.MethodGet, "/hub/v1/admin/events?afterId=0&limit=100", "operator-fixture", nil)
	if events.Code != http.StatusOK || !strings.Contains(events.Body.String(), hub.EventAgentRegistered) || strings.Contains(events.Body.String(), "hello from codex") {
		t.Fatalf("events status/body = %d/%s", events.Code, events.Body.String())
	}
	card := doJSON(t, handler, http.MethodGet, "/hub/v1/system-card.json", "", "", nil)
	if card.Code != http.StatusOK || !strings.Contains(card.Body.String(), "SELF_DECLARED") || !strings.Contains(card.Body.String(), "https://hub.example/hub/v1/system-card.json") {
		t.Fatalf("system card status/body = %d/%s", card.Code, card.Body.String())
	}
	published := doJSONWithBearer(t, handler, http.MethodPost, "/hub/v1/admin/announcements", "operator-fixture", map[string]any{
		"title": "Maintenance", "summary": "Read-only maintenance notice", "severity": "WARNING",
	})
	if published.Code != http.StatusCreated || !strings.Contains(published.Body.String(), "Maintenance") {
		t.Fatalf("publish announcement status/body = %d/%s", published.Code, published.Body.String())
	}
	feed := doJSON(t, handler, http.MethodGet, "/hub/v1/announcements?afterId=0&limit=10", "", "", nil)
	if feed.Code != http.StatusOK || !strings.Contains(feed.Body.String(), "Read-only maintenance notice") {
		t.Fatalf("announcement feed status/body = %d/%s", feed.Code, feed.Body.String())
	}

	// Security: Direct task with reserved group: prefix must be rejected
	collisionTask := map[string]any{
		"contextId":      "context-coll",
		"idempotencyKey": "group:group-1:key1",
		"message":        "should be rejected",
		"taskId":         "task-coll",
	}
	collResp := doJSON(t, handler, http.MethodPost, "/hub/v1/agents/"+agents[1].ID+"/tasks", agents[0].ID, agents[0].Token, collisionTask)
	if collResp.Code != http.StatusBadRequest {
		t.Fatalf("task with reserved group: prefix status = %d, want 400; body=%s", collResp.Code, collResp.Body.String())
	}

	// Security: Publish announcement without valid operator token must be rejected
	unauthAnnounce := doJSONWithBearer(t, handler, http.MethodPost, "/hub/v1/admin/announcements", "invalid-token", map[string]any{
		"title": "Unauthorized", "summary": "Should fail", "severity": "CRITICAL",
	})
	if unauthAnnounce.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized publish announcement status = %d, want 401; body=%s", unauthAnnounce.Code, unauthAnnounce.Body.String())
	}

	// Admin: List messages requires operator token
	unauthMessages := doJSONWithBearer(t, handler, http.MethodGet, "/hub/v1/admin/messages", "invalid-token", nil)
	if unauthMessages.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized admin messages status = %d, want 401; body=%s", unauthMessages.Code, unauthMessages.Body.String())
	}

	// Admin: List messages with valid operator token returns direct tasks
	adminMessages := doJSONWithBearer(t, handler, http.MethodGet, "/hub/v1/admin/messages?type=direct&limit=10", "operator-fixture", nil)
	if adminMessages.Code != http.StatusOK || !strings.Contains(adminMessages.Body.String(), "hello from codex") {
		t.Fatalf("admin messages status/body = %d/%s", adminMessages.Code, adminMessages.Body.String())
	}

	// Admin: List agents with valid operator token
	adminAgents := doJSONWithBearer(t, handler, http.MethodGet, "/hub/v1/admin/agents", "operator-fixture", nil)
	if adminAgents.Code != http.StatusOK || !strings.Contains(adminAgents.Body.String(), "codex") {
		t.Fatalf("admin list agents status/body = %d/%s", adminAgents.Code, adminAgents.Body.String())
	}
	var agentListResp struct {
		Agents       []hub.AgentAdminDetail `json:"agents"`
		Total        int                    `json:"total"`
		OnlineCount  int                    `json:"onlineCount"`
		OfflineCount int                    `json:"offlineCount"`
	}
	decodeResponse(t, adminAgents, &agentListResp)
	if agentListResp.Total != 3 {
		t.Fatalf("agent count = %d, want 3", agentListResp.Total)
	}

	// Admin UI: /admin, /admin/messages, and /admin/agents should serve html
	adminUI := doJSON(t, handler, http.MethodGet, "/admin", "", "", nil)
	if adminUI.Code != http.StatusOK || !strings.Contains(adminUI.Body.String(), "888a2a-lite 管理後台") {
		t.Fatalf("admin UI status/body = %d/%s", adminUI.Code, adminUI.Body.String())
	}
	adminMsgUI := doJSON(t, handler, http.MethodGet, "/admin/messages", "", "", nil)
	if adminMsgUI.Code != http.StatusOK || !strings.Contains(adminMsgUI.Body.String(), "A2A 訊息監控") {
		t.Fatalf("admin msg UI status/body = %d/%s", adminMsgUI.Code, adminMsgUI.Body.String())
	}
	adminAgentsUI := doJSON(t, handler, http.MethodGet, "/admin/agents", "", "", nil)
	if adminAgentsUI.Code != http.StatusOK || !strings.Contains(adminAgentsUI.Body.String(), "Agent 管理與在線監控") {
		t.Fatalf("admin agents UI status/body = %d/%s", adminAgentsUI.Code, adminAgentsUI.Body.String())
	}

	// Admin: Delete an agent
	delResp := doJSONWithBearer(t, handler, http.MethodDelete, "/hub/v1/admin/agents/"+agents[2].ID, "operator-fixture", nil)
	if delResp.Code != http.StatusOK || !strings.Contains(delResp.Body.String(), "DELETED") {
		t.Fatalf("delete agent status/body = %d/%s", delResp.Code, delResp.Body.String())
	}

	// Verify count is now 2
	adminAgentsAfterDel := doJSONWithBearer(t, handler, http.MethodGet, "/hub/v1/admin/agents", "operator-fixture", nil)
	decodeResponse(t, adminAgentsAfterDel, &agentListResp)
	if agentListResp.Total != 2 {
		t.Fatalf("agent count after delete = %d, want 2", agentListResp.Total)
	}

	// Admin: Prune agents
	pruneResp := doJSONWithBearer(t, handler, http.MethodPost, "/hub/v1/admin/agents/prune", "operator-fixture", nil)
	if pruneResp.Code != http.StatusOK {
		t.Fatalf("prune agents status/body = %d/%s", pruneResp.Code, pruneResp.Body.String())
	}
}

type registeredTestAgent struct {
	ID    string
	Token string
}

func doJSON(t *testing.T, handler http.Handler, method, path, agentID, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var payload *bytes.Reader
	if body == nil {
		payload = bytes.NewReader(nil)
	} else {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		payload = bytes.NewReader(encoded)
	}
	request := httptest.NewRequest(method, path, payload)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if agentID != "" {
		request.Header.Set("X-Agent-ID", agentID)
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func doJSONWithBearer(t *testing.T, handler http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	return doJSON(t, handler, method, path, "", token, body)
}

func decodeResponse(t *testing.T, response *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, response.Body.String())
	}
}

func itoa(value uint64) string {
	return strconv.FormatUint(value, 10)
}

func TestHTTPSemiOpenSharedKeyEnforcement(t *testing.T) {
	ctx := context.Background()
	database, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer func() {
		_ = database.Close()
	}()
	repository := sqlite.NewRepository(database)
	sharedKey := "semi-open-secret-key-123"
	cfg := config.Config{
		HubID:                 "public",
		ListenAddr:            ":0",
		DatabasePath:          filepath.Join(t.TempDir(), "unused.db"),
		PublicBaseURL:         "https://hub.example",
		RegistrationEnabled:   true,
		RegistrationTTL:       24 * time.Hour,
		PeerLease:             90 * time.Second,
		MaxRegisteredAgents:   10,
		MaxTasksPerMinute:     20,
		MaxConcurrentTasks:    4,
		MaxPayloadBytes:       1 << 20,
		RegistrationPerMinute: 20,
		OperatorToken:         "operator-fixture",
		SharedKey:             sharedKey,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("config.Validate: %v", err)
	}
	_ = repository.Policy().SavePolicy(ctx, hub.HubPolicy{
		HubID:               cfg.HubID,
		RegistrationEnabled: cfg.RegistrationEnabled,
		RegistrationTTL:     cfg.RegistrationTTL,
		PeerLease:           cfg.PeerLease,
		MaxRegisteredAgents: cfg.MaxRegisteredAgents,
		MaxTasksPerMinute:   cfg.MaxTasksPerMinute,
		MaxConcurrentTasks:  cfg.MaxConcurrentTasks,
		MaxPayloadBytes:     cfg.MaxPayloadBytes,
	})
	srv := New(repository, cfg)
	handler := NewHTTPServer(srv).Handler()

	// 1. Health and llms.txt should work publicly without shared key
	healthReq := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	healthRec := httptest.NewRecorder()
	handler.ServeHTTP(healthRec, healthReq)
	if healthRec.Code != http.StatusOK {
		t.Fatalf("health status = %d", healthRec.Code)
	}

	llmsReq := httptest.NewRequest(http.MethodGet, "/llms.txt", nil)
	llmsRec := httptest.NewRecorder()
	handler.ServeHTTP(llmsRec, llmsReq)
	if llmsRec.Code != http.StatusOK {
		t.Fatalf("llms status = %d", llmsRec.Code)
	}

	// 2. System card and status should report mode SEMI_OPEN
	cardReq := httptest.NewRequest(http.MethodGet, "/hub/v1/system-card.json", nil)
	cardRec := httptest.NewRecorder()
	handler.ServeHTTP(cardRec, cardReq)
	if cardRec.Code != http.StatusOK {
		t.Fatalf("system-card status = %d", cardRec.Code)
	}
	var card hub.HubSystemCard
	decodeResponse(t, cardRec, &card)
	if card.Mode != "SEMI_OPEN" {
		t.Fatalf("system-card mode = %q, want SEMI_OPEN", card.Mode)
	}

	statusReq := httptest.NewRequest(http.MethodGet, "/hub/v1/status", nil)
	statusRec := httptest.NewRecorder()
	handler.ServeHTTP(statusRec, statusReq)
	if statusRec.Code != http.StatusOK {
		t.Fatalf("status status = %d", statusRec.Code)
	}
	var status HubStatus
	decodeResponse(t, statusRec, &status)
	if status.Mode != "SEMI_OPEN" {
		t.Fatalf("status mode = %q, want SEMI_OPEN", status.Mode)
	}

	// 3. Register without shared key should fail with 401 UNAUTHENTICATED
	regBody := map[string]any{
		"displayName":                "openclaw",
		"providerFamily":             "openclaw",
		"transportId":                "http-json",
		"capabilities":               []string{"text/plain"},
		"registrationIdempotencyKey": "test-key-1",
	}
	regPayload, _ := json.Marshal(regBody)

	unauthReq := httptest.NewRequest(http.MethodPost, "/hub/v1/agents/register", bytes.NewReader(regPayload))
	unauthReq.Header.Set("Content-Type", "application/json")
	unauthRec := httptest.NewRecorder()
	handler.ServeHTTP(unauthRec, unauthReq)
	if unauthRec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth register status = %d, want 401", unauthRec.Code)
	}

	// 4. Register with invalid shared key should fail with 401
	badKeyReq := httptest.NewRequest(http.MethodPost, "/hub/v1/agents/register", bytes.NewReader(regPayload))
	badKeyReq.Header.Set("Content-Type", "application/json")
	badKeyReq.Header.Set("X-Hub-Key", "wrong-key")
	badKeyRec := httptest.NewRecorder()
	handler.ServeHTTP(badKeyRec, badKeyReq)
	if badKeyRec.Code != http.StatusUnauthorized {
		t.Fatalf("bad key register status = %d, want 401", badKeyRec.Code)
	}

	// 5. Register with valid X-Hub-Key header should succeed with 201
	validKeyReq := httptest.NewRequest(http.MethodPost, "/hub/v1/agents/register", bytes.NewReader(regPayload))
	validKeyReq.Header.Set("Content-Type", "application/json")
	validKeyReq.Header.Set("X-Hub-Key", sharedKey)
	validKeyRec := httptest.NewRecorder()
	handler.ServeHTTP(validKeyRec, validKeyReq)
	if validKeyRec.Code != http.StatusCreated {
		t.Fatalf("valid key register status = %d, want 201; body=%s", validKeyRec.Code, validKeyRec.Body.String())
	}
	var regResp struct {
		Identity hub.AgentIdentity `json:"identity"`
	}
	decodeResponse(t, validKeyRec, &regResp)
	agentID := regResp.Identity.AgentID
	agentToken := regResp.Identity.AgentToken

	// 6. Register with Bearer shared key should also succeed
	regBody2 := map[string]any{
		"displayName":                "codex",
		"providerFamily":             "codex",
		"transportId":                "http-json",
		"capabilities":               []string{"text/plain"},
		"registrationIdempotencyKey": "test-key-2",
	}
	regPayload2, _ := json.Marshal(regBody2)
	bearerKeyReq := httptest.NewRequest(http.MethodPost, "/hub/v1/agents/register", bytes.NewReader(regPayload2))
	bearerKeyReq.Header.Set("Content-Type", "application/json")
	bearerKeyReq.Header.Set("Authorization", "Bearer "+sharedKey)
	bearerKeyRec := httptest.NewRecorder()
	handler.ServeHTTP(bearerKeyRec, bearerKeyReq)
	if bearerKeyRec.Code != http.StatusCreated {
		t.Fatalf("bearer key register status = %d, want 201; body=%s", bearerKeyRec.Code, bearerKeyRec.Body.String())
	}

	// 7. Call agent API without X-Hub-Key (even with valid agent token) should fail with 401
	noKeyAgentReq := httptest.NewRequest(http.MethodGet, "/hub/v1/agents", nil)
	noKeyAgentReq.Header.Set("X-Agent-ID", agentID)
	noKeyAgentReq.Header.Set("Authorization", "Bearer "+agentToken)
	noKeyAgentRec := httptest.NewRecorder()
	handler.ServeHTTP(noKeyAgentRec, noKeyAgentReq)
	if noKeyAgentRec.Code != http.StatusUnauthorized {
		t.Fatalf("no key agent list status = %d, want 401", noKeyAgentRec.Code)
	}

	// 8. Call agent API with valid X-Hub-Key and valid agent token should succeed with 200
	withKeyAgentReq := httptest.NewRequest(http.MethodGet, "/hub/v1/agents", nil)
	withKeyAgentReq.Header.Set("X-Agent-ID", agentID)
	withKeyAgentReq.Header.Set("Authorization", "Bearer "+agentToken)
	withKeyAgentReq.Header.Set("X-Hub-Key", sharedKey)
	withKeyAgentRec := httptest.NewRecorder()
	handler.ServeHTTP(withKeyAgentRec, withKeyAgentReq)
	if withKeyAgentRec.Code != http.StatusOK {
		t.Fatalf("with key agent list status = %d, want 200; body=%s", withKeyAgentRec.Code, withKeyAgentRec.Body.String())
	}

	// 9. Call agent API with query param ?hubKey=... should succeed with 200
	queryKeyAgentReq := httptest.NewRequest(http.MethodGet, "/hub/v1/agents?hubKey="+sharedKey, nil)
	queryKeyAgentReq.Header.Set("X-Agent-ID", agentID)
	queryKeyAgentReq.Header.Set("Authorization", "Bearer "+agentToken)
	queryKeyAgentRec := httptest.NewRecorder()
	handler.ServeHTTP(queryKeyAgentRec, queryKeyAgentReq)
	if queryKeyAgentRec.Code != http.StatusOK {
		t.Fatalf("query key agent list status = %d, want 200; body=%s", queryKeyAgentRec.Code, queryKeyAgentRec.Body.String())
	}
}
