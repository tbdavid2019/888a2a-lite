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

func TestStandardGroupMultiCircleIsolationAndDynamicCircle(t *testing.T) {
	ctx := context.Background()
	database, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer func() { _ = database.Close() }()
	cfg := config.Config{HubID: "public", ListenAddr: ":0", DatabasePath: filepath.Join(t.TempDir(), "unused.db"), PublicBaseURL: "https://hub.example", RegistrationEnabled: true, RegistrationTTL: time.Hour, PeerLease: time.Minute, MaxRegisteredAgents: 20, MaxTasksPerMinute: 100, MaxConcurrentTasks: 4, MaxPayloadBytes: 1 << 20, MaxGroupMembers: 4, MaxGroupFanout: 4, MaxGroupHistoryPage: 10, RegistrationPerMinute: 50, CircleMode: "multi", SharedKeys: "team-a:private-a", AllowDynamicCircles: true, CircleDerivationSecret: "stable-secret", StandardGatewayEnabled: true, GroupExtensionEnabled: true}
	svc := New(sqlite.NewRepository(database), cfg)
	handler := NewHTTPServer(svc).Handler()
	public, _, err := svc.Register(ctx, hub.AgentDeclaration{DisplayName: "public", ProviderFamily: "test", TransportID: "http-json", Capabilities: []string{"text/plain", a2a.ExecutionCapability}, RegistrationIdempotency: "public"})
	if err != nil {
		t.Fatalf("public register: %v", err)
	}
	private, _, err := svc.RegisterWithSharedKey(ctx, hub.AgentDeclaration{DisplayName: "private", ProviderFamily: "test", TransportID: "http-json", Capabilities: []string{"text/plain", a2a.ExecutionCapability}, RegistrationIdempotency: "private"}, "private-a")
	if err != nil {
		t.Fatalf("private register: %v", err)
	}
	created := doJSON(t, handler, http.MethodPost, "/hub/v1/groups", private.AgentID, private.AgentToken, map[string]string{"name": "private group"})
	if created.Code != http.StatusCreated {
		t.Fatalf("private group create: %d/%s", created.Code, created.Body.String())
	}
	var group hub.Group
	decodeResponse(t, created, &group)
	crossList := doGroupA2ARequest(t, handler, http.MethodGet, "/a2a/v1/groups", public.AgentToken, "", nil)
	if crossList.Code != http.StatusOK || strings.Contains(crossList.Body.String(), group.GroupID) {
		t.Fatalf("cross-circle discovery: %d/%s", crossList.Code, crossList.Body.String())
	}
	crossCard := doGroupA2ARequest(t, handler, http.MethodGet, "/a2a/v1/groups/"+group.GroupID+"/card", public.AgentToken, "", nil)
	if crossCard.Code != http.StatusNotFound || crossCard.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("cross-circle card: %d/%s", crossCard.Code, crossCard.Body.String())
	}
	crossSend := doGroupA2ARequest(t, handler, http.MethodPost, "/a2a/v1/message:send", public.AgentToken, a2a.GroupExtensionURI, map[string]any{"tenant": "group:" + group.GroupID, "message": map[string]any{"messageId": "cross-message", "role": "ROLE_USER", "parts": []any{map[string]any{"text": "blocked"}}}, "configuration": map[string]any{"returnImmediately": true}})
	if crossSend.Code != http.StatusNotFound || !strings.Contains(crossSend.Body.String(), `"reason":"GROUP_NOT_FOUND"`) {
		t.Fatalf("cross-circle send: %d/%s", crossSend.Code, crossSend.Body.String())
	}

	dynamicA, _, err := svc.RegisterWithSharedKey(ctx, hub.AgentDeclaration{DisplayName: "dynamic-a", ProviderFamily: "test", TransportID: "http-json", Capabilities: []string{"text/plain", a2a.ExecutionCapability}, RegistrationIdempotency: "dynamic-a"}, "dynamic-key")
	if err != nil {
		t.Fatalf("dynamic A register: %v", err)
	}
	dynamicB, _, err := svc.RegisterWithSharedKey(ctx, hub.AgentDeclaration{DisplayName: "dynamic-b", ProviderFamily: "test", TransportID: "http-json", Capabilities: []string{"text/plain", a2a.ExecutionCapability}, RegistrationIdempotency: "dynamic-b"}, "dynamic-key")
	if err != nil {
		t.Fatalf("dynamic B register: %v", err)
	}
	dynamicGroupResponse := doJSON(t, handler, http.MethodPost, "/hub/v1/groups", dynamicA.AgentID, dynamicA.AgentToken, map[string]string{"name": "dynamic group"})
	var dynamicGroup hub.Group
	decodeResponse(t, dynamicGroupResponse, &dynamicGroup)
	invite := doJSON(t, handler, http.MethodPost, "/hub/v1/groups/"+dynamicGroup.GroupID+"/invitations", dynamicA.AgentID, dynamicA.AgentToken, map[string]string{"agentId": dynamicB.AgentID})
	var invitation hub.GroupInvitation
	decodeResponse(t, invite, &invitation)
	if accepted := doJSON(t, handler, http.MethodPost, "/hub/v1/groups/"+dynamicGroup.GroupID+"/accept", dynamicB.AgentID, dynamicB.AgentToken, nil); accepted.Code != http.StatusOK {
		t.Fatalf("dynamic accept: %d/%s", accepted.Code, accepted.Body.String())
	}
	dynamicList := doGroupA2ARequest(t, handler, http.MethodGet, "/a2a/v1/groups", dynamicA.AgentToken, "", nil)
	if dynamicList.Code != http.StatusOK || !strings.Contains(dynamicList.Body.String(), dynamicGroup.GroupID) {
		t.Fatalf("dynamic discovery: %d/%s", dynamicList.Code, dynamicList.Body.String())
	}
	dynamicSend := doGroupA2ARequest(t, handler, http.MethodPost, "/a2a/v1/message:send", dynamicA.AgentToken, a2a.GroupExtensionURI, map[string]any{"tenant": "group:" + dynamicGroup.GroupID, "message": map[string]any{"messageId": "dynamic-message", "role": "ROLE_USER", "parts": []any{map[string]any{"text": "dynamic"}}}, "configuration": map[string]any{"returnImmediately": true}})
	if dynamicSend.Code != http.StatusOK {
		t.Fatalf("dynamic send: %d/%s", dynamicSend.Code, dynamicSend.Body.String())
	}
}

func TestStandardGroupAuthorizationAndMalformedExtensionMetadata(t *testing.T) {
	ctx := context.Background()
	database, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer func() { _ = database.Close() }()
	cfg := config.Config{HubID: "public", ListenAddr: ":0", DatabasePath: filepath.Join(t.TempDir(), "unused.db"), PublicBaseURL: "https://hub.example", RegistrationEnabled: true, RegistrationTTL: time.Hour, PeerLease: time.Minute, MaxRegisteredAgents: 10, MaxTasksPerMinute: 50, MaxConcurrentTasks: 4, MaxPayloadBytes: 1 << 20, MaxGroupMembers: 4, MaxGroupFanout: 4, MaxGroupHistoryPage: 10, RegistrationPerMinute: 20, OperatorToken: "operator", StandardGatewayEnabled: true, GroupExtensionEnabled: true}
	svc := New(sqlite.NewRepository(database), cfg)
	handler := NewHTTPServer(svc).Handler()
	owner, _, err := svc.Register(ctx, hub.AgentDeclaration{DisplayName: "owner", ProviderFamily: "test", TransportID: "http-json", Capabilities: []string{"text/plain", a2a.ExecutionCapability}, RegistrationIdempotency: "owner"})
	if err != nil {
		t.Fatalf("owner register: %v", err)
	}
	nonMember, _, err := svc.Register(ctx, hub.AgentDeclaration{DisplayName: "non-member", ProviderFamily: "test", TransportID: "http-json", Capabilities: []string{"text/plain", a2a.ExecutionCapability}, RegistrationIdempotency: "non-member"})
	if err != nil {
		t.Fatalf("non-member register: %v", err)
	}
	created := doJSON(t, handler, http.MethodPost, "/hub/v1/groups", owner.AgentID, owner.AgentToken, map[string]string{"name": "auth group"})
	var group hub.Group
	decodeResponse(t, created, &group)
	card := doGroupA2ARequest(t, handler, http.MethodGet, "/a2a/v1/groups/"+group.GroupID+"/card", nonMember.AgentToken, "", nil)
	if card.Code != http.StatusNotFound {
		t.Fatalf("same-circle non-member card: %d/%s", card.Code, card.Body.String())
	}
	invited := doJSON(t, handler, http.MethodPost, "/hub/v1/groups/"+group.GroupID+"/invitations", owner.AgentID, owner.AgentToken, map[string]string{"agentId": nonMember.AgentID})
	var invitation hub.GroupInvitation
	decodeResponse(t, invited, &invitation)
	accepted := doJSON(t, handler, http.MethodPost, "/hub/v1/groups/"+group.GroupID+"/accept", nonMember.AgentID, nonMember.AgentToken, nil)
	if accepted.Code != http.StatusOK {
		t.Fatalf("accept non-member: %d/%s", accepted.Code, accepted.Body.String())
	}
	removed := doJSON(t, handler, http.MethodPost, "/hub/v1/groups/"+group.GroupID+"/members/"+nonMember.AgentID+"/remove", owner.AgentID, owner.AgentToken, nil)
	if removed.Code != http.StatusOK {
		t.Fatalf("remove member: %d/%s", removed.Code, removed.Body.String())
	}
	removedCard := doGroupA2ARequest(t, handler, http.MethodGet, "/a2a/v1/groups/"+group.GroupID+"/card", nonMember.AgentToken, "", nil)
	if removedCard.Code != http.StatusNotFound {
		t.Fatalf("removed member card: %d/%s", removedCard.Code, removedCard.Body.String())
	}
	operatorCard := doGroupA2ARequest(t, handler, http.MethodGet, "/a2a/v1/groups/"+group.GroupID+"/card", "operator", "", nil)
	if operatorCard.Code != http.StatusUnauthorized {
		t.Fatalf("operator card: %d/%s", operatorCard.Code, operatorCard.Body.String())
	}
	malformed := doGroupA2ARequest(t, handler, http.MethodPost, "/a2a/v1/message:send", owner.AgentToken, a2a.GroupExtensionURI, map[string]any{"tenant": "group:" + group.GroupID, "message": map[string]any{"messageId": "malformed", "role": "ROLE_USER", "parts": []any{map[string]any{"text": "bad metadata"}}, "metadata": map[string]any{a2a.GroupExtensionURI: "not-an-object"}}, "configuration": map[string]any{"returnImmediately": true}})
	if malformed.Code != http.StatusBadRequest || !strings.Contains(malformed.Body.String(), `"reason":"INVALID_ARGUMENT"`) {
		t.Fatalf("malformed metadata: %d/%s", malformed.Code, malformed.Body.String())
	}
	revoked := doJSONWithBearer(t, handler, http.MethodPost, "/hub/v1/admin/agents/"+nonMember.AgentID+"/revoke", "operator", map[string]string{"reason": "test"})
	if revoked.Code != http.StatusOK {
		t.Fatalf("revoke member: %d/%s", revoked.Code, revoked.Body.String())
	}
	revokedCard := doGroupA2ARequest(t, handler, http.MethodGet, "/a2a/v1/groups/"+group.GroupID+"/card", nonMember.AgentToken, "", nil)
	if revokedCard.Code != http.StatusUnauthorized {
		t.Fatalf("revoked member card: %d/%s", revokedCard.Code, revokedCard.Body.String())
	}
	noRecipient := doGroupA2ARequest(t, handler, http.MethodPost, "/a2a/v1/message:send", owner.AgentToken, a2a.GroupExtensionURI, map[string]any{"tenant": "group:" + group.GroupID, "message": map[string]any{"messageId": "no-recipient", "role": "ROLE_USER", "parts": []any{map[string]any{"text": "none"}}}, "configuration": map[string]any{"returnImmediately": true}})
	if noRecipient.Code != http.StatusBadRequest || !strings.Contains(noRecipient.Body.String(), `"reason":"GROUP_NO_RECIPIENTS"`) {
		t.Fatalf("no-recipient group: %d/%s", noRecipient.Code, noRecipient.Body.String())
	}
}
