package httpclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tbdavid2019/888a2a-lite/internal/hub"
)

func TestRegisterRetriesTransientServerFailure(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			http.Error(w, "temporary failure", http.StatusInternalServerError)
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/hub/v1/agents/register" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(RegisterResponse{Identity: hub.AgentIdentity{
			HubID: "public", AgentID: "agent-1", AgentToken: "token-1",
		}})
	}))
	defer server.Close()

	client, err := New(server.URL)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	client.MaxRetries = 1
	client.RetryDelay = time.Millisecond
	response, err := client.Register(context.Background(), hub.AgentDeclaration{
		DisplayName: "test", ProviderFamily: "test", TransportID: "http-json", RegistrationIdempotency: "install-1",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if response.Identity.AgentID != "agent-1" || client.AgentToken != "token-1" || requests.Load() != 2 {
		t.Fatalf("response/client/requests = %+v/%+v/%d", response, client, requests.Load())
	}
}

func TestReconnectRequiresExistingCredentials(t *testing.T) {
	client, err := New("https://hub.example")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := client.Reconnect(context.Background()); err == nil {
		t.Fatal("Reconnect accepted missing credentials")
	}
}

func TestGroupClientSendsAuthenticatedMessageAndDecodesResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/hub/v1/groups/group-1/messages" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("X-Agent-ID") != "agent-1" || r.Header.Get("Authorization") != "Bearer agent-token" {
			t.Fatalf("missing agent authentication headers")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"deliveryOutcome": "QUEUED",
			"message":         hub.GroupMessage{ID: 7, GroupID: "group-1", Trust: "UNTRUSTED_DATA"},
		})
	}))
	defer server.Close()
	client, err := New(server.URL)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	client.AgentID = "agent-1"
	client.AgentToken = "agent-token"
	message, duplicate, err := client.SendGroupMessage(context.Background(), "group-1", hub.GroupMessageInput{
		ContextID: "ctx-1", IdempotencyKey: "message-1", Message: "hello",
	})
	if err != nil || duplicate || message.ID != 7 || message.Trust != "UNTRUSTED_DATA" {
		t.Fatalf("SendGroupMessage = %+v duplicate:%v err:%v", message, duplicate, err)
	}
}
