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
	defer database.Close()
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
