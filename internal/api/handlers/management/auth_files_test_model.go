package management

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	apiHandlers "github.com/router-for-me/CLIProxyAPI/v7/sdk/api/handlers"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

const authFileModelTestTimeout = 30 * time.Second

type authFileModelTestRequest struct {
	Name           string  `json:"name"`
	AuthIndexSnake *string `json:"auth_index"`
	AuthIndexCamel *string `json:"authIndex"`
	Model          string  `json:"model"`
}

// TestAuthFileModel sends a tiny request through a single pinned auth file/model.
func (h *Handler) TestAuthFileModel(c *gin.Context) {
	if h == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "handler not initialized"})
		return
	}

	var body authFileModelTestRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}

	name := strings.TrimSpace(body.Name)
	authIndex := firstNonEmptyString(body.AuthIndexSnake, body.AuthIndexCamel)
	model := strings.TrimSpace(body.Model)
	if name == "" && authIndex == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name or auth_index is required"})
		return
	}
	if model == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "model is required"})
		return
	}

	auth := h.authForFileRequest(name, authIndex)
	if auth == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "auth file not found"})
		return
	}
	if auth.Disabled {
		c.JSON(http.StatusBadRequest, gin.H{"error": "auth file is disabled"})
		return
	}

	apiHandler := h.executionAPIHandler()
	if apiHandler == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "API handler not initialized"})
		return
	}

	if !authFileSupportsModel(auth, model) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "model is not registered for this auth file"})
		return
	}

	handlerType, payload, err := authFileModelTestPayload(auth, model)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), authFileModelTestTimeout)
	defer cancel()
	ctx = context.WithValue(ctx, "gin", c)
	ctx = apiHandlers.WithPinnedAuthID(ctx, auth.ID)

	started := time.Now()
	resp, _, errMsg := apiHandler.ExecuteWithAuthManager(ctx, handlerType, model, payload, "")
	elapsed := time.Since(started).Milliseconds()
	if errMsg != nil {
		status := errMsg.StatusCode
		if status <= 0 {
			status = http.StatusInternalServerError
		}
		message := "model test failed"
		if errMsg.Error != nil {
			message = errMsg.Error.Error()
		}
		c.JSON(status, gin.H{
			"error":            message,
			"model":            model,
			"auth_id":          auth.ID,
			"auth_index":       auth.EnsureIndex(),
			"response_time_ms": elapsed,
		})
		return
	}
	if len(strings.TrimSpace(string(resp))) == 0 {
		c.JSON(http.StatusBadGateway, gin.H{
			"error":            "empty upstream response",
			"model":            model,
			"auth_id":          auth.ID,
			"auth_index":       auth.EnsureIndex(),
			"response_time_ms": elapsed,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":           "success",
		"model":            model,
		"auth_id":          auth.ID,
		"auth_index":       auth.EnsureIndex(),
		"response_time_ms": elapsed,
	})
}

func (h *Handler) executionAPIHandler() *apiHandlers.BaseAPIHandler {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.apiHandler
}

func (h *Handler) authForFileRequest(name string, authIndex string) *coreauth.Auth {
	if h == nil {
		return nil
	}
	if auth := h.authByIndex(authIndex); auth != nil {
		return auth
	}
	name = strings.TrimSpace(name)
	if name == "" || h.authManager == nil {
		return nil
	}
	for _, auth := range h.authManager.List() {
		if auth == nil {
			continue
		}
		if auth.FileName == name || auth.ID == name {
			return auth
		}
	}
	return nil
}

func authFileSupportsModel(auth *coreauth.Auth, model string) bool {
	_, supported, registryHasModels := authFileRegisteredModel(auth, model)
	if !registryHasModels {
		return true
	}
	return supported
}

func authFileRegisteredModel(auth *coreauth.Auth, model string) (*registry.ModelInfo, bool, bool) {
	if auth == nil {
		return nil, false, false
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return nil, false, false
	}
	models := registry.GetGlobalRegistry().GetModelsForClient(auth.ID)
	if len(models) == 0 {
		return nil, false, false
	}
	for _, item := range models {
		if item == nil {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(item.ID), model) {
			return item, true, true
		}
	}
	return nil, false, true
}

func authFileModelTestPayload(auth *coreauth.Auth, model string) (string, []byte, error) {
	if authFileShouldUseResponsesTest(auth, model) {
		payload, err := json.Marshal(gin.H{
			"model":             model,
			"input":             "Hi",
			"stream":            false,
			"max_output_tokens": 16,
		})
		return "openai-response", payload, err
	}

	payload, err := json.Marshal(gin.H{
		"model":      model,
		"messages":   []gin.H{{"role": "user", "content": "Hi"}},
		"stream":     false,
		"max_tokens": 8,
	})
	return "openai", payload, err
}

func authFileShouldUseResponsesTest(auth *coreauth.Auth, model string) bool {
	if auth == nil || !strings.EqualFold(strings.TrimSpace(auth.Provider), "copilot") {
		return false
	}
	if info, supported, _ := authFileRegisteredModel(auth, model); supported && info != nil {
		if len(info.SupportedEndpoints) > 0 {
			return registry.CopilotSupportsResponses(info.SupportedEndpoints) && !registry.CopilotSupportsChatCompletions(info.SupportedEndpoints)
		}
	}
	return looksLikeCopilotResponsesOnlyModel(model)
}

func looksLikeCopilotResponsesOnlyModel(model string) bool {
	normalized := strings.ToLower(strings.TrimSpace(model))
	return strings.HasPrefix(normalized, "gpt-") && strings.Contains(normalized, "-codex")
}
