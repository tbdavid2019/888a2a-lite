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

func newCircleSkillTestServer(t *testing.T, writePolicy string) (*Service, http.Handler, *sqlite.DB) {
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
	}
	svc := New(sqlite.NewRepository(database), cfg)
	return svc, NewHTTPServer(svc).Handler(), database
}

func registerCircleAgentForSkills(t *testing.T, svc *Service, name, family, key string) hub.AgentIdentity {
	t.Helper()
	id, _, err := svc.RegisterWithSharedKey(context.Background(), hub.AgentDeclaration{
		DisplayName:             name,
		ProviderFamily:          family,
		TransportID:             "http-json",
		Capabilities:            []string{"text/plain"},
		RegistrationIdempotency: "reg-" + name + "-" + key,
	}, key)
	if err != nil {
		t.Fatalf("RegisterWithSharedKey(%s): %v", name, err)
	}
	return id
}

func TestCircleSkillsHTTPBotProposalHumanApproveTTLIsolationAndSSE(t *testing.T) {
	svc, handler, _ := newCircleSkillTestServer(t, "human_or_admin")
	human := registerCircleAgentForSkills(t, svc, "david", "browser", "team-alpha-key")
	bot := registerCircleAgentForSkills(t, svc, "hermes-bot", "hermes", "team-alpha-key")
	outsider := registerCircleAgentForSkills(t, svc, "stranger", "browser", "team-beta-key")

	// 1. Bot cannot directly publish active skill (403 Forbidden).
	putBody := `{"content":"# Bad\nDirect bot publish","expectedVersion":0,"idempotencyKey":"bot-put-1"}`
	req := httptest.NewRequest(http.MethodPut, "/hub/v1/circle/skills/deploy-guide", strings.NewReader(putBody))
	req.Header.Set("X-Agent-ID", bot.AgentID)
	req.Header.Set("Authorization", "Bearer "+bot.AgentToken)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("bot direct PUT status = %d, want 403 (%s)", resp.Code, resp.Body.String())
	}

	// 2. Connect capability-gated SSE client and legacy SSE client concurrently via httptest.Server.
	ts := httptest.NewServer(handler)
	defer ts.Close()

	legacyReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/hub/v1/agents/"+bot.AgentID+"/inbox/stream", nil)
	legacyReq.Header.Set("Authorization", "Bearer "+bot.AgentToken)
	legacyResp, err := ts.Client().Do(legacyReq)
	if err != nil {
		t.Fatalf("legacy SSE connect: %v", err)
	}
	defer func() { _ = legacyResp.Body.Close() }()

	optInReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/hub/v1/agents/"+human.AgentID+"/inbox/stream?events=task,circle_skill", nil)
	optInReq.Header.Set("Authorization", "Bearer "+human.AgentToken)
	optInResp, err := ts.Client().Do(optInReq)
	if err != nil {
		t.Fatalf("optIn SSE connect: %v", err)
	}
	defer func() { _ = optInResp.Body.Close() }()

	optInLines := make(chan string, 32)
	go func() {
		scanner := bufio.NewScanner(optInResp.Body)
		for scanner.Scan() {
			optInLines <- scanner.Text()
		}
		close(optInLines)
	}()

	legacyLines := make(chan string, 32)
	go func() {
		scanner := bufio.NewScanner(legacyResp.Body)
		for scanner.Scan() {
			legacyLines <- scanner.Text()
		}
		close(legacyLines)
	}()

	// Give SSE subscriptions a brief moment to register.
	time.Sleep(40 * time.Millisecond)

	// 3. Bot submits a draft skill proposal (202 Accepted).
	propBody := `{"content":"# Deploy Guide\nRun checks first.","requestedTtlSeconds":3600,"idempotencyKey":"prop-1"}`
	req = httptest.NewRequest(http.MethodPut, "/hub/v1/circle/skills/proposals/deploy-guide", strings.NewReader(propBody))
	req.Header.Set("X-Agent-ID", bot.AgentID)
	req.Header.Set("Authorization", "Bearer "+bot.AgentToken)
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusAccepted {
		t.Fatalf("bot proposal PUT status = %d, want 202 (%s)", resp.Code, resp.Body.String())
	}

	// Verify opt-in stream got proposal_created metadata-only event, legacy stream got nothing.
	sawEvent := false
	deadline := time.After(time.Second)
	for !sawEvent {
		select {
		case line, ok := <-optInLines:
			if !ok {
				t.Fatal("optIn SSE closed unexpectedly")
			}
			if strings.HasPrefix(line, "data:") {
				if strings.Contains(line, `"action":"proposal_created"`) && strings.Contains(line, `"slug":"deploy-guide"`) && !strings.Contains(line, "Run checks first") {
					sawEvent = true
				}
			}
		case <-deadline:
			t.Fatal("timed out waiting for proposal_created SSE event")
		}
	}
	select {
	case line := <-legacyLines:
		if strings.Contains(line, "circle_skill") || strings.Contains(line, "deploy-guide") {
			t.Fatalf("legacy SSE stream received unexpected circle_skill data: %q", line)
		}
	case <-time.After(60 * time.Millisecond):
	}

	// 4. Bot cannot approve its own proposal (403); Human approves it (200).
	approveBody := `{"expectedVersion":0,"ttlSeconds":120,"idempotencyKey":"app-1"}`
	req = httptest.NewRequest(http.MethodPost, "/hub/v1/circle/skills/proposals/deploy-guide/approve", strings.NewReader(approveBody))
	req.Header.Set("X-Agent-ID", bot.AgentID)
	req.Header.Set("Authorization", "Bearer "+bot.AgentToken)
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("bot approve status = %d, want 403", resp.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/hub/v1/circle/skills/proposals/deploy-guide/approve", strings.NewReader(approveBody))
	req.Header.Set("X-Agent-ID", human.AgentID)
	req.Header.Set("Authorization", "Bearer "+human.AgentToken)
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("human approve status = %d, want 200 (%s)", resp.Code, resp.Body.String())
	}

	// 5. Manifest & ETag 304 check, plus cross-circle masked 404.
	req = httptest.NewRequest(http.MethodGet, "/hub/v1/circle/skills", nil)
	req.Header.Set("X-Agent-ID", bot.AgentID)
	req.Header.Set("Authorization", "Bearer "+bot.AgentToken)
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET manifest status = %d, want 200", resp.Code)
	}
	etag := resp.Header().Get("ETag")
	if etag == "" {
		t.Fatal("missing manifest ETag")
	}
	var manifest hub.CircleSkillManifest
	if err := json.Unmarshal(resp.Body.Bytes(), &manifest); err != nil || manifest.SkillCount != 1 {
		t.Fatalf("unexpected manifest: %s (err=%v)", resp.Body.String(), err)
	}

	req = httptest.NewRequest(http.MethodGet, "/hub/v1/circle/skills", nil)
	req.Header.Set("X-Agent-ID", bot.AgentID)
	req.Header.Set("Authorization", "Bearer "+bot.AgentToken)
	req.Header.Set("If-None-Match", etag)
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusNotModified {
		t.Fatalf("If-None-Match manifest status = %d, want 304", resp.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/hub/v1/circle/skills/deploy-guide", nil)
	req.Header.Set("X-Agent-ID", outsider.AgentID)
	req.Header.Set("Authorization", "Bearer "+outsider.AgentToken)
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("cross-circle skill probe status = %d, want 404", resp.Code)
	}

	// 6. Operator endpoint & admin_only enforcement.
	opBody := `{"content":"# Admin Skill\nOperator managed.","expectedVersion":0,"idempotencyKey":"op-put-1"}`
	req = httptest.NewRequest(http.MethodPut, "/hub/v1/admin/circles/"+human.CircleID+"/skills/op-skill", strings.NewReader(opBody))
	req.Header.Set("Authorization", "Bearer op-secret-token")
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusCreated {
		t.Fatalf("operator PUT skill status = %d, want 201 (%s)", resp.Code, resp.Body.String())
	}

	// Human delete via query params without body succeeds.
	req = httptest.NewRequest(http.MethodDelete, "/hub/v1/circle/skills/deploy-guide?expectedVersion=1&idempotencyKey=del-1", nil)
	req.Header.Set("X-Agent-ID", human.AgentID)
	req.Header.Set("Authorization", "Bearer "+human.AgentToken)
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("human DELETE skill status = %d, want 200 (%s)", resp.Code, resp.Body.String())
	}
}

func TestCircleSkillsAdminOnlyPolicyAndDisabledCircle(t *testing.T) {
	svc, handler, _ := newCircleSkillTestServer(t, "admin_only")
	human := registerCircleAgentForSkills(t, svc, "human-u", "browser", "strict-key")

	body := `{"content":"# Blocked\nNot allowed under admin_only","expectedVersion":0,"idempotencyKey":"h-put-1"}`
	req := httptest.NewRequest(http.MethodPut, "/hub/v1/circle/skills/test-skill", bytes.NewBufferString(body))
	req.Header.Set("X-Agent-ID", human.AgentID)
	req.Header.Set("Authorization", "Bearer "+human.AgentToken)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("human PUT under admin_only = %d, want 403", resp.Code)
	}

	if _, err := svc.DisableCircle(context.Background(), "op-secret-token", human.CircleID); err != nil {
		t.Fatalf("DisableCircle: %v", err)
	}
	req = httptest.NewRequest(http.MethodGet, "/hub/v1/circle/skills", nil)
	req.Header.Set("X-Agent-ID", human.AgentID)
	req.Header.Set("Authorization", "Bearer "+human.AgentToken)
	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("disabled circle GET skills = %d, want 403", resp.Code)
	}
}
