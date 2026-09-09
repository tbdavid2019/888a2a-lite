package service

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tbdavid2019/888a2a-lite/internal/hub"
	"github.com/tbdavid2019/888a2a-lite/internal/store"
)

//go:embed llms.txt
var llmsText []byte

//go:embed admin.html
var adminHTML []byte

//go:embed install.sh
var installScript []byte

//go:embed a2a_bridge.py
var a2aBridgeScript []byte

type HTTPServer struct {
	service             *Service
	maxBodyBytes        int64
	baseURL             string
	registrationLimiter *requestLimiter
	taskLimiter         *requestLimiter
	standardWaitSlots   *keyedConcurrencyLimiter
	standardStreamSlots *keyedConcurrencyLimiter
}

func NewHTTPServer(service *Service) *HTTPServer {
	return &HTTPServer{
		service:             service,
		maxBodyBytes:        service.config.MaxPayloadBytes,
		baseURL:             service.config.PublicBaseURL,
		registrationLimiter: newRequestLimiter(service.config.RegistrationPerMinute, time.Minute),
		taskLimiter:         newRequestLimiter(service.config.MaxTasksPerMinute, time.Minute),
		standardWaitSlots:   newKeyedConcurrencyLimiter(service.config.MaxConcurrentTasks),
		standardStreamSlots: newKeyedConcurrencyLimiter(service.config.MaxConcurrentTasks),
	}
}

func (server *HTTPServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", server.health)
	mux.HandleFunc("GET /llms.txt", server.llms)
	mux.HandleFunc("GET /install.sh", server.installScriptHandler)
	mux.HandleFunc("GET /a2a_bridge.py", server.a2aBridgeScriptHandler)
	mux.HandleFunc("GET /admin", server.adminAnnouncementsUI)
	mux.HandleFunc("GET /admin/announcements", server.adminAnnouncementsUI)
	mux.HandleFunc("GET /admin/messages", server.adminAnnouncementsUI)
	mux.HandleFunc("GET /admin/agents", server.adminAnnouncementsUI)
	mux.HandleFunc("GET /hub/v1/system-card.json", server.systemCard)
	mux.HandleFunc("GET /hub/v1/announcements", server.announcements)
	mux.HandleFunc("GET /hub/v1/admin/announcements", server.adminListAnnouncements)
	mux.HandleFunc("GET /hub/v1/admin/messages", server.adminListMessages)
	mux.HandleFunc("POST /hub/v1/admin/announcements", server.adminCreateAnnouncement)
	mux.HandleFunc("PATCH /hub/v1/admin/announcements/{announcementId}", server.adminUpdateDraft)
	mux.HandleFunc("POST /hub/v1/admin/announcements/{announcementId}/publish", server.adminPublishDraft)
	mux.HandleFunc("POST /hub/v1/admin/announcements/{announcementId}/revision", server.adminCreateRevision)
	mux.HandleFunc("GET /hub/v1/admin/events", server.listEvents)
	if server.service.config.StandardGatewayEnabled {
		mux.HandleFunc("GET /.well-known/agent-card.json", server.standardGatewayCard)
		mux.HandleFunc("GET /a2a/v1/agents/{agentId}/.well-known/agent-card.json", server.standardAgentCard)
		if server.service.config.GroupExtensionEnabled {
			mux.HandleFunc("GET /a2a/v1/groups", server.standardListGroups)
			mux.HandleFunc("GET /a2a/v1/groups/{groupId}/card", server.standardGroupCard)
			mux.HandleFunc("GET /a2a/v1/groups/{groupId}/.well-known/agent-card.json", server.standardGroupCard)
		}
		mux.HandleFunc("POST /a2a/v1/message:send", server.standardSendMessage)
		mux.HandleFunc("POST /message:send", server.standardSendMessage)
		mux.HandleFunc("POST /a2a/v1/{tenant}/message:send", server.standardSendMessage)
		mux.HandleFunc("POST /{tenant}/message:send", server.standardSendMessage)
		mux.HandleFunc("POST /a2a/v1/message:stream", server.standardStreamMessage)
		mux.HandleFunc("POST /message:stream", server.standardStreamMessage)
		mux.HandleFunc("POST /a2a/v1/{tenant}/message:stream", server.standardStreamMessage)
		mux.HandleFunc("POST /{tenant}/message:stream", server.standardStreamMessage)
		mux.HandleFunc("GET /a2a/v1/tasks", server.standardListTasks)
		mux.HandleFunc("GET /tasks", server.standardListTasks)
		mux.HandleFunc("/a2a/v1/{first}/{second}", server.standardTasks2Dispatch)
		mux.HandleFunc("/{first}/{second}", server.standardTasks2Dispatch)
		mux.HandleFunc("/a2a/v1/{p1}/{p2}/{p3}", server.standardTasks3Dispatch)
		mux.HandleFunc("/{p1}/{p2}/{p3}", server.standardTasks3Dispatch)
		mux.HandleFunc("POST /hub/v1/a2a/tasks/{taskId}/updates", server.standardTaskUpdate)
	}
	mux.HandleFunc("GET /hub/v1/status", server.status)
	mux.HandleFunc("POST /hub/v1/agents/register", server.register)
	mux.HandleFunc("GET /hub/v1/agents", server.listAgents)
	mux.HandleFunc("GET /hub/v1/agents/{agentId}", server.getAgent)
	mux.HandleFunc("GET /hub/v1/agents/{agentId}/agent-card.json", server.agentCard)
	mux.HandleFunc("POST /hub/v1/agents/{agentId}/heartbeat", server.heartbeat)
	mux.HandleFunc("POST /hub/v1/agents/{agentId}/disconnect", server.disconnect)
	mux.HandleFunc("POST /hub/v1/agents/{targetAgentId}/tasks", server.sendTask)
	mux.HandleFunc("GET /hub/v1/agents/{agentId}/inbox", server.pollInbox)
	mux.HandleFunc("GET /hub/v1/agents/{agentId}/inbox/stream", server.streamInbox)
	mux.HandleFunc("POST /hub/v1/agents/{agentId}/inbox/{sequence}/ack", server.ackInbox)
	mux.HandleFunc("GET /hub/v1/groups", server.listGroups)
	mux.HandleFunc("POST /hub/v1/groups", server.createGroup)
	mux.HandleFunc("GET /hub/v1/groups/invitations", server.listGroupInvitations)
	mux.HandleFunc("POST /hub/v1/groups/invitations/{invitationId}/accept", server.acceptGroupInvitation)
	mux.HandleFunc("POST /hub/v1/groups/{groupId}/accept", server.acceptGroupInvitationByGroup)
	mux.HandleFunc("GET /hub/v1/groups/{groupId}", server.getGroup)
	mux.HandleFunc("POST /hub/v1/groups/{groupId}/invitations", server.inviteGroupMember)
	mux.HandleFunc("POST /hub/v1/groups/{groupId}/leave", server.leaveGroup)
	mux.HandleFunc("POST /hub/v1/groups/{groupId}/archive", server.archiveGroup)
	mux.HandleFunc("POST /hub/v1/groups/{groupId}/ownership", server.transferGroupOwnership)
	mux.HandleFunc("POST /hub/v1/groups/{groupId}/members/{agentId}/remove", server.removeGroupMember)
	mux.HandleFunc("GET /hub/v1/groups/{groupId}/roster", server.groupRoster)
	mux.HandleFunc("GET /hub/v1/groups/{groupId}/history", server.groupHistory)
	mux.HandleFunc("POST /hub/v1/groups/{groupId}/messages", server.sendGroupMessage)
	mux.HandleFunc("GET /hub/v1/groups/{groupId}/charter", server.getGroupCharter)
	mux.HandleFunc("PUT /hub/v1/groups/{groupId}/charter", server.putGroupCharter)
	mux.HandleFunc("GET /hub/v1/groups/{groupId}/charter/revisions", server.listGroupCharterRevisions)
	mux.HandleFunc("POST /hub/v1/groups/{groupId}/charter/rollback", server.rollbackGroupCharter)
	mux.HandleFunc("GET /hub/v1/groups/{groupId}/secretary", server.getGroupSecretary)
	mux.HandleFunc("PUT /hub/v1/groups/{groupId}/secretary", server.appointGroupSecretary)
	mux.HandleFunc("POST /hub/v1/groups/{groupId}/secretary/renew", server.renewGroupSecretary)
	mux.HandleFunc("POST /hub/v1/groups/{groupId}/secretary/revoke", server.revokeGroupSecretary)
	mux.HandleFunc("POST /hub/v1/admin/registration", server.setRegistration)
	mux.HandleFunc("GET /hub/v1/admin/agents", server.adminListAgents)
	mux.HandleFunc("GET /hub/v1/admin/circles", server.adminListCircles)
	mux.HandleFunc("POST /hub/v1/admin/circles/{circleId}/disable", server.adminDisableCircle)
	mux.HandleFunc("POST /hub/v1/admin/circles/{circleId}/keys/rotate", server.adminRotateCircleKey)
	mux.HandleFunc("POST /hub/v1/admin/circles/{circleId}/keys/{version}/revoke", server.adminRevokeCircleKey)
	mux.HandleFunc("POST /hub/v1/admin/agents/{agentId}/revoke", server.revokeAgent)
	mux.HandleFunc("DELETE /hub/v1/admin/agents/{agentId}", server.adminDeleteAgent)
	mux.HandleFunc("POST /hub/v1/admin/agents/prune", server.adminPruneAgents)
	mux.HandleFunc("POST /hub/v1/admin/tasks/{taskId}/cancel", server.cancelTask)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		mux.ServeHTTP(w, r)
	})
}

func (server *HTTPServer) llms(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	mode := "PUBLIC"
	authSummary := "None required. This Hub is running in PUBLIC mode; registration is open without any pre-shared key."
	regHeader := "(No authentication headers required in PUBLIC mode; simply POST payload)"
	if server.service.circleResolver.Mode() == "multi" {
		mode = "MULTI_CIRCLE"
		if server.service.config.AllowDynamicCircles {
			authSummary = "Optional. Dynamic Circles ENABLED. No key enters the public circle. ANY secret key in X-Hub-Key dynamically creates or joins an isolated private circle (動態新天地). Ask the user if they want to provide a private circle key or join the public circle."
			regHeader = "(No key for public circle; X-Hub-Key: <secret> to dynamically create/join a private circle)"
		} else {
			authSummary = "Optional. Whitelist Circles ONLY. Registration without a pre-shared key enters the public circle; an allowed team key from A2A888_HUB_SHARED_KEYS enters its private circle. Ask the user if they have an allowed team key."
			regHeader = "(No key for public circle; X-Hub-Key: <allowed_key> for a private circle)"
		}
	} else if server.service.config.SharedKey != "" {
		mode = "SEMI_OPEN"
		authSummary = "Required. This Hub is running in SEMI_OPEN mode; pre-shared key is required on registration."
		regHeader = "X-Hub-Key: <shared_key> (or Authorization: Bearer <shared_key>)"
	}

	content := strings.ReplaceAll(string(llmsText), "{{BASE_URL}}", server.baseURLFor(r))
	content = strings.ReplaceAll(content, "{{HUB_MODE}}", mode)
	content = strings.ReplaceAll(content, "{{HUB_REGISTRATION_AUTH}}", authSummary)
	content = strings.ReplaceAll(content, "{{HUB_REGISTRATION_HEADER}}", regHeader)

	_, _ = w.Write([]byte(content))
}

func (server *HTTPServer) installScriptHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	content := strings.ReplaceAll(string(installScript), "https://a2a.david888.com", server.baseURLFor(r))
	_, _ = w.Write([]byte(content))
}

func (server *HTTPServer) a2aBridgeScriptHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/x-python; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(a2aBridgeScript)
}

func (server *HTTPServer) baseURLFor(r *http.Request) string {
	if server.baseURL != "" {
		return strings.TrimRight(server.baseURL, "/")
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	parsed := url.URL{Scheme: scheme, Host: r.Host}
	return strings.TrimRight(parsed.String(), "/")
}

func (server *HTTPServer) systemCard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-cache")
	card := server.service.BuildSystemCard(server.baseURLFor(r))
	writeJSON(w, http.StatusOK, card)
}

func (server *HTTPServer) announcements(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "public, max-age=30")
	afterID, err := parseUintQuery(r, "afterId", 0)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_QUERY", "afterId must be a non-negative integer")
		return
	}
	limit, err := parseIntQuery(r, "limit", 20)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_QUERY", "limit must be an integer")
		return
	}
	items, next, err := server.service.ListActiveAnnouncements(r.Context(), afterID, limit)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"announcements": items, "nextId": next})
}

func (server *HTTPServer) adminAnnouncementsUI(w http.ResponseWriter, _ *http.Request) {
	nonceBytes := make([]byte, 18)
	if _, err := rand.Read(nonceBytes); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "admin interface is unavailable")
		return
	}
	nonce := base64.RawURLEncoding.EncodeToString(nonceBytes)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'nonce-"+nonce+"'; style-src 'unsafe-inline'; connect-src 'self'; base-uri 'none'; form-action 'none'")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(bytes.ReplaceAll(adminHTML, []byte("{{CSP_NONCE}}"), []byte(nonce)))
}

type announcementRequest struct {
	Status           string                   `json:"status"`
	Title            string                   `json:"title"`
	Summary          string                   `json:"summary"`
	Severity         hub.AnnouncementSeverity `json:"severity"`
	DocumentationURL string                   `json:"documentationUrl,omitempty"`
	ExpiresAt        *time.Time               `json:"expiresAt,omitempty"`
}

type groupCreateRequest struct {
	Name string `json:"name"`
}

type groupInviteRequest struct {
	AgentID string `json:"agentId"`
}

type groupOwnershipRequest struct {
	AgentID string `json:"agentId"`
}

type groupMessageRequest struct {
	ContextID      string `json:"contextId"`
	IdempotencyKey string `json:"idempotencyKey"`
	Message        string `json:"message"`
}

type groupCharterRequest struct {
	Content         string `json:"content"`
	ExpectedVersion int64  `json:"expectedVersion"`
	IdempotencyKey  string `json:"idempotencyKey"`
}

type groupCharterRollbackRequest struct {
	TargetVersion   int64  `json:"targetVersion"`
	ExpectedVersion int64  `json:"expectedVersion"`
	IdempotencyKey  string `json:"idempotencyKey"`
}

type groupSecretaryLeaseRequest struct {
	Epoch        int64 `json:"epoch"`
	LeaseSeconds int64 `json:"leaseSeconds"`
}

type groupSecretaryRevokeRequest struct {
	Epoch int64 `json:"epoch"`
}

type circleRotateKeyRequest struct {
	SharedKey    string `json:"sharedKey"`
	GraceSeconds int64  `json:"graceSeconds"`
}

func (request announcementRequest) input() hub.AnnouncementInput {
	return hub.AnnouncementInput{
		Title: request.Title, Summary: request.Summary, Severity: request.Severity,
		DocumentationURL: request.DocumentationURL, ExpiresAt: request.ExpiresAt,
	}
}

func (server *HTTPServer) adminListAnnouncements(w http.ResponseWriter, r *http.Request) {
	token, ok := server.operatorToken(w, r)
	if !ok {
		return
	}
	afterID, err := parseUintQuery(r, "afterId", 0)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_QUERY", "afterId must be a non-negative integer")
		return
	}
	limit, err := parseIntQuery(r, "limit", 100)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_QUERY", "limit must be an integer")
		return
	}
	items, next, err := server.service.ListAnnouncementAdmin(r.Context(), token, afterID, limit)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"announcements": items, "nextId": next})
}

func (server *HTTPServer) adminCreateAnnouncement(w http.ResponseWriter, r *http.Request) {
	token, ok := server.operatorToken(w, r)
	if !ok {
		return
	}
	var request announcementRequest
	if !decodeJSON(w, r, server.maxBodyBytes, &request) {
		return
	}
	if request.Status != "" && !strings.EqualFold(request.Status, string(hub.AnnouncementDraft)) && !strings.EqualFold(request.Status, string(hub.AnnouncementPublished)) {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "status must be DRAFT or PUBLISHED")
		return
	}
	var announcement hub.Announcement
	var err error
	if strings.EqualFold(request.Status, string(hub.AnnouncementDraft)) {
		announcement, err = server.service.CreateDraft(r.Context(), token, request.input())
	} else {
		announcement, err = server.service.PublishAnnouncement(r.Context(), token, request.input())
	}
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, announcement)
}

func (server *HTTPServer) adminUpdateDraft(w http.ResponseWriter, r *http.Request) {
	token, ok := server.operatorToken(w, r)
	if !ok {
		return
	}
	id, ok := parseAnnouncementID(w, r.PathValue("announcementId"))
	if !ok {
		return
	}
	var request announcementRequest
	if !decodeJSON(w, r, server.maxBodyBytes, &request) {
		return
	}
	announcement, err := server.service.UpdateDraft(r.Context(), token, id, request.input())
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, announcement)
}

func (server *HTTPServer) adminPublishDraft(w http.ResponseWriter, r *http.Request) {
	token, ok := server.operatorToken(w, r)
	if !ok {
		return
	}
	id, ok := parseAnnouncementID(w, r.PathValue("announcementId"))
	if !ok {
		return
	}
	if !decodeEmptyOrJSON(w, r, server.maxBodyBytes) {
		return
	}
	announcement, err := server.service.PublishDraft(r.Context(), token, id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, announcement)
}

func (server *HTTPServer) adminCreateRevision(w http.ResponseWriter, r *http.Request) {
	token, ok := server.operatorToken(w, r)
	if !ok {
		return
	}
	id, ok := parseAnnouncementID(w, r.PathValue("announcementId"))
	if !ok {
		return
	}
	var request announcementRequest
	if !decodeJSON(w, r, server.maxBodyBytes, &request) {
		return
	}
	announcement, err := server.service.CreateRevision(r.Context(), token, id, request.input())
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, announcement)
}

func (server *HTTPServer) operatorToken(w http.ResponseWriter, r *http.Request) (string, bool) {
	token, ok := bearerToken(r.Header.Get("Authorization"))
	if !ok || server.service.AuthenticateOperator(token) != nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication failed")
		return "", false
	}
	return token, true
}

func parseAnnouncementID(w http.ResponseWriter, value string) (uint64, bool) {
	return parsePositiveID(w, value, "announcementId")
}

func parsePositiveID(w http.ResponseWriter, value, name string) (uint64, bool) {
	id, err := strconv.ParseUint(value, 10, 64)
	if err != nil || id == 0 {
		writeError(w, http.StatusBadRequest, "INVALID_PATH", name+" must be a positive integer")
		return 0, false
	}
	return id, true
}

func (server *HTTPServer) health(w http.ResponseWriter, r *http.Request) {
	if _, err := server.service.Status(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "hub is not ready")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (server *HTTPServer) status(w http.ResponseWriter, r *http.Request) {
	var status HubStatus
	var err error
	if strings.TrimSpace(r.Header.Get("X-Agent-ID")) != "" {
		agentID, token, ok := server.agentCredentials(w, r, "")
		if !ok {
			return
		}
		status, err = server.service.StatusForAgent(r.Context(), agentID, token)
	} else if bearer, ok := bearerToken(r.Header.Get("Authorization")); ok {
		if server.service.AuthenticateOperator(bearer) != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication failed")
			return
		}
		status, err = server.service.StatusForOperator(r.Context(), bearer)
	} else {
		status, err = server.service.Status(r.Context())
	}
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (server *HTTPServer) register(w http.ResponseWriter, r *http.Request) {
	sharedKey := sharedKeyFromRequest(r)
	if server.service.circleResolver.Mode() != "multi" && !server.verifySharedKey(r) {
		writeError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "shared key required or invalid")
		return
	}
	if !server.registrationLimiter.allow(clientIP(r)) {
		writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", "registration rate limit exceeded")
		return
	}
	var declaration hub.AgentDeclaration
	if !decodeJSON(w, r, server.maxBodyBytes, &declaration) {
		return
	}
	identity, duplicate, err := server.service.RegisterWithSharedKey(r.Context(), declaration, sharedKey)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	metadata, err := server.service.HubMetadata(r.Context(), server.baseURLFor(r))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	policy, err := server.service.store.Policy().GetPolicy(r.Context())
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"identity":  identity,
		"policy":    policy,
		"hub":       metadata,
		"duplicate": duplicate,
	})
}

func (server *HTTPServer) listAgents(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, "")
	if !ok {
		return
	}
	stateFilter := r.URL.Query().Get("state")
	agents, err := server.service.ListAgents(r.Context(), agentID, token, server.baseURLFor(r), stateFilter)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"agents": agents})
}

func (server *HTTPServer) getAgent(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, "")
	if !ok {
		return
	}
	view, err := server.service.GetAgent(r.Context(), agentID, token, r.PathValue("agentId"), server.baseURLFor(r))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (server *HTTPServer) agentCard(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, "")
	if !ok {
		return
	}
	view, err := server.service.GetAgent(r.Context(), agentID, token, r.PathValue("agentId"), server.baseURLFor(r))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view.Card)
}

func (server *HTTPServer) heartbeat(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, r.PathValue("agentId"))
	if !ok {
		return
	}
	if !decodeEmptyOrJSON(w, r, server.maxBodyBytes) {
		return
	}
	view, err := server.service.Heartbeat(r.Context(), agentID, token, server.baseURLFor(r))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"agent": view})
}

func (server *HTTPServer) disconnect(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, r.PathValue("agentId"))
	if !ok {
		return
	}
	if !decodeEmptyOrJSON(w, r, server.maxBodyBytes) {
		return
	}
	if err := server.service.Disconnect(r.Context(), agentID, token); err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"agentId": agentID, "state": string(hub.AgentStateOffline)})
}

func (server *HTTPServer) sendTask(w http.ResponseWriter, r *http.Request) {
	requesterID, token, ok := server.agentCredentials(w, r, "")
	if !ok {
		return
	}
	if !server.taskLimiter.allow(requesterID) {
		writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", "task rate limit exceeded")
		return
	}
	var task hub.TaskDelivery
	if !decodeJSON(w, r, server.maxBodyBytes, &task) {
		return
	}
	task.TargetAgentID = r.PathValue("targetAgentId")
	if strings.TrimSpace(task.TaskID) == "" {
		task.TaskID = fmt.Sprintf("task-%d", time.Now().UnixNano())
	}
	if strings.TrimSpace(task.ContextID) == "" {
		task.ContextID = fmt.Sprintf("ctx-%d", time.Now().UnixNano())
	}
	if strings.TrimSpace(task.IdempotencyKey) == "" {
		task.IdempotencyKey = fmt.Sprintf("idem-%s", task.TaskID)
	}
	item, duplicate, err := server.service.SendTask(r.Context(), requesterID, token, task)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	status := "PENDING"
	if item.State != "" {
		status = string(item.State)
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"taskId":          item.TaskID,
		"contextId":       item.ContextID,
		"targetAgentId":   item.TargetAgentID,
		"state":           status,
		"deliveryOutcome": deliveryOutcome(duplicate),
		"sequence":        item.Sequence,
	})
}

func (server *HTTPServer) pollInbox(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, r.PathValue("agentId"))
	if !ok {
		return
	}
	after, err := parseUintQuery(r, "afterSequence", 0)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_QUERY", "afterSequence must be a non-negative integer")
		return
	}
	limit, err := parseIntQuery(r, "limit", 100)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_QUERY", "limit must be an integer")
		return
	}
	items, err := server.service.Poll(r.Context(), agentID, token, after, limit)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	next := after
	if len(items) > 0 {
		next = items[len(items)-1].Sequence
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "nextSequence": next})
}

func (server *HTTPServer) streamInbox(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, r.PathValue("agentId"))
	if !ok {
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "STREAMING_UNSUPPORTED", "streaming is not supported by the underlying transport")
		return
	}

	after, err := parseUintQuery(r, "afterSequence", 0)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_QUERY", "afterSequence must be a non-negative integer")
		return
	}
	if after == 0 {
		if lastEventID := strings.TrimSpace(r.Header.Get("Last-Event-ID")); lastEventID != "" {
			if parsed, parseErr := strconv.ParseUint(lastEventID, 10, 64); parseErr == nil {
				after = parsed
			}
		}
	}

	if _, err := server.service.AuthenticateAgent(r.Context(), agentID, token); err != nil {
		writeServiceError(w, err)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	sub := server.service.Broker().Subscribe(agentID, 64)
	defer server.service.Broker().Unsubscribe(sub)

	// Catch-up: query pending unacknowledged items from SQLite
	items, err := server.service.Poll(r.Context(), agentID, token, after, 100)
	if err == nil {
		for _, item := range items {
			if item.Sequence > after {
				after = item.Sequence
			}
			data, jsonErr := json.Marshal(item)
			if jsonErr != nil {
				continue
			}
			_, _ = fmt.Fprintf(w, "id: %d\nevent: task\ndata: %s\n\n", item.Sequence, data)
			flusher.Flush()
		}
	}

	keepaliveTicker := time.NewTicker(15 * time.Second)
	defer keepaliveTicker.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case item := <-sub.C:
			if item.Sequence <= after {
				continue
			}
			after = item.Sequence
			data, jsonErr := json.Marshal(item)
			if jsonErr != nil {
				continue
			}
			_, _ = fmt.Fprintf(w, "id: %d\nevent: task\ndata: %s\n\n", item.Sequence, data)
			flusher.Flush()
		case <-keepaliveTicker.C:
			_, _ = fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
			_, _ = server.service.Heartbeat(ctx, agentID, token, "")
		}
	}
}

func (server *HTTPServer) ackInbox(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, r.PathValue("agentId"))
	if !ok {
		return
	}
	if !decodeEmptyOrJSON(w, r, server.maxBodyBytes) {
		return
	}
	sequence, err := strconv.ParseUint(r.PathValue("sequence"), 10, 64)
	if err != nil || sequence == 0 {
		writeError(w, http.StatusBadRequest, "INVALID_PATH", "sequence must be a positive integer")
		return
	}
	if err := server.service.Acknowledge(r.Context(), agentID, token, sequence); err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sequence": sequence, "state": string(hub.DeliveryStateAcknowledged)})
}

func (server *HTTPServer) listGroups(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, "")
	if !ok {
		return
	}
	groups, err := server.service.ListGroups(r.Context(), agentID, token)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"groups": groups})
}

func (server *HTTPServer) createGroup(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, "")
	if !ok {
		return
	}
	var request groupCreateRequest
	if !decodeJSON(w, r, server.maxBodyBytes, &request) {
		return
	}
	group, err := server.service.CreateGroup(r.Context(), agentID, token, hub.CreateGroupInput{Name: request.Name})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, group)
}

func (server *HTTPServer) getGroup(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, "")
	if !ok {
		return
	}
	group, members, err := server.service.GetGroup(r.Context(), agentID, token, r.PathValue("groupId"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"group": group, "members": members})
}

func (server *HTTPServer) inviteGroupMember(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, "")
	if !ok {
		return
	}
	var request groupInviteRequest
	if !decodeJSON(w, r, server.maxBodyBytes, &request) {
		return
	}
	if strings.TrimSpace(request.AgentID) == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "agentId is required")
		return
	}
	invitation, err := server.service.InviteMember(r.Context(), agentID, token, r.PathValue("groupId"), request.AgentID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, invitation)
}

func (server *HTTPServer) listGroupInvitations(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, "")
	if !ok {
		return
	}
	invitations, err := server.service.ListInvitations(r.Context(), agentID, token)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"invitations": invitations})
}

func (server *HTTPServer) acceptGroupInvitation(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, "")
	if !ok {
		return
	}
	invitationID, ok := parsePositiveID(w, r.PathValue("invitationId"), "invitationId")
	if !ok {
		return
	}
	if !decodeEmptyOrJSON(w, r, server.maxBodyBytes) {
		return
	}
	member, err := server.service.AcceptInvitation(r.Context(), agentID, token, invitationID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, member)
}

func (server *HTTPServer) acceptGroupInvitationByGroup(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, "")
	if !ok {
		return
	}
	if !decodeEmptyOrJSON(w, r, server.maxBodyBytes) {
		return
	}
	member, err := server.service.AcceptInvitationByGroup(r.Context(), agentID, token, r.PathValue("groupId"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, member)
}

func (server *HTTPServer) leaveGroup(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, "")
	if !ok {
		return
	}
	if !decodeEmptyOrJSON(w, r, server.maxBodyBytes) {
		return
	}
	groupID := r.PathValue("groupId")
	if err := server.service.LeaveGroup(r.Context(), agentID, token, groupID); err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"groupId": groupID, "state": string(hub.MembershipLeft)})
}

func (server *HTTPServer) archiveGroup(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, "")
	if !ok {
		return
	}
	if !decodeEmptyOrJSON(w, r, server.maxBodyBytes) {
		return
	}
	groupID := r.PathValue("groupId")
	if err := server.service.ArchiveGroup(r.Context(), agentID, token, groupID); err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"groupId": groupID, "state": string(hub.GroupStateArchived)})
}

func (server *HTTPServer) transferGroupOwnership(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, "")
	if !ok {
		return
	}
	var request groupOwnershipRequest
	if !decodeJSON(w, r, server.maxBodyBytes, &request) {
		return
	}
	if strings.TrimSpace(request.AgentID) == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "agentId is required")
		return
	}
	groupID := r.PathValue("groupId")
	if err := server.service.TransferOwnership(r.Context(), agentID, token, groupID, request.AgentID); err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"groupId": groupID, "ownerAgentId": request.AgentID})
}

func (server *HTTPServer) removeGroupMember(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, "")
	if !ok {
		return
	}
	if !decodeEmptyOrJSON(w, r, server.maxBodyBytes) {
		return
	}
	groupID, targetID := r.PathValue("groupId"), r.PathValue("agentId")
	if err := server.service.RemoveMember(r.Context(), agentID, token, groupID, targetID); err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"groupId": groupID, "agentId": targetID, "state": string(hub.MembershipRemoved)})
}

func (server *HTTPServer) groupRoster(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, "")
	if !ok {
		return
	}
	members, err := server.service.GroupRoster(r.Context(), agentID, token, r.PathValue("groupId"), server.baseURLFor(r))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"groupId": r.PathValue("groupId"), "members": members})
}

func (server *HTTPServer) groupHistory(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, "")
	if !ok {
		return
	}
	afterID, err := parseUintQuery(r, "afterId", 0)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_QUERY", "afterId must be a non-negative integer")
		return
	}
	limit, err := parseIntQuery(r, "limit", server.service.maxGroupHistoryPage())
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_QUERY", "limit must be an integer")
		return
	}
	messages, next, err := server.service.GroupHistory(r.Context(), agentID, token, r.PathValue("groupId"), server.baseURLFor(r), afterID, limit)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"groupId": r.PathValue("groupId"), "messages": messages, "nextId": next})
}

func (server *HTTPServer) getGroupCharter(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, "")
	if !ok {
		return
	}
	charter, err := server.service.GetGroupCharter(r.Context(), agentID, token, r.PathValue("groupId"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, charter)
}

func (server *HTTPServer) listGroupCharterRevisions(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, "")
	if !ok {
		return
	}
	revisions, err := server.service.ListGroupCharterRevisions(r.Context(), agentID, token, r.PathValue("groupId"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, map[string]any{"groupId": r.PathValue("groupId"), "revisions": revisions})
}

func (server *HTTPServer) putGroupCharter(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, "")
	if !ok {
		return
	}
	var request groupCharterRequest
	if !decodeJSON(w, r, server.maxBodyBytes, &request) {
		return
	}
	charter, duplicate, err := server.service.PutGroupCharter(r.Context(), agentID, token, r.PathValue("groupId"), hub.GroupCharterInput{Content: request.Content, ExpectedVersion: request.ExpectedVersion, IdempotencyKey: request.IdempotencyKey})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	status := http.StatusOK
	if !duplicate {
		status = http.StatusCreated
	}
	writeJSON(w, status, charter)
}

func (server *HTTPServer) rollbackGroupCharter(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, "")
	if !ok {
		return
	}
	var request groupCharterRollbackRequest
	if !decodeJSON(w, r, server.maxBodyBytes, &request) {
		return
	}
	charter, duplicate, err := server.service.RollbackGroupCharter(r.Context(), agentID, token, r.PathValue("groupId"), request.ExpectedVersion, request.TargetVersion, request.IdempotencyKey)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	status := http.StatusOK
	if !duplicate {
		status = http.StatusCreated
	}
	writeJSON(w, status, charter)
}

func (server *HTTPServer) getGroupSecretary(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, "")
	if !ok {
		return
	}
	secretary, err := server.service.GetGroupSecretary(r.Context(), agentID, token, r.PathValue("groupId"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, secretary)
}

func (server *HTTPServer) appointGroupSecretary(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, "")
	if !ok {
		return
	}
	var request hub.GroupSecretaryAppointmentInput
	if !decodeJSON(w, r, server.maxBodyBytes, &request) {
		return
	}
	secretary, err := server.service.AppointGroupSecretary(r.Context(), agentID, token, r.PathValue("groupId"), request.AgentID, request.ExpectedEpoch, request.LeaseSeconds)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusCreated, secretary)
}

func (server *HTTPServer) renewGroupSecretary(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, "")
	if !ok {
		return
	}
	var request groupSecretaryLeaseRequest
	if !decodeJSON(w, r, server.maxBodyBytes, &request) {
		return
	}
	secretary, err := server.service.RenewGroupSecretary(r.Context(), agentID, token, r.PathValue("groupId"), request.Epoch, request.LeaseSeconds)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, secretary)
}

func (server *HTTPServer) revokeGroupSecretary(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, "")
	if !ok {
		return
	}
	var request groupSecretaryRevokeRequest
	if !decodeJSON(w, r, server.maxBodyBytes, &request) {
		return
	}
	if err := server.service.RevokeGroupSecretary(r.Context(), agentID, token, r.PathValue("groupId"), request.Epoch); err != nil {
		writeServiceError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, map[string]any{"groupId": r.PathValue("groupId"), "epoch": request.Epoch, "state": hub.SecretaryRevoked})
}

func (server *HTTPServer) sendGroupMessage(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, "")
	if !ok {
		return
	}
	if !server.taskLimiter.allow(agentID) {
		writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", "group message rate limit exceeded")
		return
	}
	var request groupMessageRequest
	if !decodeJSON(w, r, server.maxBodyBytes, &request) {
		return
	}
	if strings.TrimSpace(request.ContextID) == "" {
		request.ContextID = fmt.Sprintf("ctx-%d", time.Now().UnixNano())
	}
	if strings.TrimSpace(request.IdempotencyKey) == "" {
		request.IdempotencyKey = fmt.Sprintf("idem-%d", time.Now().UnixNano())
	}
	message, duplicate, err := server.service.SendGroupMessage(r.Context(), agentID, token, r.PathValue("groupId"), hub.GroupMessageInput{
		ContextID: request.ContextID, IdempotencyKey: request.IdempotencyKey, Message: request.Message,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	outcome := "QUEUED"
	if duplicate {
		outcome = "DUPLICATE"
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"message": message, "deliveryOutcome": outcome})
}

func (server *HTTPServer) setRegistration(w http.ResponseWriter, r *http.Request) {
	if !server.operatorCredentials(w, r) {
		return
	}
	var request struct {
		Enabled *bool `json:"enabled"`
	}
	if !decodeJSON(w, r, server.maxBodyBytes, &request) {
		return
	}
	if request.Enabled == nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "enabled is required")
		return
	}
	if err := server.service.SetRegistrationEnabled(r.Context(), *request.Enabled); err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"registrationEnabled": *request.Enabled})
}

func (server *HTTPServer) revokeAgent(w http.ResponseWriter, r *http.Request) {
	if !server.operatorCredentials(w, r) {
		return
	}
	var request struct {
		Reason string `json:"reason"`
	}
	if !decodeJSON(w, r, server.maxBodyBytes, &request) {
		return
	}
	if err := server.service.Revoke(r.Context(), r.PathValue("agentId"), request.Reason); err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"agentId": r.PathValue("agentId"), "state": string(hub.AgentStateRevoked)})
}

func (server *HTTPServer) adminListAgents(w http.ResponseWriter, r *http.Request) {
	token, ok := server.operatorToken(w, r)
	if !ok {
		return
	}
	agents, err := server.service.ListAgentsAdmin(r.Context(), token, strings.TrimSpace(r.URL.Query().Get("circleId")))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	onlineCount := 0
	offlineCount := 0
	for _, a := range agents {
		if a.IsOnline {
			onlineCount++
		} else {
			offlineCount++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"agents":       agents,
		"total":        len(agents),
		"onlineCount":  onlineCount,
		"offlineCount": offlineCount,
	})
}

func (server *HTTPServer) adminListCircles(w http.ResponseWriter, r *http.Request) {
	token, ok := server.operatorToken(w, r)
	if !ok {
		return
	}
	circles, err := server.service.ListCirclesAdmin(r.Context(), token)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"circles": circles})
}

func (server *HTTPServer) adminDisableCircle(w http.ResponseWriter, r *http.Request) {
	token, ok := server.operatorToken(w, r)
	if !ok {
		return
	}
	revoked, err := server.service.DisableCircle(r.Context(), token, r.PathValue("circleId"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"circleId": r.PathValue("circleId"), "state": hub.CircleStateDisabled, "revokedAgents": revoked})
}

func (server *HTTPServer) adminRotateCircleKey(w http.ResponseWriter, r *http.Request) {
	token, ok := server.operatorToken(w, r)
	if !ok {
		return
	}
	var request circleRotateKeyRequest
	if !decodeJSON(w, r, server.maxBodyBytes, &request) {
		return
	}
	if request.GraceSeconds < 0 || request.GraceSeconds > 30*24*60*60 {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "graceSeconds must be between 0 and 2592000")
		return
	}
	circleRecord, err := server.service.RotateCircleKey(r.Context(), token, r.PathValue("circleId"), strings.TrimSpace(request.SharedKey), time.Duration(request.GraceSeconds)*time.Second)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, circleRecord)
}

func (server *HTTPServer) adminRevokeCircleKey(w http.ResponseWriter, r *http.Request) {
	token, ok := server.operatorToken(w, r)
	if !ok {
		return
	}
	version, err := strconv.Atoi(r.PathValue("version"))
	if err != nil || version < 1 {
		writeError(w, http.StatusBadRequest, "INVALID_PATH", "version must be a positive integer")
		return
	}
	if err := server.service.RevokeCircleKey(r.Context(), token, r.PathValue("circleId"), version); err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"circleId": r.PathValue("circleId"), "version": version, "state": "REVOKED"})
}

func (server *HTTPServer) adminDeleteAgent(w http.ResponseWriter, r *http.Request) {
	token, ok := server.operatorToken(w, r)
	if !ok {
		return
	}
	agentID := r.PathValue("agentId")
	if err := server.service.DeleteAgent(r.Context(), token, agentID); err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"agentId": agentID, "status": "DELETED"})
}

func (server *HTTPServer) adminPruneAgents(w http.ResponseWriter, r *http.Request) {
	token, ok := server.operatorToken(w, r)
	if !ok {
		return
	}
	pruned, err := server.service.PruneInactiveAgents(r.Context(), token)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"prunedCount": pruned})
}

func (server *HTTPServer) cancelTask(w http.ResponseWriter, r *http.Request) {
	if !server.operatorCredentials(w, r) {
		return
	}
	var request struct {
		Reason string `json:"reason"`
	}
	if !decodeJSON(w, r, server.maxBodyBytes, &request) {
		return
	}
	if err := server.service.CancelTask(r.Context(), r.PathValue("taskId"), request.Reason); err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"taskId": r.PathValue("taskId"), "state": string(hub.DeliveryStateCanceled)})
}

func (server *HTTPServer) listEvents(w http.ResponseWriter, r *http.Request) {
	token, ok := bearerToken(r.Header.Get("Authorization"))
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication failed")
		return
	}
	afterID, err := parseUintQuery(r, "afterId", 0)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_QUERY", "afterId must be a non-negative integer")
		return
	}
	limit, err := parseIntQuery(r, "limit", 100)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_QUERY", "limit must be an integer")
		return
	}
	events, err := server.service.ListEvents(r.Context(), token, afterID, limit, strings.TrimSpace(r.URL.Query().Get("circleId")))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	next := afterID
	if len(events) > 0 {
		next = events[len(events)-1].ID
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events, "nextId": next})
}

func (server *HTTPServer) adminListMessages(w http.ResponseWriter, r *http.Request) {
	token, ok := server.operatorToken(w, r)
	if !ok {
		return
	}
	msgType := strings.TrimSpace(r.URL.Query().Get("type"))
	agentID := strings.TrimSpace(r.URL.Query().Get("agentId"))
	groupID := strings.TrimSpace(r.URL.Query().Get("groupId"))
	beforeSequence, _ := parseUintQuery(r, "beforeSequence", 0)
	beforeID, _ := parseUintQuery(r, "beforeId", 0)
	limit, err := parseIntQuery(r, "limit", 50)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_QUERY", "limit must be an integer")
		return
	}
	messages, err := server.service.ListMessagesAdmin(r.Context(), token, msgType, beforeSequence, beforeID, limit, groupID, agentID, strings.TrimSpace(r.URL.Query().Get("circleId")))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, messages)
}

func (server *HTTPServer) verifySharedKey(r *http.Request) bool {
	sharedKey := server.service.config.SharedKey
	if sharedKey == "" {
		return true
	}
	candidate := sharedKeyFromRequest(r)
	if candidate == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(candidate), []byte(sharedKey)) == 1
}

func sharedKeyFromRequest(r *http.Request) string {
	candidate := strings.TrimSpace(r.Header.Get("X-Hub-Key"))
	if candidate == "" {
		candidate = strings.TrimSpace(r.Header.Get("X-Shared-Key"))
	}
	if candidate == "" {
		candidate = strings.TrimSpace(r.Header.Get("X-A2A-Key"))
	}
	if candidate == "" {
		candidate = strings.TrimSpace(r.URL.Query().Get("hubKey"))
	}
	if candidate == "" {
		candidate = strings.TrimSpace(r.URL.Query().Get("hub_key"))
	}
	if candidate == "" {
		candidate = strings.TrimSpace(r.URL.Query().Get("sharedKey"))
	}
	if candidate == "" {
		candidate = strings.TrimSpace(r.URL.Query().Get("shared_key"))
	}
	if candidate == "" {
		candidate = strings.TrimSpace(r.URL.Query().Get("key"))
	}
	if candidate == "" {
		if bearer, ok := bearerToken(r.Header.Get("Authorization")); ok {
			candidate = bearer
		}
	}
	return candidate
}

func (server *HTTPServer) agentCredentials(w http.ResponseWriter, r *http.Request, pathAgentID string) (string, string, bool) {
	if server.service.circleResolver.Mode() != "multi" && server.service.config.SharedKey != "" {
		candidate := strings.TrimSpace(r.Header.Get("X-Hub-Key"))
		if candidate == "" {
			candidate = strings.TrimSpace(r.Header.Get("X-Shared-Key"))
		}
		if candidate == "" {
			candidate = strings.TrimSpace(r.Header.Get("X-A2A-Key"))
		}
		if candidate == "" {
			candidate = strings.TrimSpace(r.URL.Query().Get("hubKey"))
		}
		if candidate == "" {
			candidate = strings.TrimSpace(r.URL.Query().Get("hub_key"))
		}
		if candidate == "" {
			candidate = strings.TrimSpace(r.URL.Query().Get("sharedKey"))
		}
		if candidate == "" {
			candidate = strings.TrimSpace(r.URL.Query().Get("shared_key"))
		}
		if candidate == "" {
			candidate = strings.TrimSpace(r.URL.Query().Get("key"))
		}
		if candidate != "" && subtle.ConstantTimeCompare([]byte(candidate), []byte(server.service.config.SharedKey)) != 1 {
			writeError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "shared key invalid")
			return "", "", false
		}
	}
	agentID := strings.TrimSpace(r.Header.Get("X-Agent-ID"))
	if agentID == "" || (pathAgentID != "" && agentID != pathAgentID) {
		writeError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication failed")
		return "", "", false
	}
	token, ok := bearerToken(r.Header.Get("Authorization"))
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication failed")
		return "", "", false
	}
	if _, err := server.service.AuthenticateAgent(r.Context(), agentID, token); err != nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication failed")
		return "", "", false
	}
	return agentID, token, true
}

func (server *HTTPServer) operatorCredentials(w http.ResponseWriter, r *http.Request) bool {
	_, ok := server.operatorToken(w, r)
	return ok
}

func decodeJSON(w http.ResponseWriter, r *http.Request, maxBytes int64, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "request body is invalid")
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "request body must contain one JSON value")
		return false
	}
	return true
}

func decodeEmptyOrJSON(w http.ResponseWriter, r *http.Request, maxBytes int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	decoder := json.NewDecoder(r.Body)
	var value any
	if err := decoder.Decode(&value); err == io.EOF {
		return true
	} else if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "request body is invalid")
		return false
	}
	if err := decoder.Decode(&value); err != io.EOF {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "request body must contain one JSON value")
		return false
	}
	return true
}

func bearerToken(value string) (string, bool) {
	parts := strings.Fields(value)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return "", false
	}
	return parts[1], true
}

func parseUintQuery(r *http.Request, name string, fallback uint64) (uint64, error) {
	value := r.URL.Query().Get(name)
	if value == "" {
		return fallback, nil
	}
	return strconv.ParseUint(value, 10, 64)
}

func parseIntQuery(r *http.Request, name string, fallback int) (int, error) {
	value := r.URL.Query().Get(name)
	if value == "" {
		return fallback, nil
	}
	return strconv.Atoi(value)
}

func deliveryOutcome(duplicate bool) string {
	if duplicate {
		return "DUPLICATE"
	}
	return "QUEUED"
}

func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrUnauthenticated):
		writeError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication failed")
	case errors.Is(err, ErrRegistrationDisabled):
		writeError(w, http.StatusForbidden, "REGISTRATION_DISABLED", "registration is disabled")
	case errors.Is(err, ErrCircleDisabled):
		writeError(w, http.StatusForbidden, "CIRCLE_DISABLED", "circle is disabled")
	case errors.Is(err, ErrCircleKeyInactive):
		writeError(w, http.StatusUnauthorized, "CIRCLE_KEY_INACTIVE", "shared key is inactive")
	case errors.Is(err, ErrAgentLimit):
		writeError(w, http.StatusTooManyRequests, "AGENT_LIMIT_REACHED", "agent limit reached")
	case errors.Is(err, ErrTaskLimit):
		writeError(w, http.StatusTooManyRequests, "TASK_LIMIT_REACHED", "task limit reached")
	case errors.Is(err, ErrGroupLimit):
		writeError(w, http.StatusTooManyRequests, "GROUP_LIMIT_REACHED", "group limit reached")
	case errors.Is(err, ErrGroupArchived):
		writeError(w, http.StatusConflict, "GROUP_ARCHIVED", "group is archived")
	case errors.Is(err, ErrGroupUnavailable):
		writeError(w, http.StatusNotFound, "GROUP_UNAVAILABLE", "group is unavailable")
	case errors.Is(err, ErrInvitationInvalid):
		writeError(w, http.StatusConflict, "INVITATION_INVALID", "invitation is invalid")
	case errors.Is(err, store.ErrInvalidState):
		writeError(w, http.StatusConflict, "INVALID_STATE", "resource state does not allow this operation")
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "CONFLICT", "resource was changed concurrently")
	case errors.Is(err, store.ErrForbidden):
		writeError(w, http.StatusForbidden, "FORBIDDEN", "operation is not permitted")
	case errors.Is(err, ErrAgentUnavailable):
		writeError(w, http.StatusNotFound, "AGENT_UNAVAILABLE", "agent is unavailable")
	case errors.Is(err, ErrForbidden):
		writeError(w, http.StatusForbidden, "FORBIDDEN", "operation is not permitted")
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "resource not found")
	case errors.Is(err, store.ErrCanceled):
		writeError(w, http.StatusConflict, "TASK_CANCELED", "task is canceled")
	case errors.Is(err, ErrValidation):
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "hub operation failed")
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

type requestLimiter struct {
	mu       sync.Mutex
	max      int
	window   time.Duration
	requests map[string][]time.Time
}

type keyedConcurrencyLimiter struct {
	mu     sync.Mutex
	max    int
	active map[string]int
}

func newKeyedConcurrencyLimiter(max int) *keyedConcurrencyLimiter {
	if max < 1 {
		max = 1
	}
	return &keyedConcurrencyLimiter{max: max, active: make(map[string]int)}
}

func (limiter *keyedConcurrencyLimiter) acquire(key string) bool {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	if limiter.active[key] >= limiter.max {
		return false
	}
	limiter.active[key]++
	return true
}

func (limiter *keyedConcurrencyLimiter) release(key string) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	if limiter.active[key] <= 1 {
		delete(limiter.active, key)
		return
	}
	limiter.active[key]--
}

func newRequestLimiter(max int, window time.Duration) *requestLimiter {
	return &requestLimiter{max: max, window: window, requests: make(map[string][]time.Time)}
}

func (limiter *requestLimiter) allow(key string) bool {
	now := time.Now()
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	requests := limiter.requests[key]
	cutoff := now.Add(-limiter.window)
	first := 0
	for first < len(requests) && requests[first].Before(cutoff) {
		first++
	}
	requests = requests[first:]
	if len(requests) == 0 {
		delete(limiter.requests, key)
	}
	if len(requests) >= limiter.max {
		limiter.requests[key] = requests
		return false
	}
	limiter.requests[key] = append(requests, now)
	if len(limiter.requests) > 10000 {
		for k, v := range limiter.requests {
			if len(v) == 0 || v[len(v)-1].Before(cutoff) {
				delete(limiter.requests, k)
			}
		}
	}
	return true
}
