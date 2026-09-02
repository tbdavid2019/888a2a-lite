package service

import (
	"bytes"
	"crypto/rand"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
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

type HTTPServer struct {
	service             *Service
	maxBodyBytes        int64
	baseURL             string
	registrationLimiter *requestLimiter
	taskLimiter         *requestLimiter
}

func NewHTTPServer(service *Service) *HTTPServer {
	return &HTTPServer{
		service:             service,
		maxBodyBytes:        service.config.MaxPayloadBytes,
		baseURL:             service.config.PublicBaseURL,
		registrationLimiter: newRequestLimiter(service.config.RegistrationPerMinute, time.Minute),
		taskLimiter:         newRequestLimiter(service.config.MaxTasksPerMinute, time.Minute),
	}
}

func (server *HTTPServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", server.health)
	mux.HandleFunc("GET /llms.txt", server.llms)
	mux.HandleFunc("GET /admin/announcements", server.adminAnnouncementsUI)
	mux.HandleFunc("GET /hub/v1/system-card.json", server.systemCard)
	mux.HandleFunc("GET /hub/v1/announcements", server.announcements)
	mux.HandleFunc("GET /hub/v1/admin/announcements", server.adminListAnnouncements)
	mux.HandleFunc("POST /hub/v1/admin/announcements", server.adminCreateAnnouncement)
	mux.HandleFunc("PATCH /hub/v1/admin/announcements/{announcementId}", server.adminUpdateDraft)
	mux.HandleFunc("POST /hub/v1/admin/announcements/{announcementId}/publish", server.adminPublishDraft)
	mux.HandleFunc("POST /hub/v1/admin/announcements/{announcementId}/revision", server.adminCreateRevision)
	mux.HandleFunc("GET /hub/v1/admin/events", server.listEvents)
	mux.HandleFunc("GET /hub/v1/status", server.status)
	mux.HandleFunc("POST /hub/v1/agents/register", server.register)
	mux.HandleFunc("GET /hub/v1/agents", server.listAgents)
	mux.HandleFunc("GET /hub/v1/agents/{agentId}", server.getAgent)
	mux.HandleFunc("GET /hub/v1/agents/{agentId}/agent-card.json", server.agentCard)
	mux.HandleFunc("POST /hub/v1/agents/{agentId}/heartbeat", server.heartbeat)
	mux.HandleFunc("POST /hub/v1/agents/{agentId}/disconnect", server.disconnect)
	mux.HandleFunc("POST /hub/v1/agents/{targetAgentId}/tasks", server.sendTask)
	mux.HandleFunc("GET /hub/v1/agents/{agentId}/inbox", server.pollInbox)
	mux.HandleFunc("POST /hub/v1/agents/{agentId}/inbox/{sequence}/ack", server.ackInbox)
	mux.HandleFunc("GET /hub/v1/groups", server.listGroups)
	mux.HandleFunc("POST /hub/v1/groups", server.createGroup)
	mux.HandleFunc("GET /hub/v1/groups/invitations", server.listGroupInvitations)
	mux.HandleFunc("POST /hub/v1/groups/invitations/{invitationId}/accept", server.acceptGroupInvitation)
	mux.HandleFunc("GET /hub/v1/groups/{groupId}", server.getGroup)
	mux.HandleFunc("POST /hub/v1/groups/{groupId}/invitations", server.inviteGroupMember)
	mux.HandleFunc("POST /hub/v1/groups/{groupId}/leave", server.leaveGroup)
	mux.HandleFunc("POST /hub/v1/groups/{groupId}/archive", server.archiveGroup)
	mux.HandleFunc("POST /hub/v1/groups/{groupId}/ownership", server.transferGroupOwnership)
	mux.HandleFunc("POST /hub/v1/groups/{groupId}/members/{agentId}/remove", server.removeGroupMember)
	mux.HandleFunc("GET /hub/v1/groups/{groupId}/roster", server.groupRoster)
	mux.HandleFunc("GET /hub/v1/groups/{groupId}/history", server.groupHistory)
	mux.HandleFunc("POST /hub/v1/groups/{groupId}/messages", server.sendGroupMessage)
	mux.HandleFunc("POST /hub/v1/admin/registration", server.setRegistration)
	mux.HandleFunc("POST /hub/v1/admin/agents/{agentId}/revoke", server.revokeAgent)
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
	_, _ = w.Write([]byte(strings.ReplaceAll(string(llmsText), "{{BASE_URL}}", server.baseURLFor(r))))
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
	status, err := server.service.Status(r.Context())
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (server *HTTPServer) register(w http.ResponseWriter, r *http.Request) {
	if !server.registrationLimiter.allow(clientIP(r)) {
		writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", "registration rate limit exceeded")
		return
	}
	var declaration hub.AgentDeclaration
	if !decodeJSON(w, r, server.maxBodyBytes, &declaration) {
		return
	}
	identity, duplicate, err := server.service.Register(r.Context(), declaration)
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
	agents, err := server.service.ListAgents(r.Context(), agentID, token, server.baseURLFor(r))
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
	events, err := server.service.ListEvents(r.Context(), token, afterID, limit)
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

func (server *HTTPServer) agentCredentials(w http.ResponseWriter, r *http.Request, pathAgentID string) (string, string, bool) {
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
