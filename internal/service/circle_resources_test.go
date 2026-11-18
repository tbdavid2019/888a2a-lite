package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tbdavid2019/888a2a-lite/internal/config"
	"github.com/tbdavid2019/888a2a-lite/internal/hub"
	"github.com/tbdavid2019/888a2a-lite/internal/store/sqlite"
)

func newCircleResourceTestServer(t *testing.T, writePolicy, uploadEndpoint string) (*Service, http.Handler) {
	t.Helper()
	database, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	cfg := config.Config{
		HubID:                  "public",
		ListenAddr:             ":0",
		DatabasePath:           filepath.Join(t.TempDir(), "unused.db"),
		RegistrationEnabled:    true,
		RegistrationTTL:        24 * time.Hour,
		PeerLease:              5 * time.Minute,
		MaxRegisteredAgents:    20,
		MaxCircles:             20,
		EmptyCircleTTL:         24 * time.Hour,
		MaxTasksPerMinute:      60,
		MaxConcurrentTasks:     4,
		MaxPayloadBytes:        1 << 20,
		RegistrationPerMinute:  30,
		OperatorToken:          "op-secret-token",
		CircleMode:             "multi",
		AllowDynamicCircles:    true,
		CircleDerivationSecret: "circle-secret-for-tests",
		CircleSkillWritePolicy: writePolicy,
		UploadEndpoint:         uploadEndpoint,
	}
	svc := New(sqlite.NewRepository(database), cfg)
	return svc, NewHTTPServer(svc).Handler()
}

func registerCircleResourceAgent(t *testing.T, svc *Service, name, family, key string) hub.AgentIdentity {
	t.Helper()
	id, _, err := svc.RegisterWithSharedKey(context.Background(), hub.AgentDeclaration{
		DisplayName:             name,
		ProviderFamily:          family,
		TransportID:             "http-json",
		Capabilities:            []string{"text/plain"},
		RegistrationIdempotency: "reg-res-" + name + "-" + key,
	}, key)
	if err != nil {
		t.Fatalf("RegisterWithSharedKey(%s): %v", name, err)
	}
	return id
}

func TestCircleResourcesHTTPProposalApproveETagSSEAndCSP(t *testing.T) {
	svc, handler := newCircleResourceTestServer(t, "human_or_admin", config.DefaultUploadEndpoint)
	human := registerCircleResourceAgent(t, svc, "david", "browser", "team-res-alpha")
	bot := registerCircleResourceAgent(t, svc, "hermes-bot", "hermes", "team-res-alpha")
	outsider := registerCircleResourceAgent(t, svc, "stranger", "browser", "team-res-beta")

	// 0. Check CSP connect-src & status uploadEndpoint & system-card extension
	chatReq := httptest.NewRequest(http.MethodGet, "/", nil)
	chatResp := httptest.NewRecorder()
	handler.ServeHTTP(chatResp, chatReq)
	if chatResp.Code != http.StatusOK {
		t.Fatalf("GET / status = %d", chatResp.Code)
	}
	if csp := chatResp.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "connect-src 'self' https://box.david888.com;") {
		t.Fatalf("chat CSP missing configured upload origin: %q", csp)
	}
	statusReq := httptest.NewRequest(http.MethodGet, "/hub/v1/status", nil)
	statusResp := httptest.NewRecorder()
	handler.ServeHTTP(statusResp, statusReq)
	if !strings.Contains(statusResp.Body.String(), config.DefaultUploadEndpoint) {
		t.Fatalf("status response missing uploadEndpoint: %s", statusResp.Body.String())
	}
	cardReq := httptest.NewRequest(http.MethodGet, "/hub/v1/system-card.json", nil)
	cardResp := httptest.NewRecorder()
	handler.ServeHTTP(cardResp, cardReq)
	if !strings.Contains(cardResp.Body.String(), hub.CircleResourceExtensionURI) {
		t.Fatalf("system card missing circle resource extension: %s", cardResp.Body.String())
	}

	// 1. Bot cannot directly publish active resource (403 Forbidden).
	putBody := `{"kind":"doc","content":"# Direct\nforbidden","expectedVersion":0,"idempotencyKey":"bot-res-1"}`
	req := httptest.NewRequest(http.MethodPut, "/hub/v1/circle/resources/api-spec", strings.NewReader(putBody))
	req.Header.Set("X-Agent-ID", bot.AgentID)
	req.Header.Set("Authorization", "Bearer "+bot.AgentToken)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("bot direct PUT status = %d, want 403 (%s)", resp.Code, resp.Body.String())
	}

	// 2. Connect capability-gated SSE client (`?events=circle_resource`) and verify event delivery.
	ts := httptest.NewServer(handler)
	defer ts.Close()

	resStreamReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/hub/v1/agents/"+bot.AgentID+"/inbox/stream?events=circle_skill,circle_resource", nil)
	resStreamReq.Header.Set("X-Agent-ID", bot.AgentID)
	resStreamReq.Header.Set("Authorization", "Bearer "+bot.AgentToken)
	resStreamResp, err := ts.Client().Do(resStreamReq)
	if err != nil {
		t.Fatalf("connect resource SSE stream: %v", err)
	}
	defer func() { _ = resStreamResp.Body.Close() }()

	resEventCh := make(chan string, 8)
	go func() {
		scanner := bufio.NewScanner(resStreamResp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "event: circle_resource") {
				resEventCh <- line
			}
		}
	}()

	time.Sleep(50 * time.Millisecond)

	// 3. Bot submits asset proposal -> 202 Accepted + proposal_created SSE
	propBody := `{"kind":"asset","title":"Wireframe","summary":"Homepage PNG","url":"https://box.david888.com/f/wireframe.png","mediaType":"image/png","filename":"wireframe.png","sizeBytes":10240,"baseVersion":0,"requestedTtlSeconds":3600,"idempotencyKey":"prop-res-1"}`
	req = httptest.NewRequest(http.MethodPut, "/hub/v1/circle/resources/proposals/wireframe", strings.NewReader(propBody))
	req.Header.Set("X-Agent-ID", bot.AgentID)
	req.Header.Set("Authorization", "Bearer "+bot.AgentToken)
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusAccepted {
		t.Fatalf("bot proposal PUT status = %d, want 202 (%s)", resp.Code, resp.Body.String())
	}

	select {
	case <-resEventCh:
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for circle_resource proposal_created SSE event")
	}

	// 4. Bot cannot approve its own proposal (403 Forbidden)
	approveBody := `{"expectedVersion":0,"idempotencyKey":"approve-res-1"}`
	req = httptest.NewRequest(http.MethodPost, "/hub/v1/circle/resources/proposals/wireframe/approve", strings.NewReader(approveBody))
	req.Header.Set("X-Agent-ID", bot.AgentID)
	req.Header.Set("Authorization", "Bearer "+bot.AgentToken)
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("bot approve status = %d, want 403", resp.Code)
	}

	// 5. Human in same Circle approves -> 200 OK + SSE updated event
	req = httptest.NewRequest(http.MethodPost, "/hub/v1/circle/resources/proposals/wireframe/approve", strings.NewReader(approveBody))
	req.Header.Set("X-Agent-ID", human.AgentID)
	req.Header.Set("Authorization", "Bearer "+human.AgentToken)
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("human approve status = %d, want 200 (%s)", resp.Code, resp.Body.String())
	}
	select {
	case <-resEventCh:
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for circle_resource updated SSE event")
	}

	// 6. Manifest + ETag 304 + cross-circle isolation
	req = httptest.NewRequest(http.MethodGet, "/hub/v1/circle/resources", nil)
	req.Header.Set("X-Agent-ID", bot.AgentID)
	req.Header.Set("Authorization", "Bearer "+bot.AgentToken)
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET manifest status = %d", resp.Code)
	}
	var manifest hub.CircleResourceManifest
	if err := json.Unmarshal(resp.Body.Bytes(), &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if manifest.ResourceCount != 1 || manifest.Resources[0].URL != "https://box.david888.com/f/wireframe.png" {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}
	req304 := httptest.NewRequest(http.MethodGet, "/hub/v1/circle/resources", nil)
	req304.Header.Set("X-Agent-ID", bot.AgentID)
	req304.Header.Set("Authorization", "Bearer "+bot.AgentToken)
	req304.Header.Set("If-None-Match", manifest.ETag)
	resp304 := httptest.NewRecorder()
	handler.ServeHTTP(resp304, req304)
	if resp304.Code != http.StatusNotModified {
		t.Fatalf("GET manifest If-None-Match status = %d, want 304", resp304.Code)
	}

	// Outsider in different Circle sees 0 resources
	reqOut := httptest.NewRequest(http.MethodGet, "/hub/v1/circle/resources", nil)
	reqOut.Header.Set("X-Agent-ID", outsider.AgentID)
	reqOut.Header.Set("Authorization", "Bearer "+outsider.AgentToken)
	respOut := httptest.NewRecorder()
	handler.ServeHTTP(respOut, reqOut)
	if respOut.Code != http.StatusOK {
		t.Fatalf("outsider GET manifest status = %d", respOut.Code)
	}
	var outManifest hub.CircleResourceManifest
	_ = json.Unmarshal(respOut.Body.Bytes(), &outManifest)
	if outManifest.ResourceCount != 0 {
		t.Fatalf("outsider saw cross-circle resources: %+v", outManifest)
	}

	// 7. Human directly publishes a doc resource & deletes it
	docBody := `{"kind":"doc","content":"# API Notes\nUse v1 carefully.","expectedVersion":0,"idempotencyKey":"put-doc-1"}`
	reqDoc := httptest.NewRequest(http.MethodPut, "/hub/v1/circle/resources/api-notes", strings.NewReader(docBody))
	reqDoc.Header.Set("X-Agent-ID", human.AgentID)
	reqDoc.Header.Set("Authorization", "Bearer "+human.AgentToken)
	respDoc := httptest.NewRecorder()
	handler.ServeHTTP(respDoc, reqDoc)
	if respDoc.Code != http.StatusCreated {
		t.Fatalf("human PUT doc status = %d, want 201 (%s)", respDoc.Code, respDoc.Body.String())
	}
	reqGetDoc := httptest.NewRequest(http.MethodGet, "/hub/v1/circle/resources/api-notes", nil)
	reqGetDoc.Header.Set("X-Agent-ID", bot.AgentID)
	reqGetDoc.Header.Set("Authorization", "Bearer "+bot.AgentToken)
	respGetDoc := httptest.NewRecorder()
	handler.ServeHTTP(respGetDoc, reqGetDoc)
	if respGetDoc.Code != http.StatusOK || !strings.Contains(respGetDoc.Body.String(), "Use v1 carefully.") {
		t.Fatalf("GET doc failed: %d %s", respGetDoc.Code, respGetDoc.Body.String())
	}
	etag1 := respGetDoc.Header().Get("ETag")

	// Updating title without changing content must produce a distinct ETag (no stale 304)
	docBodyV2 := `{"kind":"doc","title":"Renamed API Notes","content":"# API Notes\nUse v1 carefully.","expectedVersion":1,"idempotencyKey":"put-doc-2"}`
	reqDocV2 := httptest.NewRequest(http.MethodPut, "/hub/v1/circle/resources/api-notes", strings.NewReader(docBodyV2))
	reqDocV2.Header.Set("X-Agent-ID", human.AgentID)
	reqDocV2.Header.Set("Authorization", "Bearer "+human.AgentToken)
	respDocV2 := httptest.NewRecorder()
	handler.ServeHTTP(respDocV2, reqDocV2)
	if respDocV2.Code != http.StatusOK {
		t.Fatalf("human PUT doc v2 status = %d, want 200", respDocV2.Code)
	}
	reqGetDoc2 := httptest.NewRequest(http.MethodGet, "/hub/v1/circle/resources/api-notes", nil)
	reqGetDoc2.Header.Set("X-Agent-ID", bot.AgentID)
	reqGetDoc2.Header.Set("Authorization", "Bearer "+bot.AgentToken)
	reqGetDoc2.Header.Set("If-None-Match", etag1)
	respGetDoc2 := httptest.NewRecorder()
	handler.ServeHTTP(respGetDoc2, reqGetDoc2)
	if respGetDoc2.Code != http.StatusOK || respGetDoc2.Header().Get("ETag") == etag1 {
		t.Fatalf("expected 200 with updated ETag after metadata edit, got %d etag1=%q etag2=%q", respGetDoc2.Code, etag1, respGetDoc2.Header().Get("ETag"))
	}

	reqDel := httptest.NewRequest(http.MethodDelete, "/hub/v1/circle/resources/api-notes?expectedVersion=2&idempotencyKey=del-doc-1", nil)
	reqDel.Header.Set("X-Agent-ID", human.AgentID)
	reqDel.Header.Set("Authorization", "Bearer "+human.AgentToken)
	respDel := httptest.NewRecorder()
	handler.ServeHTTP(respDel, reqDel)
	if respDel.Code != http.StatusOK {
		t.Fatalf("DELETE doc failed: %d %s", respDel.Code, respDel.Body.String())
	}
}

func TestCircleResourcesAdminOnlyWritePolicyAndOperatorApprove(t *testing.T) {
	svc, handler := newCircleResourceTestServer(t, "admin_only", "")
	human := registerCircleResourceAgent(t, svc, "alice", "browser", "team-admin-only")

	body := bytes.NewBufferString(`{"kind":"doc","content":"# Only Admin","expectedVersion":0,"idempotencyKey":"hum-put-1"}`)
	req := httptest.NewRequest(http.MethodPut, "/hub/v1/circle/resources/notes", body)
	req.Header.Set("X-Agent-ID", human.AgentID)
	req.Header.Set("Authorization", "Bearer "+human.AgentToken)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("human write under admin_only = %d, want 403", resp.Code)
	}

	// Operator puts resource directly
	opBody := `{"kind":"doc","content":"# Approved by Op","expectedVersion":0,"idempotencyKey":"op-res-1"}`
	reqOp := httptest.NewRequest(http.MethodPut, "/hub/v1/admin/circles/"+human.CircleID+"/resources/notes", strings.NewReader(opBody))
	reqOp.Header.Set("Authorization", "Bearer op-secret-token")
	respOp := httptest.NewRecorder()
	handler.ServeHTTP(respOp, reqOp)
	if respOp.Code != http.StatusCreated {
		t.Fatalf("operator PUT resource = %d (%s)", respOp.Code, respOp.Body.String())
	}

	reqAdminList := httptest.NewRequest(http.MethodGet, "/hub/v1/admin/circles/"+human.CircleID+"/resources", nil)
	reqAdminList.Header.Set("Authorization", "Bearer op-secret-token")
	respAdminList := httptest.NewRecorder()
	handler.ServeHTTP(respAdminList, reqAdminList)
	if respAdminList.Code != http.StatusOK || !strings.Contains(respAdminList.Body.String(), "notes") {
		t.Fatalf("operator list resources = %d (%s)", respAdminList.Code, respAdminList.Body.String())
	}
}
