package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tbdavid2019/888a2a-lite/internal/config"
	"github.com/tbdavid2019/888a2a-lite/internal/store/sqlite"
)

func TestHandlerServesLLMSTxtAtRoot(t *testing.T) {
	database, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer func() {
		if err := database.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	}()
	cfg := config.Config{
		HubID: "public", ListenAddr: ":0", DatabasePath: filepath.Join(t.TempDir(), "unused.db"),
		RegistrationEnabled: true, RegistrationTTL: 24 * time.Hour, PeerLease: 90 * time.Second,
		MaxRegisteredAgents: 10, MaxTasksPerMinute: 20, MaxConcurrentTasks: 4,
		MaxPayloadBytes: 1 << 20, RegistrationPerMinute: 20,
	}
	handler := NewHTTPServer(New(sqlite.NewRepository(database), cfg)).Handler()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/llms.txt", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if contentType := response.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/plain") {
		t.Fatalf("content type = %q, want text/plain", contentType)
	}
	body := response.Body.String()
	for _, expected := range []string{"# 888a2a-lite", "https://github.com/tbdavid2019/888a2a-lite", "http://example.com/hub/v1"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("llms.txt does not contain %q", expected)
		}
	}

	cfg.PublicBaseURL = "https://new-hub.example"
	handler = NewHTTPServer(New(sqlite.NewRepository(database), cfg)).Handler()
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/llms.txt", nil))
	if !strings.Contains(response.Body.String(), "https://new-hub.example/hub/v1") || strings.Contains(response.Body.String(), "http://example.com/hub/v1") {
		t.Fatalf("configured public URL was not applied: %s", response.Body.String())
	}
}
