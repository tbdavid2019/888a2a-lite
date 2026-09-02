package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/tbdavid2019/888a2a-lite/internal/hub"
)

const maxResponseBytes = 16 << 20

type Client struct {
	BaseURL    string
	HTTPClient *http.Client
	AgentID    string
	AgentToken string
	MaxRetries int
	RetryDelay time.Duration
}

type RegisterResponse struct {
	Identity  hub.AgentIdentity `json:"identity"`
	Policy    hub.HubPolicy     `json:"policy"`
	Duplicate bool              `json:"duplicate"`
}

type PollResponse struct {
	Items        []hub.InboxItem `json:"items"`
	NextSequence uint64          `json:"nextSequence"`
}

type APIError struct {
	StatusCode int
	Code       string
	Message    string
}

func (err *APIError) Error() string {
	if err.Code == "" {
		return fmt.Sprintf("hub request failed with status %d", err.StatusCode)
	}
	return fmt.Sprintf("hub request failed with status %d: %s", err.StatusCode, err.Code)
}

func New(baseURL string) (*Client, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, errors.New("hub base URL is required")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, errors.New("hub base URL must be an absolute URL")
	}
	return &Client{
		BaseURL:    baseURL,
		HTTPClient: &http.Client{Timeout: 15 * time.Second},
		MaxRetries: 2,
		RetryDelay: 100 * time.Millisecond,
	}, nil
}

func (client *Client) Register(ctx context.Context, declaration hub.AgentDeclaration) (RegisterResponse, error) {
	body, err := client.request(ctx, http.MethodPost, "/hub/v1/agents/register", declaration, false)
	if err != nil {
		return RegisterResponse{}, err
	}
	var response RegisterResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return RegisterResponse{}, fmt.Errorf("decode register response: %w", err)
	}
	if response.Identity.AgentToken != "" {
		client.AgentID = response.Identity.AgentID
		client.AgentToken = response.Identity.AgentToken
	}
	return response, nil
}

func (client *Client) Heartbeat(ctx context.Context) (hub.AgentView, error) {
	body, err := client.request(ctx, http.MethodPost, "/hub/v1/agents/"+url.PathEscape(client.AgentID)+"/heartbeat", nil, true)
	if err != nil {
		return hub.AgentView{}, err
	}
	var response struct {
		Agent hub.AgentView `json:"agent"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return hub.AgentView{}, fmt.Errorf("decode heartbeat response: %w", err)
	}
	return response.Agent, nil
}

func (client *Client) ListPeers(ctx context.Context) ([]hub.AgentView, error) {
	body, err := client.request(ctx, http.MethodGet, "/hub/v1/agents", nil, true)
	if err != nil {
		return nil, err
	}
	var response struct {
		Agents []hub.AgentView `json:"agents"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("decode peer response: %w", err)
	}
	return response.Agents, nil
}

func (client *Client) GetAgent(ctx context.Context, agentID string) (hub.AgentView, error) {
	body, err := client.request(ctx, http.MethodGet, "/hub/v1/agents/"+url.PathEscape(agentID), nil, true)
	if err != nil {
		return hub.AgentView{}, err
	}
	var response hub.AgentView
	if err := json.Unmarshal(body, &response); err != nil {
		return hub.AgentView{}, fmt.Errorf("decode agent response: %w", err)
	}
	return response, nil
}

func (client *Client) SendTask(ctx context.Context, task hub.TaskDelivery) (hub.InboxItem, bool, error) {
	body, err := client.request(ctx, http.MethodPost, "/hub/v1/agents/"+url.PathEscape(task.TargetAgentID)+"/tasks", task, true)
	if err != nil {
		return hub.InboxItem{}, false, err
	}
	var response struct {
		TaskID          string `json:"taskId"`
		ContextID       string `json:"contextId"`
		TargetAgentID   string `json:"targetAgentId"`
		State           string `json:"state"`
		DeliveryOutcome string `json:"deliveryOutcome"`
		Sequence        uint64 `json:"sequence"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return hub.InboxItem{}, false, fmt.Errorf("decode task response: %w", err)
	}
	return hub.InboxItem{
		TaskID: response.TaskID, ContextID: response.ContextID,
		TargetAgentID: response.TargetAgentID, State: hub.DeliveryState(response.State), Sequence: response.Sequence,
	}, response.DeliveryOutcome == "DUPLICATE", nil
}

func (client *Client) PollInbox(ctx context.Context, afterSequence uint64, limit int) (PollResponse, error) {
	path := "/hub/v1/agents/" + url.PathEscape(client.AgentID) + "/inbox?afterSequence=" + strconv.FormatUint(afterSequence, 10) + "&limit=" + strconv.Itoa(limit)
	body, err := client.request(ctx, http.MethodGet, path, nil, true)
	if err != nil {
		return PollResponse{}, err
	}
	var response PollResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return PollResponse{}, fmt.Errorf("decode inbox response: %w", err)
	}
	return response, nil
}

func (client *Client) Acknowledge(ctx context.Context, sequence uint64) error {
	path := "/hub/v1/agents/" + url.PathEscape(client.AgentID) + "/inbox/" + strconv.FormatUint(sequence, 10) + "/ack"
	_, err := client.request(ctx, http.MethodPost, path, nil, true)
	return err
}

func (client *Client) Reconnect(ctx context.Context) (hub.AgentView, error) {
	if client.AgentID == "" || client.AgentToken == "" {
		return hub.AgentView{}, errors.New("agent credentials are required to reconnect")
	}
	return client.Heartbeat(ctx)
}

func (client *Client) request(ctx context.Context, method, path string, payload any, authenticated bool) ([]byte, error) {
	encoded := []byte(nil)
	var err error
	if payload != nil {
		encoded, err = json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("encode request: %w", err)
		}
	}
	httpClient := client.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	maxRetries := client.MaxRetries
	if maxRetries < 0 {
		maxRetries = 0
	}
	delay := client.RetryDelay
	if delay <= 0 {
		delay = 100 * time.Millisecond
	}
	for attempt := 0; attempt <= maxRetries; attempt++ {
		request, err := http.NewRequestWithContext(ctx, method, client.BaseURL+path, bytes.NewReader(encoded))
		if err != nil {
			return nil, err
		}
		request.Header.Set("Accept", "application/json")
		if payload != nil {
			request.Header.Set("Content-Type", "application/json")
		}
		if authenticated {
			request.Header.Set("X-Agent-ID", client.AgentID)
			request.Header.Set("Authorization", "Bearer "+client.AgentToken)
		}
		response, err := httpClient.Do(request)
		if err != nil {
			if attempt < maxRetries && retryContext(ctx, delay, attempt) == nil {
				continue
			}
			return nil, err
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
		closeErr := response.Body.Close()
		if readErr == nil {
			readErr = closeErr
		}
		if readErr != nil {
			return nil, readErr
		}
		if len(body) > maxResponseBytes {
			return nil, errors.New("hub response exceeded size limit")
		}
		if response.StatusCode >= 200 && response.StatusCode < 300 {
			return body, nil
		}
		if attempt < maxRetries && (response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500) {
			if retryContext(ctx, delay, attempt) == nil {
				continue
			}
		}
		return nil, parseAPIError(response.StatusCode, body)
	}
	return nil, errors.New("hub request retries exhausted")
}

func retryContext(ctx context.Context, delay time.Duration, attempt int) error {
	timer := time.NewTimer(delay * time.Duration(1<<attempt))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func parseAPIError(status int, body []byte) error {
	var response struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &response)
	return &APIError{StatusCode: status, Code: response.Error.Code, Message: response.Error.Message}
}
