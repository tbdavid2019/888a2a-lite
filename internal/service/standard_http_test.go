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

func TestStandardGatewayUsesBearerTenantAndSeparateEnvelopes(t *testing.T) {
	ctx := context.Background()
	database, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer func() { _ = database.Close() }()
	cfg := config.Config{HubID: "public", ListenAddr: ":0", DatabasePath: filepath.Join(t.TempDir(), "unused.db"), PublicBaseURL: "https://hub.example", RegistrationEnabled: true, RegistrationTTL: time.Hour, PeerLease: time.Minute, MaxRegisteredAgents: 10, MaxTasksPerMinute: 20, MaxConcurrentTasks: 4, MaxPayloadBytes: 1 << 20, RegistrationPerMinute: 20}
	svc := New(sqlite.NewRepository(database), cfg)
	handler := NewHTTPServer(svc).Handler()
	sender, _, err := svc.Register(ctx, hub.AgentDeclaration{DisplayName: "sender", ProviderFamily: "test", TransportID: "http-json", Capabilities: []string{"text/plain"}, RegistrationIdempotency: "sender"})
	if err != nil { t.Fatalf("register sender: %v", err) }
	target, _, err := svc.Register(ctx, hub.AgentDeclaration{DisplayName: "executor", ProviderFamily: "test", TransportID: "http-json", Capabilities: []string{"text/plain", a2a.ExecutionCapability}, RegistrationIdempotency: "target"})
	if err != nil { t.Fatalf("register target: %v", err) }

	card := doStandardRequest(t, handler, http.MethodGet, "/.well-known/agent-card.json", "", nil)
	if card.Code != http.StatusOK || !strings.Contains(card.Body.String(), `"protocolBinding":"HTTP+JSON"`) { t.Fatalf("gateway card = %d/%s", card.Code, card.Body.String()) }
	perAgentCard := doStandardRequest(t, handler, http.MethodGet, "/a2a/v1/agents/"+target.AgentID+"/card", target.AgentToken, nil)
	if perAgentCard.Code != http.StatusOK || !strings.Contains(perAgentCard.Body.String(), `"tenant":"`+target.AgentID+`"`) || perAgentCard.Header().Get("Cache-Control") != "private, no-store" { t.Fatalf("per-agent card = %d/%s", perAgentCard.Code, perAgentCard.Body.String()) }

	message := map[string]any{"tenant": target.AgentID, "message": map[string]any{"messageId": "message-1", "role": "ROLE_USER", "parts": []any{map[string]any{"text": "hello"}}}, "configuration": map[string]any{"returnImmediately": true}}
	sent := doStandardRequest(t, handler, http.MethodPost, "/a2a/v1/message:send", sender.AgentToken, message)
	if sent.Code != http.StatusOK || !strings.Contains(sent.Body.String(), `"task"`) || !strings.Contains(sent.Body.String(), string(a2a.TaskStateSubmitted)) { t.Fatalf("standard send = %d/%s", sent.Code, sent.Body.String()) }
	var sentResponse a2a.SendMessageResponse
	if err := json.NewDecoder(sent.Body).Decode(&sentResponse); err != nil { t.Fatalf("decode send: %v", err) }
	if sentResponse.Task == nil || sentResponse.Task.ID == "" { t.Fatalf("send response = %+v", sentResponse) }

	got := doStandardRequest(t, handler, http.MethodGet, "/a2a/v1/tasks/"+sentResponse.Task.ID, sender.AgentToken, nil)
	if got.Code != http.StatusOK || strings.Contains(got.Body.String(), sender.AgentToken) { t.Fatalf("get task = %d/%s", got.Code, got.Body.String()) }
	listed := doStandardRequest(t, handler, http.MethodGet, "/a2a/v1/tasks?pageSize=1", sender.AgentToken, nil)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"tasks"`) || !strings.Contains(listed.Body.String(), `"pageSize":1`) { t.Fatalf("list tasks = %d/%s", listed.Code, listed.Body.String()) }

	wrongCaller := doStandardRequest(t, handler, http.MethodGet, "/a2a/v1/tasks/"+sentResponse.Task.ID, target.AgentToken, nil)
	if wrongCaller.Code != http.StatusNotFound || !strings.Contains(wrongCaller.Body.String(), `"reason":"TASK_NOT_FOUND"`) { t.Fatalf("wrong caller task access = %d/%s", wrongCaller.Code, wrongCaller.Body.String()) }
	unsupported := map[string]any{"tenant": target.AgentID, "message": map[string]any{"messageId": "message-file", "role": "ROLE_USER", "parts": []any{map[string]any{"text": "hello"}, map[string]any{"url": "https://example.invalid/file"}}}, "configuration": map[string]any{"returnImmediately": true}}
	unsupportedResponse := doStandardRequest(t, handler, http.MethodPost, "/message:send", sender.AgentToken, unsupported)
	if unsupportedResponse.Code != http.StatusBadRequest || !strings.Contains(unsupportedResponse.Body.String(), `"reason":"CONTENT_TYPE_NOT_SUPPORTED"`) { t.Fatalf("unsupported part = %d/%s", unsupportedResponse.Code, unsupportedResponse.Body.String()) }
}

func doStandardRequest(t *testing.T, handler http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body == nil { reader = bytes.NewReader(nil) } else { encoded, err := json.Marshal(body); if err != nil { t.Fatalf("marshal request: %v", err) }; reader = bytes.NewReader(encoded) }
	request := httptest.NewRequest(method, path, reader)
	if body != nil { request.Header.Set("Content-Type", a2a.MediaType) }
	if token != "" { request.Header.Set("Authorization", "Bearer "+token) }
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
