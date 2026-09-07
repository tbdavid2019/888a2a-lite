package service

import (
	"bufio"
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
	svc := New(repository, cfg)
	handler := NewHTTPServer(svc).Handler()

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

	// Static installer script and bridge download routes
	installResp := doJSON(t, handler, http.MethodGet, "/install.sh", "", "", nil)
	if installResp.Code != http.StatusOK || !strings.Contains(installResp.Body.String(), "Universal Agent Bridge Installer") {
		t.Fatalf("install.sh status/body = %d/%s", installResp.Code, installResp.Body.String())
	}
	if ct := installResp.Header().Get("Content-Type"); !strings.Contains(ct, "text/x-shellscript") {
		t.Fatalf("install.sh content-type = %s, want text/x-shellscript", ct)
	}

	bridgeResp := doJSON(t, handler, http.MethodGet, "/a2a_bridge.py", "", "", nil)
	if bridgeResp.Code != http.StatusOK || !strings.Contains(bridgeResp.Body.String(), "Universal Agent Bridge") {
		t.Fatalf("a2a_bridge.py status/body = %d/%s", bridgeResp.Code, bridgeResp.Body.String())
	}
	if ct := bridgeResp.Header().Get("Content-Type"); !strings.Contains(ct, "text/x-python") {
		t.Fatalf("a2a_bridge.py content-type = %s, want text/x-python", ct)
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

	// 7. Call agent API with valid agent token (standard adapter without X-Hub-Key) succeeds seamlessly
	standardAgentReq := httptest.NewRequest(http.MethodGet, "/hub/v1/agents", nil)
	standardAgentReq.Header.Set("X-Agent-ID", agentID)
	standardAgentReq.Header.Set("Authorization", "Bearer "+agentToken)
	standardAgentRec := httptest.NewRecorder()
	handler.ServeHTTP(standardAgentRec, standardAgentReq)
	if standardAgentRec.Code != http.StatusOK {
		t.Fatalf("standard agent list status = %d, want 200; body=%s", standardAgentRec.Code, standardAgentRec.Body.String())
	}

	// 8. Stranger calling agent API without valid token fails with 401
	strangerReq := httptest.NewRequest(http.MethodGet, "/hub/v1/agents", nil)
	strangerReq.Header.Set("X-Agent-ID", "stranger-agent")
	strangerReq.Header.Set("Authorization", "Bearer invalid-token")
	strangerRec := httptest.NewRecorder()
	handler.ServeHTTP(strangerRec, strangerReq)
	if strangerRec.Code != http.StatusUnauthorized {
		t.Fatalf("stranger agent list status = %d, want 401", strangerRec.Code)
	}

	// 9. Call agent API with valid agent token BUT explicitly bad X-Hub-Key fails with 401
	badKeyReq2 := httptest.NewRequest(http.MethodGet, "/hub/v1/agents", nil)
	badKeyReq2.Header.Set("X-Agent-ID", agentID)
	badKeyReq2.Header.Set("Authorization", "Bearer "+agentToken)
	badKeyReq2.Header.Set("X-Hub-Key", "wrong-key")
	badKeyRec2 := httptest.NewRecorder()
	handler.ServeHTTP(badKeyRec2, badKeyReq2)
	if badKeyRec2.Code != http.StatusUnauthorized {
		t.Fatalf("bad key agent list status = %d, want 401", badKeyRec2.Code)
	}

	// 10. Call agent API with valid X-Hub-Key and valid agent token succeeds with 200
	withKeyAgentReq := httptest.NewRequest(http.MethodGet, "/hub/v1/agents", nil)
	withKeyAgentReq.Header.Set("X-Agent-ID", agentID)
	withKeyAgentReq.Header.Set("Authorization", "Bearer "+agentToken)
	withKeyAgentReq.Header.Set("X-Hub-Key", sharedKey)
	withKeyAgentRec := httptest.NewRecorder()
	handler.ServeHTTP(withKeyAgentRec, withKeyAgentReq)
	if withKeyAgentRec.Code != http.StatusOK {
		t.Fatalf("with key agent list status = %d, want 200; body=%s", withKeyAgentRec.Code, withKeyAgentRec.Body.String())
	}

	// 11. Call agent API with query param ?hubKey=... succeeds with 200
	queryKeyAgentReq := httptest.NewRequest(http.MethodGet, "/hub/v1/agents?hubKey="+sharedKey, nil)
	queryKeyAgentReq.Header.Set("X-Agent-ID", agentID)
	queryKeyAgentReq.Header.Set("Authorization", "Bearer "+agentToken)
	queryKeyAgentRec := httptest.NewRecorder()
	handler.ServeHTTP(queryKeyAgentRec, queryKeyAgentReq)
	if queryKeyAgentRec.Code != http.StatusOK {
		t.Fatalf("query key agent list status = %d, want 200; body=%s", queryKeyAgentRec.Code, queryKeyAgentRec.Body.String())
	}
}

func TestHTTPMultiCircleIsolation(t *testing.T) {
	ctx := context.Background()
	database, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer func() { _ = database.Close() }()
	repository := sqlite.NewRepository(database)
	cfg := config.Config{
		HubID: "public", ListenAddr: ":0", DatabasePath: filepath.Join(t.TempDir(), "unused.db"),
		RegistrationEnabled: true, RegistrationTTL: 24 * time.Hour, PeerLease: 90 * time.Second,
		MaxRegisteredAgents: 20, MaxTasksPerMinute: 20, MaxConcurrentTasks: 4,
		MaxPayloadBytes: 1 << 20, RegistrationPerMinute: 20,
		OperatorToken: "operator-fixture", CircleMode: "multi",
		SharedKeys: "team-a:private-a,team-b:private-b", CircleDerivationSecret: "stable-hub-secret",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("config.Validate: %v", err)
	}
	_ = repository.Policy().SavePolicy(ctx, hub.HubPolicy{
		HubID: cfg.HubID, RegistrationEnabled: cfg.RegistrationEnabled,
		RegistrationTTL: cfg.RegistrationTTL, PeerLease: cfg.PeerLease,
		MaxRegisteredAgents: cfg.MaxRegisteredAgents, MaxTasksPerMinute: cfg.MaxTasksPerMinute,
		MaxConcurrentTasks: cfg.MaxConcurrentTasks, MaxPayloadBytes: cfg.MaxPayloadBytes,
	})
	svc := New(repository, cfg)
	handler := NewHTTPServer(svc).Handler()

	register := func(name, key, registrationKey string) hub.AgentIdentity {
		request := httptest.NewRequest(http.MethodPost, "/hub/v1/agents/register", bytes.NewReader([]byte(`{"displayName":"`+name+`","providerFamily":"test","transportId":"http","capabilities":["text/plain"],"registrationIdempotencyKey":"`+registrationKey+`"}`)))
		request.Header.Set("Content-Type", "application/json")
		if key != "" {
			request.Header.Set("X-Hub-Key", key)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusCreated {
			t.Fatalf("register %s status/body = %d/%s", name, response.Code, response.Body.String())
		}
		var body struct {
			Identity hub.AgentIdentity `json:"identity"`
		}
		decodeResponse(t, response, &body)
		return body.Identity
	}

	publicAgent := register("public", "", "public-install")
	privateA := register("private-a-1", "private-a", "public-install")
	privateA2 := register("private-a-2", "private-a", "private-a-2")
	privateB := register("private-b", "private-b", "private-b")
	if publicAgent.CircleID != "public" || privateA.CircleID == "public" || privateA.CircleID != privateA2.CircleID || privateA.CircleID == privateB.CircleID {
		t.Fatalf("circle assignment mismatch: public=%q privateA=%q privateA2=%q privateB=%q", publicAgent.CircleID, privateA.CircleID, privateA2.CircleID, privateB.CircleID)
	}

	publicPeers := doJSON(t, handler, http.MethodGet, "/hub/v1/agents", publicAgent.AgentID, publicAgent.AgentToken, nil)
	if publicPeers.Code != http.StatusOK || strings.Contains(publicPeers.Body.String(), privateA.AgentID) {
		t.Fatalf("public peers leaked private agent: %d/%s", publicPeers.Code, publicPeers.Body.String())
	}
	privatePeers := doJSON(t, handler, http.MethodGet, "/hub/v1/agents", privateA.AgentID, privateA.AgentToken, nil)
	if privatePeers.Code != http.StatusOK || strings.Contains(privatePeers.Body.String(), publicAgent.AgentID) || !strings.Contains(privatePeers.Body.String(), privateA2.AgentID) {
		t.Fatalf("private peers were not isolated: %d/%s", privatePeers.Code, privatePeers.Body.String())
	}

	crossLookup := doJSON(t, handler, http.MethodGet, "/hub/v1/agents/"+publicAgent.AgentID, privateA.AgentID, privateA.AgentToken, nil)
	if crossLookup.Code != http.StatusNotFound {
		t.Fatalf("cross-circle lookup status = %d, want 404", crossLookup.Code)
	}
	crossCard := doJSON(t, handler, http.MethodGet, "/hub/v1/agents/"+publicAgent.AgentID+"/agent-card.json", privateA.AgentID, privateA.AgentToken, nil)
	if crossCard.Code != http.StatusNotFound {
		t.Fatalf("cross-circle Agent Card status = %d, want 404", crossCard.Code)
	}
	crossTask := doJSON(t, handler, http.MethodPost, "/hub/v1/agents/"+privateA.AgentID+"/tasks", publicAgent.AgentID, publicAgent.AgentToken, map[string]string{
		"contextId": "cross", "idempotencyKey": "cross", "message": "must not cross", "taskId": "cross",
	})
	if crossTask.Code != http.StatusNotFound {
		t.Fatalf("cross-circle task status = %d, want 404", crossTask.Code)
	}

	intraTask := doJSON(t, handler, http.MethodPost, "/hub/v1/agents/"+privateA2.AgentID+"/tasks", privateA.AgentID, privateA.AgentToken, map[string]string{
		"contextId": "intra", "idempotencyKey": "intra", "message": "same circle", "taskId": "intra",
	})
	if intraTask.Code != http.StatusAccepted {
		t.Fatalf("same-circle task status/body = %d/%s", intraTask.Code, intraTask.Body.String())
	}
	intraInbox := doJSON(t, handler, http.MethodGet, "/hub/v1/agents/"+privateA2.AgentID+"/inbox?afterSequence=0", privateA2.AgentID, privateA2.AgentToken, nil)
	if intraInbox.Code != http.StatusOK || !strings.Contains(intraInbox.Body.String(), "same circle") {
		t.Fatalf("same-circle inbox status/body = %d/%s", intraInbox.Code, intraInbox.Body.String())
	}
	duplicatePrivate := registerRequest(t, handler, "private-a-duplicate", "private-a", "public-install")
	if duplicatePrivate.Code != http.StatusCreated || !strings.Contains(duplicatePrivate.Body.String(), `"duplicate":true`) || strings.Contains(duplicatePrivate.Body.String(), `"agentToken"`) {
		t.Fatalf("same-circle registration retry mismatch: %d/%s", duplicatePrivate.Code, duplicatePrivate.Body.String())
	}

	publicStatus := doJSON(t, handler, http.MethodGet, "/hub/v1/status", "", "", nil)
	var publicStatusBody HubStatus
	decodeResponse(t, publicStatus, &publicStatusBody)
	if publicStatus.Code != http.StatusOK || publicStatusBody.RegisteredAgents != 0 || publicStatusBody.PendingTasks != 0 {
		t.Fatalf("anonymous multi-circle status leaked global counts: %d/%+v", publicStatus.Code, publicStatusBody)
	}
	agentStatus := doJSON(t, handler, http.MethodGet, "/hub/v1/status", privateA.AgentID, privateA.AgentToken, nil)
	var agentStatusBody HubStatus
	decodeResponse(t, agentStatus, &agentStatusBody)
	if agentStatus.Code != http.StatusOK || agentStatusBody.CircleID != privateA.CircleID || agentStatusBody.RegisteredAgents != 2 {
		t.Fatalf("agent-scoped status mismatch: %d/%+v", agentStatus.Code, agentStatusBody)
	}
	operatorStatus := doJSONWithBearer(t, handler, http.MethodGet, "/hub/v1/status", "operator-fixture", nil)
	var operatorStatusBody HubStatus
	decodeResponse(t, operatorStatus, &operatorStatusBody)
	if operatorStatus.Code != http.StatusOK || operatorStatusBody.RegisteredAgents != 4 {
		t.Fatalf("operator global status mismatch: %d/%+v", operatorStatus.Code, operatorStatusBody)
	}

	adminCircles := doJSONWithBearer(t, handler, http.MethodGet, "/hub/v1/admin/circles", "operator-fixture", nil)
	if adminCircles.Code != http.StatusOK || !strings.Contains(adminCircles.Body.String(), privateA.CircleID) || strings.Contains(adminCircles.Body.String(), "private-a") {
		t.Fatalf("admin circle response leaked alias/key or omitted circle: %d/%s", adminCircles.Code, adminCircles.Body.String())
	}
	adminMessages := doJSONWithBearer(t, handler, http.MethodGet, "/hub/v1/admin/messages?type=direct&circleId="+privateA.CircleID, "operator-fixture", nil)
	if adminMessages.Code != http.StatusOK || !strings.Contains(adminMessages.Body.String(), "same circle") {
		t.Fatalf("admin circle message filter mismatch: %d/%s", adminMessages.Code, adminMessages.Body.String())
	}

	groupResponse := doJSON(t, handler, http.MethodPost, "/hub/v1/groups", privateA.AgentID, privateA.AgentToken, map[string]string{"name": "private coordination"})
	if groupResponse.Code != http.StatusCreated {
		t.Fatalf("private group creation status/body = %d/%s", groupResponse.Code, groupResponse.Body.String())
	}
	var privateGroup hub.Group
	decodeResponse(t, groupResponse, &privateGroup)
	crossInvite := doJSON(t, handler, http.MethodPost, "/hub/v1/groups/"+privateGroup.GroupID+"/invitations", privateA.AgentID, privateA.AgentToken, map[string]string{"agentId": publicAgent.AgentID})
	if crossInvite.Code != http.StatusNotFound {
		t.Fatalf("cross-circle group invitation status = %d, want 404", crossInvite.Code)
	}
	crossGroup := doJSON(t, handler, http.MethodGet, "/hub/v1/groups/"+privateGroup.GroupID, publicAgent.AgentID, publicAgent.AgentToken, nil)
	if crossGroup.Code != http.StatusNotFound {
		t.Fatalf("cross-circle group lookup status = %d, want 404", crossGroup.Code)
	}

	if _, err := svc.RotateCircleKey(ctx, "operator-fixture", privateA.CircleID, "private-a-rotated", 0); err != nil {
		t.Fatalf("rotate private circle key: %v", err)
	}
	rotated := register("private-a-rotated", "private-a-rotated", "private-a-rotated")
	if rotated.CircleID != privateA.CircleID {
		t.Fatalf("rotated key entered a different circle: %q != %q", rotated.CircleID, privateA.CircleID)
	}
	oldKey := registerRequest(t, handler, "private-a-old", "private-a", "private-a-old")
	if oldKey.Code != http.StatusUnauthorized {
		t.Fatalf("revoked old key status = %d, want 401", oldKey.Code)
	}
	if _, err := svc.DisableCircle(ctx, "operator-fixture", privateA.CircleID); err != nil {
		t.Fatalf("disable private circle: %v", err)
	}
	disabledAgent := doJSON(t, handler, http.MethodGet, "/hub/v1/agents", privateA.AgentID, privateA.AgentToken, nil)
	if disabledAgent.Code != http.StatusUnauthorized {
		t.Fatalf("disabled circle agent status = %d, want 401", disabledAgent.Code)
	}
}

func registerRequest(t *testing.T, handler http.Handler, name, key, registrationKey string) *httptest.ResponseRecorder {
	t.Helper()
	payload := map[string]any{
		"displayName": name, "providerFamily": "test", "transportId": "http",
		"capabilities": []string{"text/plain"}, "registrationIdempotencyKey": registrationKey,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal registration: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/hub/v1/agents/register", bytes.NewReader(encoded))
	request.Header.Set("Content-Type", "application/json")
	if key != "" {
		request.Header.Set("X-Hub-Key", key)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestMultiCirclePersistsAcrossHubRestart(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "hub.db")
	cfg := config.Config{
		HubID: "public", ListenAddr: ":0", DatabasePath: databasePath,
		RegistrationEnabled: true, RegistrationTTL: 24 * time.Hour, PeerLease: 90 * time.Second,
		MaxRegisteredAgents: 10, MaxTasksPerMinute: 20, MaxConcurrentTasks: 4,
		MaxPayloadBytes: 1 << 20, RegistrationPerMinute: 20,
		OperatorToken: "operator-fixture", CircleMode: "multi",
		SharedKeys: "team-a:private-a", CircleDerivationSecret: "stable-hub-secret",
	}
	database, err := sqlite.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	repository := sqlite.NewRepository(database)
	service := New(repository, cfg)
	identity, _, err := service.RegisterWithSharedKey(ctx, hub.AgentDeclaration{
		DisplayName: "persistent-private", ProviderFamily: "test", TransportID: "http",
		Capabilities: []string{"text/plain"}, RegistrationIdempotency: "persistent-install",
	}, "private-a")
	if err != nil {
		t.Fatalf("register private agent: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close first database: %v", err)
	}

	database, err = sqlite.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("reopen sqlite: %v", err)
	}
	defer func() { _ = database.Close() }()
	restarted := New(sqlite.NewRepository(database), cfg)
	agent, err := restarted.AuthenticateAgent(ctx, identity.AgentID, identity.AgentToken)
	if err != nil {
		t.Fatalf("authenticate after restart: %v", err)
	}
	if agent.CircleID != identity.CircleID || identity.CircleID == "public" {
		t.Fatalf("circle identity changed after restart: before=%q after=%q", identity.CircleID, agent.CircleID)
	}
}

func TestHTTPSSEInboxStream(t *testing.T) {
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
		HubID: "public", ListenAddr: ":0", DatabasePath: filepath.Join(t.TempDir(), "unused.db"),
		RegistrationEnabled: true, RegistrationTTL: 24 * time.Hour, PeerLease: 90 * time.Second,
		MaxRegisteredAgents: 10, MaxTasksPerMinute: 20, MaxConcurrentTasks: 4,
		MaxPayloadBytes: 1 << 20, RegistrationPerMinute: 20,
		CircleMode: "multi", CircleDerivationSecret: "sse-test-derivation-secret",
	}
	svc := New(repository, cfg)
	ts := httptest.NewServer(NewHTTPServer(svc).Handler())
	defer ts.Close()

	// Register sender (agentA) and recipient (agentB)
	identA, _, err := svc.Register(ctx, hub.AgentDeclaration{
		DisplayName: "sender", ProviderFamily: "test", TransportID: "http",
		Capabilities: []string{"text/plain"}, RegistrationIdempotency: "sender-inst",
	})
	if err != nil {
		t.Fatalf("register A: %v", err)
	}
	identB, _, err := svc.Register(ctx, hub.AgentDeclaration{
		DisplayName: "recipient", ProviderFamily: "test", TransportID: "http",
		Capabilities: []string{"text/plain"}, RegistrationIdempotency: "recipient-inst",
	})
	if err != nil {
		t.Fatalf("register B: %v", err)
	}

	// 1. Send Task 1 before stream opens (tests catch-up)
	_, _, err = svc.SendTask(ctx, identA.AgentID, identA.AgentToken, hub.TaskDelivery{
		TargetAgentID: identB.AgentID, ContextID: "ctx-1", IdempotencyKey: "idem-1",
		Message: "task 1 message", TaskID: "task-1",
	})
	if err != nil {
		t.Fatalf("send task 1: %v", err)
	}

	// 2. Open SSE stream for agentB
	streamReq, err := http.NewRequest(http.MethodGet, ts.URL+"/hub/v1/agents/"+identB.AgentID+"/inbox/stream", nil)
	if err != nil {
		t.Fatalf("new stream req: %v", err)
	}
	streamReq.Header.Set("X-Agent-ID", identB.AgentID)
	streamReq.Header.Set("Authorization", "Bearer "+identB.AgentToken)

	client := &http.Client{}
	resp, err := client.Do(streamReq)
	if err != nil {
		t.Fatalf("stream request: %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stream status = %d, want 200", resp.StatusCode)
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", resp.Header.Get("Content-Type"))
	}

	reader := bufio.NewReader(resp.Body)

	readEvent := func() (string, string, string) {
		var id, event, data string
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatalf("read line: %v", err)
			}
			line = strings.TrimRight(line, "\r\n")
			if line == "" {
				if event != "" || data != "" {
					return id, event, data
				}
				continue
			}
			if strings.HasPrefix(line, ":") {
				continue
			}
			if strings.HasPrefix(line, "id: ") {
				id = strings.TrimPrefix(line, "id: ")
			} else if strings.HasPrefix(line, "event: ") {
				event = strings.TrimPrefix(line, "event: ")
			} else if strings.HasPrefix(line, "data: ") {
				data = strings.TrimPrefix(line, "data: ")
			}
		}
	}

	id1, event1, data1 := readEvent()
	if id1 != "1" || event1 != "task" || !strings.Contains(data1, "task 1 message") {
		t.Fatalf("event 1 mismatch: id=%q, event=%q, data=%q", id1, event1, data1)
	}

	// 3. Send Task 2 while stream is open (tests instant live push)
	go func() {
		time.Sleep(50 * time.Millisecond)
		_, _, sendErr := svc.SendTask(context.Background(), identA.AgentID, identA.AgentToken, hub.TaskDelivery{
			TargetAgentID: identB.AgentID, ContextID: "ctx-2", IdempotencyKey: "idem-2",
			Message: "task 2 message", TaskID: "task-2",
		})
		if sendErr != nil {
			t.Errorf("send task 2: %v", sendErr)
		}
	}()

	id2, event2, data2 := readEvent()
	if id2 != "2" || event2 != "task" || !strings.Contains(data2, "task 2 message") {
		t.Fatalf("event 2 mismatch: id=%q, event=%q, data=%q", id2, event2, data2)
	}
}
