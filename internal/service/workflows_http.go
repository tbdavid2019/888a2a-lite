package service

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/tbdavid2019/888a2a-lite/internal/hub"
)

func (server *HTTPServer) createWorkflow(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, "")
	if !ok {
		return
	}
	if !server.taskLimiter.allow(agentID) {
		writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", "workflow rate limit exceeded")
		return
	}
	var input WorkflowCreateInput
	if !decodeJSON(w, r, server.maxBodyBytes, &input) {
		return
	}
	workflow, duplicate, err := server.service.CreateWorkflow(r.Context(), agentID, token, input)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	status := http.StatusCreated
	if duplicate {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{"workflow": workflow, "duplicate": duplicate})
}

func (server *HTTPServer) listWorkflows(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, "")
	if !ok {
		return
	}
	limit, err := parseIntQuery(r, "limit", 50)
	if err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "limit must be an integer")
		return
	}
	offset, err := parseIntQuery(r, "offset", 0)
	if err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "offset must be an integer")
		return
	}
	workflows, total, err := server.service.ListWorkflows(r.Context(), agentID, token, limit, offset)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, map[string]any{"workflows": workflows, "total": total, "limit": limit, "offset": offset})
}

func (server *HTTPServer) getWorkflow(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, "")
	if !ok {
		return
	}
	workflow, err := server.service.GetWorkflow(r.Context(), agentID, token, r.PathValue("workflowId"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, workflow)
}

func (server *HTTPServer) registerWorkflowAttempt(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, "")
	if !ok {
		return
	}
	if !server.taskLimiter.allow(agentID) {
		writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", "workflow rate limit exceeded")
		return
	}
	var input hub.WorkflowAttemptRegistration
	if !decodeJSON(w, r, server.maxBodyBytes, &input) {
		return
	}
	workflow, duplicate, err := server.service.RegisterWorkflowAttempt(r.Context(), agentID, token, r.PathValue("workflowId"), input)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	status := http.StatusCreated
	if duplicate {
		status = http.StatusOK
	}
	writeWorkflowMutation(w, status, agentID, workflow, input.StepID, input.Attempt, duplicate)
}

func (server *HTTPServer) reportWorkflowOutcome(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, "")
	if !ok {
		return
	}
	if !server.taskLimiter.allow(agentID) {
		writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", "workflow rate limit exceeded")
		return
	}
	attempt, err := strconv.Atoi(r.PathValue("attempt"))
	if err != nil || attempt < 1 {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "attempt must be a positive integer")
		return
	}
	var input struct {
		State  hub.WorkflowStepState `json:"state"`
		Result string                `json:"result,omitempty"`
		Error  string                `json:"error,omitempty"`
	}
	if !decodeJSON(w, r, server.maxBodyBytes, &input) {
		return
	}
	outcome := hub.WorkflowOutcome{StepID: r.PathValue("stepId"), Attempt: attempt, State: input.State, Result: input.Result, Error: input.Error}
	workflow, duplicate, err := server.service.ReportWorkflowOutcome(r.Context(), agentID, token, r.PathValue("workflowId"), outcome)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeWorkflowMutation(w, http.StatusOK, agentID, workflow, outcome.StepID, outcome.Attempt, duplicate)
}

func writeWorkflowMutation(w http.ResponseWriter, status int, actorAgentID string, workflow hub.Workflow, stepID string, attempt int, duplicate bool) {
	if actorAgentID == workflow.OwnerAgentID {
		writeJSON(w, status, map[string]any{"workflow": workflow, "duplicate": duplicate})
		return
	}
	var stepState hub.WorkflowStepState
	for _, step := range workflow.Steps {
		if step.StepID == stepID {
			stepState = step.State
			break
		}
	}
	writeJSON(w, status, map[string]any{"workflowId": workflow.WorkflowID, "stepId": stepID, "attempt": attempt, "state": stepState, "duplicate": duplicate})
}

func (server *HTTPServer) cancelWorkflow(w http.ResponseWriter, r *http.Request) {
	agentID, token, ok := server.agentCredentials(w, r, "")
	if !ok {
		return
	}
	if !server.taskLimiter.allow(agentID) {
		writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", "workflow rate limit exceeded")
		return
	}
	if !decodeEmptyOrJSON(w, r, server.maxBodyBytes) {
		return
	}
	workflow, err := server.service.CancelWorkflow(r.Context(), agentID, token, strings.TrimSpace(r.PathValue("workflowId")))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, workflow)
}
