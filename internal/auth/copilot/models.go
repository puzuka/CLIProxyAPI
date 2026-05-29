package copilot

import (
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
