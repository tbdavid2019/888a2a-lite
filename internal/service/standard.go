package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/tbdavid2019/888a2a-lite/internal/a2a"
	"github.com/tbdavid2019/888a2a-lite/internal/hub"
	"github.com/tbdavid2019/888a2a-lite/internal/store"
)

const standardWaitTimeout = 30 * time.Second

type StandardError struct {
	HTTPStatus int
	Reason     string
	Message    string
}

func (err *StandardError) Error() string { return err.Message }

func standardError(status int, reason, message string) error {
	return &StandardError{HTTPStatus: status, Reason: reason, Message: message}
}

func (service *Service) AuthenticateStandardAgent(ctx context.Context, token string) (hub.RegisteredAgent, error) {
	agent, err := service.store.Agents().AuthenticateAgentByToken(ctx, token)
	if err != nil {
		return hub.RegisteredAgent{}, standardError(401, "UNAUTHENTICATED", "authentication failed")
	}
	if state := agent.StateAt(service.now().UTC()); state == hub.AgentStateExpired || state == hub.AgentStateRevoked {
		return hub.RegisteredAgent{}, standardError(401, "UNAUTHENTICATED", "authentication failed")
	}
	if service.circleResolver.Mode() == "multi" {
		circleRecord, circleErr := service.store.Circles().FindCircle(ctx, agent.CircleID)
		if circleErr != nil || circleRecord.State == hub.CircleStateDisabled {
			return hub.RegisteredAgent{}, standardError(401, "UNAUTHENTICATED", "authentication failed")
		}
	}
	return agent, nil
}

func supportsStandardExecution(agent hub.RegisteredAgent) bool {
	for _, capability := range agent.Capabilities {
		if strings.EqualFold(strings.TrimSpace(capability), a2a.ExecutionCapability) {
			return true
		}
	}
	return false
}

func (service *Service) standardPrincipalActive(ctx context.Context, principal hub.RegisteredAgent) bool {
	agent, err := service.store.Agents().FindAgent(ctx, principal.AgentID)
	if err != nil || agent.HubID != principal.HubID || agent.CircleID != principal.CircleID {
		return false
	}
	state := agent.StateAt(service.now().UTC())
	if state == hub.AgentStateExpired || state == hub.AgentStateRevoked || state == hub.AgentStateOffline || state == hub.AgentStatePending {
		return false
	}
	if service.circleResolver.Mode() == "multi" {
		circleRecord, circleErr := service.store.Circles().FindCircle(ctx, principal.CircleID)
		if circleErr != nil || circleRecord.State == hub.CircleStateDisabled {
			return false
		}
	}
	return true
}

func (service *Service) standardTaskStreamAuthorized(ctx context.Context, principal hub.RegisteredAgent, task a2a.TaskRecord) bool {
	if !service.standardPrincipalActive(ctx, principal) {
		return false
	}
	if groupID, ok := parseGroupTenant(task.TargetAgentID); ok {
		_, _, err := service.groupMemberForStandard(ctx, principal, groupID)
		return err == nil
	}
	return true
}

func (service *Service) StandardGatewayCard(baseURL string) a2a.AgentCard {
	return service.standardCard(baseURL, "888a2a-lite A2A Gateway", "A2A HTTP+JSON Gateway for registered Agents", "")
}

func (service *Service) StandardAgentCard(ctx context.Context, token, agentID, baseURL string) (a2a.AgentCard, error) {
	requester, err := service.AuthenticateStandardAgent(ctx, token)
	if err != nil {
		return a2a.AgentCard{}, err
	}
	target, err := service.store.Agents().FindAgent(ctx, agentID)
	if err != nil || target.CircleID != requester.CircleID || target.StateAt(service.now().UTC()) == hub.AgentStateRevoked || target.StateAt(service.now().UTC()) == hub.AgentStateExpired || !supportsStandardExecution(target) {
		return a2a.AgentCard{}, standardError(404, "TASK_NOT_FOUND", "agent card not found")
	}
	return service.standardCard(baseURL, target.DisplayName, "Standard A2A interface for "+target.DisplayName, target.AgentID), nil
}

func (service *Service) standardCard(baseURL, name, description, tenant string) a2a.AgentCard {
	baseURL = strings.TrimRight(baseURL, "/")
	card := a2a.AgentCard{
		Name: name, Description: description, Version: "1.0.0",
		SupportedInterfaces:  []a2a.AgentInterface{{URL: baseURL + "/a2a/v1", ProtocolBinding: a2a.ProtocolBinding, ProtocolVersion: a2a.ProtocolVersion, Tenant: tenant}},
		Provider:             &a2a.AgentProvider{URL: baseURL, Organization: "888a2a-lite"},
		Capabilities:         a2a.AgentCapabilities{Streaming: true, PushNotifications: false, ExtendedCard: false},
		SecuritySchemes:      map[string]a2a.SecurityScheme{"bearerAuth": {HTTPAuthSecurityScheme: &a2a.HTTPAuthSecurityScheme{Scheme: "bearer", BearerFormat: "AgentToken"}}},
		SecurityRequirements: []a2a.SecurityRequirement{{Schemes: map[string]a2a.StringList{"bearerAuth": {List: []string{}}}}},
		DefaultInputModes:    []string{"text/plain"}, DefaultOutputModes: []string{"text/plain"},
		Skills: []a2a.AgentSkill{{ID: "text-relay", Name: "Text relay", Description: "Durable text Message delivery through the Hub", Tags: []string{"text", "relay"}, InputModes: []string{"text/plain"}, OutputModes: []string{"text/plain"}}},
	}
	if service.config.GroupExtensionEnabled {
		card.Capabilities.Extensions = []a2a.AgentExtension{{URI: a2a.GroupExtensionURI, Description: "Virtual group tenant fan-out and member outcome aggregation", Required: false, Params: map[string]any{"tenantPrefix": "group:"}}}
	}
	return card
}

func messageDigest(message a2a.Message) (string, error) {
	encoded, err := json.Marshal(message)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func validateStandardMessage(message a2a.Message) (string, error) {
	if strings.TrimSpace(message.MessageID) == "" {
		return "", standardError(400, "INVALID_ARGUMENT", "message.messageId is required")
	}
	if message.Role != "ROLE_USER" {
		return "", standardError(400, "INVALID_ARGUMENT", "message.role must be ROLE_USER")
	}
	text, err := a2a.TextFromParts(message.Parts)
	if err != nil {
		return "", standardError(400, a2a.ReasonContentTypeNotSupported, err.Error())
	}
	return text, nil
}

func (service *Service) CreateStandardTask(ctx context.Context, requester hub.RegisteredAgent, request a2a.SendMessageRequest) (a2a.TaskRecord, bool, error) {
	text, err := validateStandardMessage(request.Message)
	if err != nil {
		return a2a.TaskRecord{}, false, err
	}
	targetID := strings.TrimSpace(request.Tenant)
	if targetID == "" {
		return a2a.TaskRecord{}, false, standardError(400, "INVALID_ARGUMENT", "tenant is required")
	}
	target, err := service.store.Agents().FindAgent(ctx, targetID)
	if err != nil || target.CircleID != requester.CircleID || target.StateAt(service.now().UTC()) == hub.AgentStateExpired || target.StateAt(service.now().UTC()) == hub.AgentStateRevoked {
		return a2a.TaskRecord{}, false, standardError(404, "TASK_NOT_FOUND", "target agent not found")
	}
	if !supportsStandardExecution(target) {
		return a2a.TaskRecord{}, false, standardError(404, "TASK_NOT_FOUND", "target agent not found")
	}
	digest, err := messageDigest(request.Message)
	if err != nil {
		return a2a.TaskRecord{}, false, err
	}
	if existing, findErr := service.store.StandardTasks().FindTaskByMessage(ctx, requester.HubID, requester.CircleID, requester.AgentID, targetID, request.Message.MessageID); findErr == nil {
		if existing.ContentDigest != digest {
			return a2a.TaskRecord{}, false, standardError(409, "INVALID_ARGUMENT", "messageId was reused with different content")
		}
		return existing, true, nil
	} else if !errors.Is(findErr, store.ErrNotFound) {
		return a2a.TaskRecord{}, false, findErr
	}
	taskID := strings.TrimSpace(request.Message.TaskID)
	contextID := strings.TrimSpace(request.Message.ContextID)
	if taskID != "" {
		existing, findErr := service.store.StandardTasks().FindTask(ctx, requester.HubID, requester.CircleID, requester.AgentID, taskID)
		if findErr != nil {
			return a2a.TaskRecord{}, false, standardError(404, "TASK_NOT_FOUND", "task not found")
		}
		if contextID != "" && contextID != existing.ContextID {
			return a2a.TaskRecord{}, false, standardError(400, "INVALID_ARGUMENT", "message.contextId does not match task")
		}
		if existing.State == a2a.TaskStateWorking {
			return a2a.TaskRecord{}, false, standardError(409, "TASK_NOT_CANCELABLE", "task is already working")
		}
		if existing.State != a2a.TaskStateInputRequired && existing.State != a2a.TaskStateAuthRequired {
			return a2a.TaskRecord{}, false, standardError(400, "UNSUPPORTED_OPERATION", "task cannot accept a new turn")
		}
		now := service.now().UTC()
		turnID := fmt.Sprintf("a2a-turn-%d", service.now().UnixNano())
		resumed := existing
		resumed.Message = request.Message
		resumed.MessageID = request.Message.MessageID
		resumed.TurnID = turnID
		resumed.Revision = existing.Revision + 1
		resumed.State = a2a.TaskStateSubmitted
		resumed.ResultMessage = nil
		resumed.History = append(append([]a2a.Message(nil), existing.History...), request.Message)
		resumed.ContentDigest = digest
		resumed.ExecutionDeadline = now.Add(5 * time.Minute)
		resumed.RetryBudget = 3
		resumed.UpdatedAt = now
		item := hub.InboxItem{HubID: requester.HubID, CircleID: requester.CircleID, TargetAgentID: targetID, RequesterAgentID: requester.AgentID, TaskID: existing.ID, ContextID: existing.ContextID, IdempotencyKey: "a2a:" + request.Message.MessageID, Message: text, State: hub.DeliveryStatePending, CreatedAt: now, Protocol: "A2A/1.0", MessageID: request.Message.MessageID, TurnID: turnID, TaskRevision: resumed.Revision}
		resumed, duplicate, err := service.store.StandardTasks().ResumeTaskWithDelivery(ctx, resumed, existing.Revision, item)
		if err != nil {
			if errors.Is(err, store.ErrConflict) {
				return a2a.TaskRecord{}, false, standardError(409, "INVALID_ARGUMENT", "task changed while a new turn was being created")
			}
			if errors.Is(err, store.ErrInvalidState) {
				return a2a.TaskRecord{}, false, standardError(409, "UNSUPPORTED_OPERATION", "task cannot accept a new turn")
			}
			return a2a.TaskRecord{}, false, err
		}
		if !duplicate && service.broker != nil {
			stored, _, enqueueErr := service.store.Inbox().FindByIdempotencyKey(ctx, hub.IdempotencyKey{HubID: requester.HubID, TargetAgentID: targetID, RequesterAgentID: requester.AgentID, Key: item.IdempotencyKey})
			if enqueueErr == nil {
				service.broker.Publish(stored)
			}
		}
		return resumed, duplicate, nil
	}
	if contextID == "" {
		contextID = fmt.Sprintf("a2a-context-%d", service.now().UnixNano())
	}
	taskID = fmt.Sprintf("a2a-task-%d", service.now().UnixNano())
	turnID := fmt.Sprintf("a2a-turn-%d", service.now().UnixNano())
	now := service.now().UTC()
	task := a2a.TaskRecord{HubID: requester.HubID, ID: taskID, CircleID: requester.CircleID, RequesterAgentID: requester.AgentID, TargetAgentID: targetID, ContextID: contextID, MessageID: request.Message.MessageID, TurnID: turnID, Revision: 1, State: a2a.TaskStateSubmitted, Message: request.Message, History: []a2a.Message{request.Message}, ContentDigest: digest, ExecutionDeadline: now.Add(5 * time.Minute), RetryBudget: 3, CreatedAt: now, UpdatedAt: now}
	item := hub.InboxItem{HubID: requester.HubID, CircleID: requester.CircleID, TargetAgentID: targetID, RequesterAgentID: requester.AgentID, TaskID: taskID, ContextID: contextID, IdempotencyKey: "a2a:" + request.Message.MessageID, Message: text, State: hub.DeliveryStatePending, CreatedAt: now, Protocol: "A2A/1.0", MessageID: request.Message.MessageID, TurnID: turnID, TaskRevision: 1}
	created, duplicate, err := service.store.StandardTasks().CreateTaskWithDelivery(ctx, task, item)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			return a2a.TaskRecord{}, false, standardError(409, "INVALID_ARGUMENT", "messageId conflicts with an existing task")
		}
		return a2a.TaskRecord{}, false, err
	}
	if !duplicate && service.broker != nil {
		stored, _, enqueueErr := service.store.Inbox().FindByIdempotencyKey(ctx, hub.IdempotencyKey{HubID: requester.HubID, TargetAgentID: targetID, RequesterAgentID: requester.AgentID, Key: item.IdempotencyKey})
		if enqueueErr == nil {
			service.broker.Publish(stored)
		}
	}
	return created, duplicate, nil
}

func (service *Service) WaitForStandardTask(ctx context.Context, requester hub.RegisteredAgent, taskID string) (a2a.TaskRecord, error) {
	waitTimeout := service.config.StandardWaitTimeout
	if waitTimeout <= 0 {
		waitTimeout = standardWaitTimeout
	}
	deadline := time.NewTimer(waitTimeout)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		task, err := service.store.StandardTasks().FindTask(ctx, requester.HubID, requester.CircleID, requester.AgentID, taskID)
		if err != nil {
			return a2a.TaskRecord{}, err
		}
		switch task.State {
		case a2a.TaskStateCompleted, a2a.TaskStateFailed, a2a.TaskStateCanceled, a2a.TaskStateRejected, a2a.TaskStateInputRequired, a2a.TaskStateAuthRequired:
			return task, nil
		}
		select {
		case <-ctx.Done():
			return a2a.TaskRecord{}, ctx.Err()
		case <-deadline.C:
			return task, standardError(504, "DEADLINE_EXCEEDED", "standard task wait deadline exceeded")
		case <-ticker.C:
		}
	}
}

func (service *Service) GetStandardTask(ctx context.Context, requester hub.RegisteredAgent, taskID string) (a2a.TaskRecord, error) {
	task, err := service.store.StandardTasks().FindTask(ctx, requester.HubID, requester.CircleID, requester.AgentID, taskID)
	if errors.Is(err, store.ErrNotFound) {
		return a2a.TaskRecord{}, standardError(404, "TASK_NOT_FOUND", "task not found")
	}
	return task, err
}

func (service *Service) ListStandardTasks(ctx context.Context, requester hub.RegisteredAgent, filter a2a.TaskFilter) ([]a2a.TaskRecord, int, error) {
	filter.HubID, filter.CircleID, filter.RequesterAgentID = requester.HubID, requester.CircleID, requester.AgentID
	return service.store.StandardTasks().ListTasks(ctx, filter)
}

func (service *Service) CancelStandardTask(ctx context.Context, requester hub.RegisteredAgent, taskID string) (a2a.TaskRecord, error) {
	task, err := service.store.StandardTasks().CancelStandardTask(ctx, requester.HubID, requester.CircleID, requester.AgentID, taskID, service.now().UTC())
	if errors.Is(err, store.ErrNotFound) {
		return a2a.TaskRecord{}, standardError(404, "TASK_NOT_FOUND", "task not found")
	}
	if errors.Is(err, store.ErrInvalidState) {
		return a2a.TaskRecord{}, standardError(400, "TASK_NOT_CANCELABLE", "task is not cancelable")
	}
	return task, err
}

func (service *Service) ApplyStandardUpdate(ctx context.Context, update a2a.TaskUpdate) (a2a.TaskRecord, bool, error) {
	task, duplicate, err := service.store.StandardTasks().ApplyUpdate(ctx, update)
	if errors.Is(err, store.ErrNotFound) {
		return a2a.TaskRecord{}, false, standardError(404, "TASK_NOT_FOUND", "task not found")
	}
	if errors.Is(err, store.ErrConflict) {
		return a2a.TaskRecord{}, false, standardError(409, "INVALID_ARGUMENT", "update conflicts with the current task revision")
	}
	if errors.Is(err, store.ErrInvalidState) {
		return a2a.TaskRecord{}, false, standardError(400, "TASK_NOT_CANCELABLE", "task state transition is invalid")
	}
	return task, duplicate, err
}
