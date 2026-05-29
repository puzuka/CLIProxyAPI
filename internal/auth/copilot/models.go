package copilot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	log "github.com/sirupsen/logrus"
)

// CopilotModel is a single entry from the Copilot GET /models response.
type CopilotModel struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	Object             string   `json:"object"`
	Vendor             string   `json:"vendor"`
	SupportedEndpoints []string `json:"supported_endpoints,omitempty"`
	ModelPickerEnabled *bool    `json:"model_picker_enabled,omitempty"`
	Capabilities       struct {
		Type   string `json:"type"`
		Limits struct {
			MaxContextWindowTokens int `json:"max_context_window_tokens"`
			MaxOutputTokens        int `json:"max_output_tokens"`
			MaxPromptTokens        int `json:"max_prompt_tokens"`
		} `json:"limits"`
		Supports struct {
			Vision bool `json:"vision"`
		} `json:"supports"`
	} `json:"capabilities"`
	Policy struct {
		State string `json:"state"`
	} `json:"policy"`
}

// ModelProbeResult captures the result of a live Copilot model probe.
type ModelProbeResult struct {
	Model             string
	Callable          bool
	ModelNotSupported bool
	StatusCode        int
	ErrorCode         string
	ErrorMessage      string
}

// ChatModelProbeResult is kept for compatibility with callers that still refer
// to the original chat-completions probe result name.
type ChatModelProbeResult = ModelProbeResult

// ListModels fetches the live Copilot model catalog from {endpoint}/models
// using the internal Copilot token as the bearer credential.
func (a *Auth) ListModels(ctx context.Context, endpoint, copilotToken string) ([]*CopilotModel, error) {
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if endpoint == "" {
		endpoint = DefaultAPIEndpoint
	}
	copilotToken = strings.TrimSpace(copilotToken)
	if copilotToken == "" {
		return nil, fmt.Errorf("copilot: copilot token is empty")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("copilot: create models request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+copilotToken)
	for key, value := range RequestHeaders() {
		req.Header.Set(key, value)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("copilot: models request failed: %w", err)
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.Errorf("copilot models: close body error: %v", errClose)
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("copilot: read models response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("copilot: models request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var out struct {
		Data []*CopilotModel `json:"data"`
	}
	if err = json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("copilot: parse models response: %w", err)
	}
	return out.Data, nil
}

// ProbeChatCompletionModel verifies that a model is actually callable through
// Copilot's OpenAI-compatible chat completions endpoint. Copilot /models can
// report account-disabled models as enabled, so the live call is the source of truth.
func (a *Auth) ProbeChatCompletionModel(ctx context.Context, endpoint, copilotToken, model string) (*ModelProbeResult, error) {
	payload := map[string]any{
		"model":      strings.TrimSpace(model),
		"messages":   []map[string]string{{"role": "user", "content": "Hi"}},
		"stream":     false,
		"max_tokens": 1,
	}
	return a.probeModel(ctx, endpoint, copilotToken, model, "/chat/completions", payload)
}

// ProbeResponsesModel verifies that a model is callable through Copilot's
// OpenAI-compatible Responses endpoint.
func (a *Auth) ProbeResponsesModel(ctx context.Context, endpoint, copilotToken, model string) (*ModelProbeResult, error) {
	payload := map[string]any{
		"model":             strings.TrimSpace(model),
		"input":             "Hi",
		"stream":            false,
		"max_output_tokens": 16,
	}
	return a.probeModel(ctx, endpoint, copilotToken, model, "/responses", payload)
}

func (a *Auth) probeModel(ctx context.Context, endpoint, copilotToken, model, path string, payload map[string]any) (*ModelProbeResult, error) {
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if endpoint == "" {
		endpoint = DefaultAPIEndpoint
	}
	copilotToken = strings.TrimSpace(copilotToken)
	if copilotToken == "" {
		return nil, fmt.Errorf("copilot: copilot token is empty")
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return nil, fmt.Errorf("copilot: model is empty")
	}
	if payload == nil {
		return nil, fmt.Errorf("copilot: model probe payload is empty")
	}
	payload["model"] = model

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("copilot: marshal model probe payload: %w", err)
	}

	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("copilot: model probe path is empty")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("copilot: create model probe request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+copilotToken)
	req.Header.Set("Content-Type", "application/json")
	for key, value := range RequestHeaders() {
		req.Header.Set(key, value)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("copilot: model probe request failed: %w", err)
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.Errorf("copilot model probe: close body error: %v", errClose)
		}
	}()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("copilot: read model probe response: %w", err)
	}

	result := &ModelProbeResult{
		Model:      model,
		Callable:   resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices,
		StatusCode: resp.StatusCode,
	}
	if result.Callable {
		return result, nil
	}

	code, message := parseCopilotErrorBody(respBody)
	result.ErrorCode = code
	result.ErrorMessage = message
	result.ModelNotSupported = isCopilotModelNotSupportedProbe(code, message)
	return result, nil
}

func parseCopilotErrorBody(body []byte) (string, string) {
	var parsed struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if len(body) > 0 && json.Unmarshal(body, &parsed) == nil {
		return strings.TrimSpace(parsed.Error.Code), strings.TrimSpace(parsed.Error.Message)
	}
	return "", strings.TrimSpace(string(body))
}

func isCopilotModelNotSupportedProbe(code, message string) bool {
	lowerCode := strings.ToLower(strings.TrimSpace(code))
	lowerMessage := strings.ToLower(strings.TrimSpace(message))
	if strings.Contains(lowerCode, "model_not_supported") {
		return true
	}
	patterns := [...]string{
		"requested model is not supported",
		"requested model is unsupported",
		"requested model is unavailable",
		"model is not supported",
		"model not supported",
		"unsupported model",
		"model unavailable",
		"not available for your plan",
		"not available for your account",
	}
	for _, pattern := range patterns {
		if strings.Contains(lowerMessage, pattern) {
			return true
		}
	}
	return false
}
