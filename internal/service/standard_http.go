package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tbdavid2019/888a2a-lite/internal/a2a"
	"github.com/tbdavid2019/888a2a-lite/internal/hub"
	"github.com/tbdavid2019/888a2a-lite/internal/store"
)

func (server *HTTPServer) standardGatewayCard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "public, max-age=60")
	writeJSON(w, http.StatusOK, server.service.StandardGatewayCard(server.baseURLFor(r)))
}

func (server *HTTPServer) standardAgentCard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	token, ok := bearerToken(r.Header.Get("Authorization"))
	if !ok {
		writeStandardError(w, &StandardError{HTTPStatus: http.StatusUnauthorized, Reason: "UNAUTHENTICATED", Message: "authentication failed"})
		return
	}
	card, err := server.service.StandardAgentCard(r.Context(), token, r.PathValue("agentId"), server.baseURLFor(r))
	if err != nil {
		writeStandardServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, card)
}

func (server *HTTPServer) standardRequestChecks(w http.ResponseWriter, r *http.Request) bool {
	w.Header().Set("A2A-Version", a2a.ProtocolVersion)
	if err := a2a.ValidateContentType(r.Header.Get("Content-Type")); err != nil {
		writeStandardError(w, &StandardError{HTTPStatus: http.StatusBadRequest, Reason: a2a.ReasonContentTypeNotSupported, Message: err.Error()})
		return false
	}
	if err := a2a.ValidateVersion(r.Header.Get("A2A-Version")); err != nil {
		writeStandardError(w, &StandardError{HTTPStatus: http.StatusBadRequest, Reason: a2a.ReasonVersionNotSupported, Message: err.Error()})
		return false
	}
	return true
}

func (server *HTTPServer) standardAuth(w http.ResponseWriter, r *http.Request) (hub.RegisteredAgent, bool) {
	token, ok := bearerToken(r.Header.Get("Authorization"))
	if !ok {
		writeStandardError(w, &StandardError{HTTPStatus: http.StatusUnauthorized, Reason: "UNAUTHENTICATED", Message: "authentication failed"})
		return hub.RegisteredAgent{}, false
	}
	agent, err := server.service.AuthenticateStandardAgent(r.Context(), token)
	if err != nil {
		writeStandardServiceError(w, err)
		return hub.RegisteredAgent{}, false
	}
	return agent, true
}

func (server *HTTPServer) standardSendMessage(w http.ResponseWriter, r *http.Request) {
	if !server.standardRequestChecks(w, r) {
		return
	}
	requester, ok := server.standardAuth(w, r)
	if !ok {
		return
	}
	if !server.taskLimiter.allow(requester.AgentID) {
		writeStandardError(w, &StandardError{HTTPStatus: http.StatusTooManyRequests, Reason: "RESOURCE_EXHAUSTED", Message: "task rate limit exceeded"})
		return
	}
	var request a2a.SendMessageRequest
	if !decodeStandardJSON(w, r, server.maxBodyBytes, &request) {
		return
	}
	if tenant := strings.TrimSpace(r.PathValue("tenant")); tenant != "" {
		if request.Tenant != "" && request.Tenant != tenant {
			writeStandardError(w, &StandardError{HTTPStatus: http.StatusBadRequest, Reason: "INVALID_ARGUMENT", Message: "tenant path and body do not match"})
			return
		}
		request.Tenant = tenant
	}
	task, _, err := server.service.CreateStandardTask(r.Context(), requester, request)
	if err != nil {
		writeStandardServiceError(w, err)
		return
	}
	if request.Configuration == nil || !request.Configuration.ReturnImmediately {
		if !server.standardWaitSlots.acquire(requester.AgentID) {
			writeStandardError(w, &StandardError{HTTPStatus: http.StatusTooManyRequests, Reason: "RESOURCE_EXHAUSTED", Message: "too many standard waits for this Agent"})
			return
		}
		task, err = server.service.WaitForStandardTask(r.Context(), requester, task.ID)
		server.standardWaitSlots.release(requester.AgentID)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			writeStandardServiceError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, a2a.SendMessageResponse{Task: taskPointer(task.PublicTask(-1, true))})
}

func (server *HTTPServer) standardListTasks(w http.ResponseWriter, r *http.Request) {
	if !server.standardRequestChecks(w, r) {
		return
	}
	requester, ok := server.standardAuth(w, r)
	if !ok {
		return
	}
	pageSize, err := parseIntQuery(r, "pageSize", 50)
	if err != nil || pageSize < 1 || pageSize > 100 {
		writeStandardError(w, &StandardError{HTTPStatus: http.StatusBadRequest, Reason: "INVALID_ARGUMENT", Message: "pageSize must be between 1 and 100"})
		return
	}
	tenant := strings.TrimSpace(r.PathValue("tenant"))
	offset, err := decodeStandardPageToken(r.URL.Query().Get("pageToken"), requester, tenant, r.URL.Query().Get("contextId"), r.URL.Query().Get("status"))
	if err != nil {
		writeStandardServiceError(w, err)
		return
	}
	filter := a2a.TaskFilter{TargetAgentID: tenant, ContextID: strings.TrimSpace(r.URL.Query().Get("contextId")), PageSize: pageSize, Offset: offset}
	if value := strings.TrimSpace(r.URL.Query().Get("status")); value != "" {
		filter.State = a2a.TaskState(value)
	}
	items, total, err := server.service.ListStandardTasks(r.Context(), requester, filter)
	if err != nil {
		writeStandardServiceError(w, err)
		return
	}
	tasks := make([]a2a.Task, 0, len(items))
	historyLength := parseHistoryLength(r.URL.Query().Get("historyLength"))
	includeArtifacts := r.URL.Query().Get("includeArtifacts") == "true"
	for _, item := range items {
		tasks = append(tasks, item.PublicTask(historyLength, includeArtifacts))
	}
	next := ""
	if offset+len(items) < total {
		next = encodeStandardPageToken(offset+len(items), requester, filter.TargetAgentID, filter.ContextID, string(filter.State))
	}
	writeJSON(w, http.StatusOK, a2a.ListTasksResponse{Tasks: tasks, NextPageToken: next, PageSize: int32(pageSize), TotalSize: int32(total)})
}

func (server *HTTPServer) standardGetTask(w http.ResponseWriter, r *http.Request) {
	if !server.standardRequestChecks(w, r) {
		return
	}
	requester, ok := server.standardAuth(w, r)
	if !ok {
		return
	}
	task, err := server.service.GetStandardTask(r.Context(), requester, r.PathValue("id"))
	if err != nil {
		writeStandardServiceError(w, err)
		return
	}
	if tenant := strings.TrimSpace(r.PathValue("tenant")); tenant != "" && tenant != task.TargetAgentID {
		writeStandardError(w, &StandardError{HTTPStatus: http.StatusNotFound, Reason: "TASK_NOT_FOUND", Message: "task not found"})
		return
	}
	writeJSON(w, http.StatusOK, task.PublicTask(parseHistoryLength(r.URL.Query().Get("historyLength")), r.URL.Query().Get("includeArtifacts") == "true"))
}

func (server *HTTPServer) standardTasksDispatch(w http.ResponseWriter, r *http.Request) {
	first := r.PathValue("first")
	second := r.PathValue("second")
	if first == "tasks" {
		r.SetPathValue("id", second)
		server.standardGetTask(w, r)
		return
	}
	if second == "tasks" {
		r.SetPathValue("tenant", first)
		server.standardListTasks(w, r)
		return
	}
	writeStandardError(w, &StandardError{HTTPStatus: http.StatusNotFound, Reason: "TASK_NOT_FOUND", Message: "task not found"})
}

func (server *HTTPServer) standard5SegmentDispatch(w http.ResponseWriter, r *http.Request) {
	p1 := r.PathValue("p1")
	p2 := r.PathValue("p2")
	p3 := r.PathValue("p3")
	if p1 == "agents" && p3 == "card" {
		r.SetPathValue("agentId", p2)
		server.standardAgentCard(w, r)
		return
	}
	if p2 == "tasks" {
		r.SetPathValue("tenant", p1)
		if strings.HasSuffix(p3, ":subscribe") {
			r.SetPathValue("id", strings.TrimSuffix(p3, ":subscribe"))
			server.standardSubscribeTask(w, r)
			return
		}
		r.SetPathValue("id", p3)
		server.standardGetTask(w, r)
		return
	}
	writeStandardError(w, &StandardError{HTTPStatus: http.StatusNotFound, Reason: "TASK_NOT_FOUND", Message: "task not found"})
}

func (server *HTTPServer) standardCancelTask(w http.ResponseWriter, r *http.Request) {
	if !server.standardRequestChecks(w, r) {
		return
	}
	requester, ok := server.standardAuth(w, r)
	if !ok {
		return
	}
	if !decodeEmptyOrStandardJSON(w, r, server.maxBodyBytes) {
		return
	}
	task, err := server.service.CancelStandardTask(r.Context(), requester, r.PathValue("id"))
	if err != nil {
		writeStandardServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, task.PublicTask(-1, true))
}

func (server *HTTPServer) standardStreamMessage(w http.ResponseWriter, r *http.Request) {
	if !server.standardRequestChecks(w, r) {
		return
	}
	requester, ok := server.standardAuth(w, r)
	if !ok {
		return
	}
	if !server.taskLimiter.allow(requester.AgentID) {
		writeStandardError(w, &StandardError{HTTPStatus: http.StatusTooManyRequests, Reason: "RESOURCE_EXHAUSTED", Message: "task rate limit exceeded"})
		return
	}
	var request a2a.SendMessageRequest
	if !decodeStandardJSON(w, r, server.maxBodyBytes, &request) {
		return
	}
	if tenant := strings.TrimSpace(r.PathValue("tenant")); tenant != "" {
		if request.Tenant != "" && request.Tenant != tenant {
			writeStandardError(w, &StandardError{HTTPStatus: http.StatusBadRequest, Reason: "INVALID_ARGUMENT", Message: "tenant path and body do not match"})
			return
		}
		request.Tenant = tenant
	}
	request.Configuration = &a2a.SendMessageConfiguration{ReturnImmediately: true}
	task, _, err := server.service.CreateStandardTask(r.Context(), requester, request)
	if err != nil {
		writeStandardServiceError(w, err)
		return
	}
	if !server.standardStreamSlots.acquire(requester.AgentID) {
		writeStandardError(w, &StandardError{HTTPStatus: http.StatusTooManyRequests, Reason: "RESOURCE_EXHAUSTED", Message: "too many standard streams for this Agent"})
		return
	}
	defer server.standardStreamSlots.release(requester.AgentID)
	server.streamStandardTask(w, r, requester, task)
}

func (server *HTTPServer) standardSubscribeTask(w http.ResponseWriter, r *http.Request) {
	if !server.standardRequestChecks(w, r) {
		return
	}
	requester, ok := server.standardAuth(w, r)
	if !ok {
		return
	}
	task, err := server.service.GetStandardTask(r.Context(), requester, r.PathValue("id"))
	if err != nil {
		writeStandardServiceError(w, err)
		return
	}
	if tenant := strings.TrimSpace(r.PathValue("tenant")); tenant != "" && tenant != task.TargetAgentID {
		writeStandardError(w, &StandardError{HTTPStatus: http.StatusNotFound, Reason: "TASK_NOT_FOUND", Message: "task not found"})
		return
	}
	if !server.standardStreamSlots.acquire(requester.AgentID) {
		writeStandardError(w, &StandardError{HTTPStatus: http.StatusTooManyRequests, Reason: "RESOURCE_EXHAUSTED", Message: "too many standard streams for this Agent"})
		return
	}
	defer server.standardStreamSlots.release(requester.AgentID)
	server.streamStandardTask(w, r, requester, task)
}

func (server *HTTPServer) streamStandardTask(w http.ResponseWriter, r *http.Request, requester hub.RegisteredAgent, task a2a.TaskRecord) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeStandardError(w, &StandardError{HTTPStatus: http.StatusInternalServerError, Reason: "UNSUPPORTED_OPERATION", Message: "streaming is not supported"})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	writeStreamResponse(w, flusher, a2a.StreamResponse{Task: taskPointer(task.PublicTask(-1, true))})
	if isTerminalTask(task.State) {
		return
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()
	lastRevision := task.Revision
	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepalive.C:
			if !server.service.standardPrincipalActive(r.Context(), requester) {
				return
			}
			if _, err := io.WriteString(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-ticker.C:
			if !server.service.standardPrincipalActive(r.Context(), requester) {
				return
			}
			events, err := server.service.store.StandardTasks().ListTaskEvents(r.Context(), requester.HubID, requester.CircleID, requester.AgentID, task.ID, lastRevision)
			if err != nil {
				return
			}
			for _, durableEvent := range events {
				lastRevision = durableEvent.Revision
				event := a2a.StreamResponse{StatusUpdate: &a2a.TaskStatusUpdateEvent{TaskID: durableEvent.Task.ID, ContextID: durableEvent.Task.ContextID, Status: durableEvent.Task.Status}}
				if !writeStreamResponse(w, flusher, event) || isTerminalTask(durableEvent.Task.Status.State) {
					return
				}
			}
		}
	}
}

func (server *HTTPServer) standardTaskUpdate(w http.ResponseWriter, r *http.Request) {
	if !server.standardRequestChecks(w, r) {
		return
	}
	target, ok := server.standardAuth(w, r)
	if !ok {
		return
	}
	if !server.taskLimiter.allow(target.AgentID) {
		writeStandardError(w, &StandardError{HTTPStatus: http.StatusTooManyRequests, Reason: "RESOURCE_EXHAUSTED", Message: "task rate limit exceeded"})
		return
	}
	var request struct {
		UpdateID         string         `json:"updateId"`
		TurnID           string         `json:"turnId"`
		ExpectedRevision int64          `json:"expectedRevision"`
		State            a2a.TaskState  `json:"state"`
		Message          *a2a.Message   `json:"message,omitempty"`
		Artifacts        []a2a.Artifact `json:"artifacts,omitempty"`
	}
	if !decodeStandardJSON(w, r, server.maxBodyBytes, &request) {
		return
	}
	if strings.TrimSpace(request.UpdateID) == "" || strings.TrimSpace(request.TurnID) == "" || request.ExpectedRevision < 1 || request.State == "" {
		writeStandardError(w, &StandardError{HTTPStatus: http.StatusBadRequest, Reason: "INVALID_ARGUMENT", Message: "updateId, turnId, expectedRevision, and state are required"})
		return
	}
	if request.Message != nil {
		if request.Message.Role != "ROLE_AGENT" {
			writeStandardError(w, &StandardError{HTTPStatus: http.StatusBadRequest, Reason: "INVALID_ARGUMENT", Message: "update.message.role must be ROLE_AGENT"})
			return
		}
		if _, err := a2a.TextFromParts(request.Message.Parts); err != nil {
			writeStandardError(w, &StandardError{HTTPStatus: http.StatusBadRequest, Reason: a2a.ReasonContentTypeNotSupported, Message: err.Error()})
			return
		}
	}
	task, duplicate, err := server.service.ApplyStandardUpdate(r.Context(), a2a.TaskUpdate{HubID: target.HubID, TaskID: r.PathValue("taskId"), TargetAgentID: target.AgentID, UpdateID: request.UpdateID, TurnID: request.TurnID, ExpectedRevision: request.ExpectedRevision, State: request.State, Message: request.Message, Artifacts: request.Artifacts})
	if err != nil {
		writeStandardServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"task": task.PublicTask(-1, true), "duplicate": duplicate})
}

func isTerminalTask(state a2a.TaskState) bool {
	return state == a2a.TaskStateCompleted || state == a2a.TaskStateFailed || state == a2a.TaskStateCanceled || state == a2a.TaskStateRejected
}

func taskPointer(task a2a.Task) *a2a.Task { return &task }

func writeStreamResponse(w io.Writer, flusher http.Flusher, response a2a.StreamResponse) bool {
	encoded, err := json.Marshal(response)
	if err != nil {
		return false
	}
	if responseWriter, ok := w.(http.ResponseWriter); ok {
		controller := http.NewResponseController(responseWriter)
		_ = controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
		defer func() { _ = controller.SetWriteDeadline(time.Time{}) }()
	}
	if _, err := io.WriteString(w, "data: "+string(encoded)+"\n\n"); err != nil {
		return false
	}
	flusher.Flush()
	return true
}

func decodeStandardJSON(w http.ResponseWriter, r *http.Request, maxBytes int64, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeStandardError(w, &StandardError{HTTPStatus: http.StatusRequestEntityTooLarge, Reason: "RESOURCE_EXHAUSTED", Message: "request body exceeds the configured limit"})
			return false
		}
		writeStandardError(w, &StandardError{HTTPStatus: http.StatusBadRequest, Reason: "INVALID_ARGUMENT", Message: "request body is invalid"})
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		writeStandardError(w, &StandardError{HTTPStatus: http.StatusBadRequest, Reason: "INVALID_ARGUMENT", Message: "request body must contain one JSON value"})
		return false
	}
	return true
}

func decodeEmptyOrStandardJSON(w http.ResponseWriter, r *http.Request, maxBytes int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	decoder := json.NewDecoder(r.Body)
	var value any
	err := decoder.Decode(&value)
	if err == io.EOF {
		return true
	}
	if err != nil {
		writeStandardError(w, &StandardError{HTTPStatus: http.StatusBadRequest, Reason: "INVALID_ARGUMENT", Message: "request body is invalid"})
		return false
	}
	if err := decoder.Decode(&value); err != io.EOF {
		writeStandardError(w, &StandardError{HTTPStatus: http.StatusBadRequest, Reason: "INVALID_ARGUMENT", Message: "request body must contain one JSON value"})
		return false
	}
	return true
}

func parseHistoryLength(value string) int {
	if value == "" {
		return -1
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return -1
	}
	return parsed
}

type standardPageToken struct {
	Offset                                      int `json:"offset"`
	AgentID, CircleID, Tenant, ContextID, State string
}

func encodeStandardPageToken(offset int, agent hub.RegisteredAgent, tenant, contextID, state string) string {
	encoded, _ := json.Marshal(standardPageToken{Offset: offset, AgentID: agent.AgentID, CircleID: agent.CircleID, Tenant: tenant, ContextID: contextID, State: state})
	return base64.RawURLEncoding.EncodeToString(encoded)
}

func decodeStandardPageToken(value string, agent hub.RegisteredAgent, tenant, contextID, state string) (int, error) {
	if value == "" {
		return 0, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return 0, standardError(400, "INVALID_ARGUMENT", "pageToken is invalid")
	}
	var token standardPageToken
	if err := json.Unmarshal(decoded, &token); err != nil || token.AgentID != agent.AgentID || token.CircleID != agent.CircleID || token.Tenant != tenant || token.ContextID != contextID || token.State != state || token.Offset < 0 {
		return 0, standardError(400, "INVALID_ARGUMENT", "pageToken is invalid for this scope")
	}
	return token.Offset, nil
}

func writeStandardServiceError(w http.ResponseWriter, err error) {
	var standardErr *StandardError
	if errors.As(err, &standardErr) {
		writeStandardError(w, standardErr)
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		writeStandardError(w, &StandardError{HTTPStatus: 404, Reason: "TASK_NOT_FOUND", Message: "task not found"})
		return
	}
	writeStandardError(w, &StandardError{HTTPStatus: 500, Reason: "INTERNAL", Message: "hub operation failed"})
}

func writeStandardError(w http.ResponseWriter, err *StandardError) {
	status := err.HTTPStatus
	if status == 0 {
		status = http.StatusInternalServerError
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", a2a.JSONMediaType)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(a2a.ErrorResponse{Error: a2a.Status{Code: status, Status: standardStatusName(status), Message: err.Message, Details: []a2a.ErrorInfo{{Type: "type.googleapis.com/google.rpc.ErrorInfo", Reason: err.Reason, Domain: "a2a-protocol.org", Metadata: map[string]string{}}}}})
}

func standardStatusName(status int) string {
	return map[int]string{400: "INVALID_ARGUMENT", 401: "UNAUTHENTICATED", 403: "PERMISSION_DENIED", 404: "NOT_FOUND", 409: "ABORTED", 413: "RESOURCE_EXHAUSTED", 429: "RESOURCE_EXHAUSTED", 500: "INTERNAL", 504: "DEADLINE_EXCEEDED"}[status]
}
