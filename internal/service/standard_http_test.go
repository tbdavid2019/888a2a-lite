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
	cfg := config.Config{HubID: "public", ListenAddr: ":0", DatabasePath: filepath.Join(t.TempDir(), "unused.db"), PublicBaseURL: "https://hub.example", RegistrationEnabled: true, RegistrationTTL: time.Hour, PeerLease: time.Minute, MaxRegisteredAgents: 10, MaxTasksPerMinute: 20, MaxConcurrentTasks: 4, MaxPayloadBytes: 1 << 20, RegistrationPerMinute: 20, StandardWaitTimeout: 2 * time.Second, StandardGatewayEnabled: true}
	svc := New(sqlite.NewRepository(database), cfg)
	handler := NewHTTPServer(svc).Handler()
	sender, _, err := svc.Register(ctx, hub.AgentDeclaration{DisplayName: "sender", ProviderFamily: "test", TransportID: "http-json", Capabilities: []string{"text/plain"}, RegistrationIdempotency: "sender"})
	if err != nil {
		t.Fatalf("register sender: %v", err)
	}
	target, _, err := svc.Register(ctx, hub.AgentDeclaration{DisplayName: "executor", ProviderFamily: "test", TransportID: "http-json", Capabilities: []string{"text/plain", a2a.ExecutionCapability}, RegistrationIdempotency: "target"})
	if err != nil {
		t.Fatalf("register target: %v", err)
	}

	card := doStandardRequest(t, handler, http.MethodGet, "/.well-known/agent-card.json", "", nil)
	if card.Code != http.StatusOK || !strings.Contains(card.Body.String(), `"protocolBinding":"HTTP+JSON"`) {
		t.Fatalf("gateway card = %d/%s", card.Code, card.Body.String())
	}
	var gatewayCard a2a.AgentCard
	if err := json.NewDecoder(card.Body).Decode(&gatewayCard); err != nil {
		t.Fatalf("decode gateway card: %v", err)
	}
	hasMode := func(modes []string, expected string) bool {
		for _, mode := range modes {
			if mode == expected {
				return true
			}
		}
		return false
	}
	for _, required := range []string{"image/*", "application/zip"} {
		if !hasMode(gatewayCard.DefaultInputModes, required) || !hasMode(gatewayCard.DefaultOutputModes, required) {
			t.Errorf("gateway card does not advertise %q in both directions", required)
		}
	}
	if hasMode(gatewayCard.DefaultInputModes, "raw") || hasMode(gatewayCard.DefaultOutputModes, "raw") {
		t.Fatal("gateway card advertises unsupported raw Parts")
	}
	perAgentCard := doStandardRequest(t, handler, http.MethodGet, "/a2a/v1/agents/"+target.AgentID+"/card", target.AgentToken, nil)
	if perAgentCard.Code != http.StatusOK || !strings.Contains(perAgentCard.Body.String(), `"tenant":"`+target.AgentID+`"`) || perAgentCard.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("per-agent card = %d/%s", perAgentCard.Code, perAgentCard.Body.String())
	}

	attachmentURL := "https://box.david888.com/storage/asset.pdf?signature=short-lived"
	message := map[string]any{"tenant": target.AgentID, "message": map[string]any{"messageId": "message-1", "role": "ROLE_USER", "parts": []any{map[string]any{"text": "hello"}, map[string]any{"url": attachmentURL, "filename": "asset.pdf", "mediaType": "application/pdf"}}}, "configuration": map[string]any{"returnImmediately": true}}
	sent := doStandardRequest(t, handler, http.MethodPost, "/a2a/v1/message:send", sender.AgentToken, message)
	if sent.Code != http.StatusOK || !strings.Contains(sent.Body.String(), `"task"`) || !strings.Contains(sent.Body.String(), string(a2a.TaskStateSubmitted)) {
		t.Fatalf("standard send = %d/%s", sent.Code, sent.Body.String())
	}
	var sentResponse a2a.SendMessageResponse
	if err := json.NewDecoder(sent.Body).Decode(&sentResponse); err != nil {
		t.Fatalf("decode send: %v", err)
	}
	if sentResponse.Task == nil || sentResponse.Task.ID == "" {
		t.Fatalf("send response = %+v", sentResponse)
	}
	legacyInbox := doJSON(t, handler, http.MethodGet, "/hub/v1/agents/"+target.AgentID+"/inbox?afterSequence=0", target.AgentID, target.AgentToken, nil)
	if legacyInbox.Code != http.StatusOK || !strings.Contains(legacyInbox.Body.String(), `"protocol":"A2A/1.0"`) || !strings.Contains(legacyInbox.Body.String(), `"messageId":"message-1"`) {
		t.Fatalf("legacy inbox did not expose standard correlation = %d/%s", legacyInbox.Code, legacyInbox.Body.String())
	}
	var inboxBody struct {
		Items []hub.InboxItem `json:"items"`
	}
	decodeResponse(t, legacyInbox, &inboxBody)
	if len(inboxBody.Items) != 1 {
		t.Fatalf("standard inbox items = %+v", inboxBody.Items)
	}
	if len(inboxBody.Items[0].Parts) != 2 || inboxBody.Items[0].Parts[1].URL == nil || *inboxBody.Items[0].Parts[1].URL != attachmentURL {
		t.Fatalf("standard inbox parts = %+v", inboxBody.Items[0].Parts)
	}
	ack := doJSON(t, handler, http.MethodPost, "/hub/v1/agents/"+target.AgentID+"/inbox/"+itoa(inboxBody.Items[0].Sequence)+"/ack", target.AgentID, target.AgentToken, nil)
	if ack.Code != http.StatusOK {
		t.Fatalf("standard inbox ack = %d/%s", ack.Code, ack.Body.String())
	}
	invalidUpdate := map[string]any{"updateId": "update-invalid-artifact", "turnId": inboxBody.Items[0].TurnID, "expectedRevision": 2, "state": string(a2a.TaskStateCompleted), "artifacts": []any{map[string]any{"artifactId": "artifact-invalid", "parts": []any{map[string]any{"raw": "encoded", "mediaType": "application/pdf"}}}}}
	invalidUpdated := doStandardRequest(t, handler, http.MethodPost, "/hub/v1/a2a/tasks/"+sentResponse.Task.ID+"/updates", target.AgentToken, invalidUpdate)
	if invalidUpdated.Code != http.StatusBadRequest || !strings.Contains(invalidUpdated.Body.String(), `"reason":"CONTENT_TYPE_NOT_SUPPORTED"`) {
		t.Fatalf("invalid artifact update = %d/%s", invalidUpdated.Code, invalidUpdated.Body.String())
	}
	missingReplyID := map[string]any{"updateId": "update-missing-message-id", "turnId": inboxBody.Items[0].TurnID, "expectedRevision": 2, "state": string(a2a.TaskStateCompleted), "message": map[string]any{"role": "ROLE_AGENT", "parts": []any{map[string]any{"text": "done"}}}}
	missingReplyIDResponse := doStandardRequest(t, handler, http.MethodPost, "/hub/v1/a2a/tasks/"+sentResponse.Task.ID+"/updates", target.AgentToken, missingReplyID)
	if missingReplyIDResponse.Code != http.StatusBadRequest || !strings.Contains(missingReplyIDResponse.Body.String(), "messageId is required") {
		t.Fatalf("missing reply messageId = %d/%s", missingReplyIDResponse.Code, missingReplyIDResponse.Body.String())
	}
	mismatchedReply := map[string]any{"updateId": "update-mismatched-reply", "turnId": inboxBody.Items[0].TurnID, "expectedRevision": 2, "state": string(a2a.TaskStateCompleted), "message": map[string]any{"messageId": "reply-mismatch", "contextId": "wrong-context", "taskId": sentResponse.Task.ID, "role": "ROLE_AGENT", "parts": []any{map[string]any{"text": "done"}}}}
	mismatchedReplyResponse := doStandardRequest(t, handler, http.MethodPost, "/hub/v1/a2a/tasks/"+sentResponse.Task.ID+"/updates", target.AgentToken, mismatchedReply)
	if mismatchedReplyResponse.Code != http.StatusBadRequest || !strings.Contains(mismatchedReplyResponse.Body.String(), "must match the task") {
		t.Fatalf("mismatched reply correlation = %d/%s", mismatchedReplyResponse.Code, mismatchedReplyResponse.Body.String())
	}
	update := map[string]any{"updateId": "update-1", "turnId": inboxBody.Items[0].TurnID, "expectedRevision": 2, "state": string(a2a.TaskStateCompleted), "message": map[string]any{"messageId": "reply-1", "contextId": sentResponse.Task.ContextID, "taskId": sentResponse.Task.ID, "role": "ROLE_AGENT", "parts": []any{map[string]any{"text": "done"}}}, "artifacts": []any{map[string]any{"artifactId": "artifact-1", "name": "processed asset", "parts": []any{map[string]any{"url": "https://box.david888.com/storage/processed.pdf", "filename": "processed.pdf", "mediaType": "application/pdf"}}}}}
	updated := doStandardRequest(t, handler, http.MethodPost, "/hub/v1/a2a/tasks/"+sentResponse.Task.ID+"/updates", target.AgentToken, update)
	if updated.Code != http.StatusOK || !strings.Contains(updated.Body.String(), string(a2a.TaskStateCompleted)) || !strings.Contains(updated.Body.String(), "processed.pdf") {
		t.Fatalf("standard update = %d/%s", updated.Code, updated.Body.String())
	}
	duplicateUpdate := doStandardRequest(t, handler, http.MethodPost, "/hub/v1/a2a/tasks/"+sentResponse.Task.ID+"/updates", target.AgentToken, update)
	if duplicateUpdate.Code != http.StatusOK || !strings.Contains(duplicateUpdate.Body.String(), `"duplicate":true`) {
		t.Fatalf("duplicate standard update = %d/%s", duplicateUpdate.Code, duplicateUpdate.Body.String())
	}
	forgedUpdate := doStandardRequest(t, handler, http.MethodPost, "/hub/v1/a2a/tasks/"+sentResponse.Task.ID+"/updates", sender.AgentToken, update)
	if forgedUpdate.Code != http.StatusNotFound || !strings.Contains(forgedUpdate.Body.String(), `"reason":"TASK_NOT_FOUND"`) {
		t.Fatalf("forged standard update = %d/%s", forgedUpdate.Code, forgedUpdate.Body.String())
	}
	completed := doStandardRequest(t, handler, http.MethodGet, "/a2a/v1/tasks/"+sentResponse.Task.ID, sender.AgentToken, nil)
	if completed.Code != http.StatusOK || !strings.Contains(completed.Body.String(), string(a2a.TaskStateCompleted)) || !strings.Contains(completed.Body.String(), "reply-1") {
		t.Fatalf("completed standard task = %d/%s", completed.Code, completed.Body.String())
	}
	subscribed := doStandardRequest(t, handler, http.MethodPost, "/a2a/v1/tasks/"+sentResponse.Task.ID+":subscribe", sender.AgentToken, nil)
	if subscribed.Code != http.StatusOK || !strings.Contains(subscribed.Header().Get("Content-Type"), "text/event-stream") || !strings.Contains(subscribed.Body.String(), string(a2a.TaskStateCompleted)) {
		t.Fatalf("standard subscribe = %d/%s", subscribed.Code, subscribed.Body.String())
	}
	historyZero := doStandardRequest(t, handler, http.MethodGet, "/a2a/v1/tasks/"+sentResponse.Task.ID+"?historyLength=0", sender.AgentToken, nil)
	if historyZero.Code != http.StatusOK || strings.Contains(historyZero.Body.String(), `"history"`) {
		t.Fatalf("historyLength=0 task = %d/%s", historyZero.Code, historyZero.Body.String())
	}
	changedMessage := map[string]any{"tenant": target.AgentID, "message": map[string]any{"messageId": "message-1", "role": "ROLE_USER", "parts": []any{map[string]any{"text": "changed"}}}, "configuration": map[string]any{"returnImmediately": true}}
	changedResponse := doStandardRequest(t, handler, http.MethodPost, "/message:send", sender.AgentToken, changedMessage)
	if changedResponse.Code != http.StatusConflict || !strings.Contains(changedResponse.Body.String(), `"reason":"INVALID_ARGUMENT"`) {
		t.Fatalf("changed messageId = %d/%s", changedResponse.Code, changedResponse.Body.String())
	}
	terminalResume := map[string]any{"tenant": target.AgentID, "message": map[string]any{"messageId": "message-terminal-resume", "taskId": sentResponse.Task.ID, "contextId": "wrong-context", "role": "ROLE_USER", "parts": []any{map[string]any{"text": "resume"}}}, "configuration": map[string]any{"returnImmediately": true}}
	terminalResumeResponse := doStandardRequest(t, handler, http.MethodPost, "/message:send", sender.AgentToken, terminalResume)
	if terminalResumeResponse.Code != http.StatusBadRequest || !strings.Contains(terminalResumeResponse.Body.String(), `"reason":"INVALID_ARGUMENT"`) {
		t.Fatalf("terminal context mismatch = %d/%s", terminalResumeResponse.Code, terminalResumeResponse.Body.String())
	}
	cancelSend := doStandardRequest(t, handler, http.MethodPost, "/message:send", sender.AgentToken, map[string]any{"tenant": target.AgentID, "message": map[string]any{"messageId": "message-cancel", "role": "ROLE_USER", "parts": []any{map[string]any{"text": "cancel me"}}}, "configuration": map[string]any{"returnImmediately": true}})
	if cancelSend.Code != http.StatusOK {
		t.Fatalf("cancel task send = %d/%s", cancelSend.Code, cancelSend.Body.String())
	}
	var cancelEnvelope a2a.SendMessageResponse
	if err := json.NewDecoder(cancelSend.Body).Decode(&cancelEnvelope); err != nil || cancelEnvelope.Task == nil {
		t.Fatalf("cancel task envelope = %s", cancelSend.Body.String())
	}
	cancelResponse := doStandardRequest(t, handler, http.MethodPost, "/tasks/"+cancelEnvelope.Task.ID+":cancel", sender.AgentToken, map[string]any{})
	if cancelResponse.Code != http.StatusOK || !strings.Contains(cancelResponse.Body.String(), string(a2a.TaskStateCanceled)) {
		t.Fatalf("cancel task = %d/%s", cancelResponse.Code, cancelResponse.Body.String())
	}
	repeatedCancel := doStandardRequest(t, handler, http.MethodPost, "/tasks/"+cancelEnvelope.Task.ID+":cancel", sender.AgentToken, map[string]any{})
	if repeatedCancel.Code != http.StatusOK || !strings.Contains(repeatedCancel.Body.String(), string(a2a.TaskStateCanceled)) {
		t.Fatalf("repeated cancel = %d/%s", repeatedCancel.Code, repeatedCancel.Body.String())
	}
	lateAck := doJSON(t, handler, http.MethodPost, "/hub/v1/agents/"+target.AgentID+"/inbox/2/ack", target.AgentID, target.AgentToken, nil)
	if lateAck.Code != http.StatusConflict {
		t.Fatalf("late ACK after standard cancel = %d/%s", lateAck.Code, lateAck.Body.String())
	}

	got := doStandardRequest(t, handler, http.MethodGet, "/a2a/v1/tasks/"+sentResponse.Task.ID, sender.AgentToken, nil)
	if got.Code != http.StatusOK || strings.Contains(got.Body.String(), sender.AgentToken) {
		t.Fatalf("get task = %d/%s", got.Code, got.Body.String())
	}
	listed := doStandardRequest(t, handler, http.MethodGet, "/a2a/v1/tasks?pageSize=1", sender.AgentToken, nil)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"tasks"`) || !strings.Contains(listed.Body.String(), `"pageSize":1`) {
		t.Fatalf("list tasks = %d/%s", listed.Code, listed.Body.String())
	}

	wrongCaller := doStandardRequest(t, handler, http.MethodGet, "/a2a/v1/tasks/"+sentResponse.Task.ID, target.AgentToken, nil)
	if wrongCaller.Code != http.StatusNotFound || !strings.Contains(wrongCaller.Body.String(), `"reason":"TASK_NOT_FOUND"`) {
		t.Fatalf("wrong caller task access = %d/%s", wrongCaller.Code, wrongCaller.Body.String())
	}
	unsupported := map[string]any{"tenant": target.AgentID, "message": map[string]any{"messageId": "message-file", "role": "ROLE_USER", "parts": []any{map[string]any{"text": "hello"}, map[string]any{"url": "https://example.invalid/file"}}}, "configuration": map[string]any{"returnImmediately": true}}
	unsupportedResponse := doStandardRequest(t, handler, http.MethodPost, "/message:send", sender.AgentToken, unsupported)
	if unsupportedResponse.Code != http.StatusBadRequest || !strings.Contains(unsupportedResponse.Body.String(), `"reason":"CONTENT_TYPE_NOT_SUPPORTED"`) {
		t.Fatalf("unsupported part = %d/%s", unsupportedResponse.Code, unsupportedResponse.Body.String())
	}
}

func TestStandardGatewayMasksCrossCircleTargetsAndCards(t *testing.T) {
	ctx := context.Background()
	database, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer func() { _ = database.Close() }()
	cfg := config.Config{HubID: "public", ListenAddr: ":0", DatabasePath: filepath.Join(t.TempDir(), "unused.db"), PublicBaseURL: "https://hub.example", RegistrationEnabled: true, RegistrationTTL: time.Hour, PeerLease: time.Minute, MaxRegisteredAgents: 10, MaxTasksPerMinute: 20, MaxConcurrentTasks: 4, MaxPayloadBytes: 1 << 20, RegistrationPerMinute: 20, CircleMode: "multi", SharedKeys: "team-a:private-a", AllowDynamicCircles: true, CircleDerivationSecret: "stable-secret", StandardGatewayEnabled: true}
	svc := New(sqlite.NewRepository(database), cfg)
	handler := NewHTTPServer(svc).Handler()
	publicSender, _, err := svc.Register(ctx, hub.AgentDeclaration{DisplayName: "public-sender", ProviderFamily: "test", TransportID: "http-json", Capabilities: []string{"text/plain"}, RegistrationIdempotency: "public-sender"})
	if err != nil {
		t.Fatalf("register public sender: %v", err)
	}
	privateTarget, _, err := svc.RegisterWithSharedKey(ctx, hub.AgentDeclaration{DisplayName: "private-target", ProviderFamily: "test", TransportID: "http-json", Capabilities: []string{"text/plain", a2a.ExecutionCapability}, RegistrationIdempotency: "private-target"}, "private-a")
	if err != nil {
		t.Fatalf("register private target: %v", err)
	}
	body := map[string]any{"tenant": privateTarget.AgentID, "message": map[string]any{"messageId": "cross-circle", "role": "ROLE_USER", "parts": []any{map[string]any{"text": "must not cross"}}}, "configuration": map[string]any{"returnImmediately": true}}
	sent := doStandardRequest(t, handler, http.MethodPost, "/a2a/v1/message:send", publicSender.AgentToken, body)
	if sent.Code != http.StatusNotFound || !strings.Contains(sent.Body.String(), `"reason":"TASK_NOT_FOUND"`) {
		t.Fatalf("cross-circle send = %d/%s", sent.Code, sent.Body.String())
	}
	card := doStandardRequest(t, handler, http.MethodGet, "/a2a/v1/agents/"+privateTarget.AgentID+"/card", publicSender.AgentToken, nil)
	if card.Code != http.StatusNotFound || card.Header().Get("Cache-Control") != "private, no-store" || strings.Contains(card.Body.String(), privateTarget.AgentID) {
		t.Fatalf("cross-circle card = %d/%s", card.Code, card.Body.String())
	}
	listed := doStandardRequest(t, handler, http.MethodGet, "/a2a/v1/tasks", publicSender.AgentToken, nil)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"tasks":[]`) {
		t.Fatalf("cross-circle task state = %d/%s", listed.Code, listed.Body.String())
	}
	dynamicSender, _, err := svc.RegisterWithSharedKey(ctx, hub.AgentDeclaration{DisplayName: "dynamic-sender", ProviderFamily: "test", TransportID: "http-json", Capabilities: []string{"text/plain"}, RegistrationIdempotency: "dynamic-sender"}, "dynamic-key")
	if err != nil {
		t.Fatalf("register dynamic sender: %v", err)
	}
	dynamicTarget, _, err := svc.RegisterWithSharedKey(ctx, hub.AgentDeclaration{DisplayName: "dynamic-target", ProviderFamily: "test", TransportID: "http-json", Capabilities: []string{"text/plain", a2a.ExecutionCapability}, RegistrationIdempotency: "dynamic-target"}, "dynamic-key")
	if err != nil {
		t.Fatalf("register dynamic target: %v", err)
	}
	dynamicSend := doStandardRequest(t, handler, http.MethodPost, "/a2a/v1/"+dynamicTarget.AgentID+"/message:send", dynamicSender.AgentToken, map[string]any{"message": map[string]any{"messageId": "dynamic-message", "role": "ROLE_USER", "parts": []any{map[string]any{"text": "dynamic circle"}}}, "configuration": map[string]any{"returnImmediately": true}})
	if dynamicSend.Code != http.StatusOK || !strings.Contains(dynamicSend.Body.String(), string(a2a.TaskStateSubmitted)) {
		t.Fatalf("dynamic-circle send = %d/%s", dynamicSend.Code, dynamicSend.Body.String())
	}
	var dynamicEnvelope a2a.SendMessageResponse
	if err := json.NewDecoder(dynamicSend.Body).Decode(&dynamicEnvelope); err != nil || dynamicEnvelope.Task == nil {
		t.Fatalf("decode dynamic send: %v", err)
	}
	dynamicTaskID := dynamicEnvelope.Task.ID
	crossGet := doStandardRequest(t, handler, http.MethodGet, "/a2a/v1/tasks/"+dynamicTaskID, publicSender.AgentToken, nil)
	if crossGet.Code != http.StatusNotFound || !strings.Contains(crossGet.Body.String(), `"reason":"TASK_NOT_FOUND"`) {
		t.Fatalf("cross-circle get task = %d/%s", crossGet.Code, crossGet.Body.String())
	}
	crossCancel := doStandardRequest(t, handler, http.MethodPost, "/a2a/v1/tasks/"+dynamicTaskID+":cancel", publicSender.AgentToken, map[string]any{})
	if crossCancel.Code != http.StatusNotFound || !strings.Contains(crossCancel.Body.String(), `"reason":"TASK_NOT_FOUND"`) {
		t.Fatalf("cross-circle cancel task = %d/%s", crossCancel.Code, crossCancel.Body.String())
	}
	crossSubscribe := doStandardRequest(t, handler, http.MethodPost, "/a2a/v1/tasks/"+dynamicTaskID+":subscribe", publicSender.AgentToken, nil)
	if crossSubscribe.Code != http.StatusNotFound || !strings.Contains(crossSubscribe.Body.String(), `"reason":"TASK_NOT_FOUND"`) {
		t.Fatalf("cross-circle subscribe task = %d/%s", crossSubscribe.Code, crossSubscribe.Body.String())
	}
}

func TestStandardBlockingTimeoutPreservesTask(t *testing.T) {
	ctx := context.Background()
	database, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer func() { _ = database.Close() }()
	cfg := config.Config{HubID: "public", ListenAddr: ":0", DatabasePath: filepath.Join(t.TempDir(), "unused.db"), RegistrationEnabled: true, RegistrationTTL: time.Hour, PeerLease: time.Minute, MaxRegisteredAgents: 2, MaxTasksPerMinute: 20, MaxConcurrentTasks: 4, MaxPayloadBytes: 1 << 20, RegistrationPerMinute: 20, StandardWaitTimeout: 10 * time.Millisecond, StandardGatewayEnabled: true}
	svc := New(sqlite.NewRepository(database), cfg)
	handler := NewHTTPServer(svc).Handler()
	sender, _, err := svc.Register(ctx, hub.AgentDeclaration{DisplayName: "sender", ProviderFamily: "test", TransportID: "http-json", Capabilities: []string{"text/plain"}, RegistrationIdempotency: "sender"})
	if err != nil {
		t.Fatalf("register sender: %v", err)
	}
	target, _, err := svc.Register(ctx, hub.AgentDeclaration{DisplayName: "executor", ProviderFamily: "test", TransportID: "http-json", Capabilities: []string{"text/plain", a2a.ExecutionCapability}, RegistrationIdempotency: "target"})
	if err != nil {
		t.Fatalf("register target: %v", err)
	}
	body := map[string]any{"tenant": target.AgentID, "message": map[string]any{"messageId": "timeout-message", "role": "ROLE_USER", "parts": []any{map[string]any{"text": "wait"}}}}
	response := doStandardRequest(t, handler, http.MethodPost, "/message:send", sender.AgentToken, body)
	if response.Code != http.StatusGatewayTimeout || !strings.Contains(response.Body.String(), `"reason":"DEADLINE_EXCEEDED"`) {
		t.Fatalf("blocking timeout = %d/%s", response.Code, response.Body.String())
	}
	listed := doStandardRequest(t, handler, http.MethodGet, "/tasks", sender.AgentToken, nil)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), "timeout-message") {
		t.Fatalf("task after timeout = %d/%s", listed.Code, listed.Body.String())
	}
}

func TestStandardGatewayRejectsUnsupportedVersionWithGoogleRPCStatus(t *testing.T) {
	ctx := context.Background()
	database, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer func() { _ = database.Close() }()
	cfg := config.Config{HubID: "public", ListenAddr: ":0", DatabasePath: filepath.Join(t.TempDir(), "unused.db"), RegistrationEnabled: true, RegistrationTTL: time.Hour, PeerLease: time.Minute, MaxRegisteredAgents: 2, MaxTasksPerMinute: 20, MaxConcurrentTasks: 4, MaxPayloadBytes: 1 << 20, RegistrationPerMinute: 20, StandardGatewayEnabled: true}
	svc := New(sqlite.NewRepository(database), cfg)
	handler := NewHTTPServer(svc).Handler()
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/message:send", bytes.NewReader([]byte(`{}`)))
	request.Header.Set("Content-Type", a2a.MediaType)
	request.Header.Set("A2A-Version", "2.0")
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"reason":"VERSION_NOT_SUPPORTED"`) || !strings.Contains(response.Body.String(), `google.rpc.ErrorInfo`) {
		t.Fatalf("unsupported version = %d/%s", response.Code, response.Body.String())
	}
}

func doStandardRequest(t *testing.T, handler http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
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
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
