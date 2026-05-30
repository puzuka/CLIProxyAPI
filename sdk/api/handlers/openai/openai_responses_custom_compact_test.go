package openai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	internalconfig "github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/api/handlers"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
	"github.com/tidwall/gjson"
)

// customCompactLLMExecutor captures the chat completions call and returns a
// canned compact summary matching the required handoff sections.
type customCompactLLMExecutor struct {
	provider string
	model    string
	alt      string
	calls    int
	payload  []byte

	// responseText is the canned text the executor returns as the LLM response.
	responseText string
}

func (e *customCompactLLMExecutor) Identifier() string { return e.provider }

func (e *customCompactLLMExecutor) Execute(ctx context.Context, auth *coreauth.Auth, req coreexecutor.Request, opts coreexecutor.Options) (coreexecutor.Response, error) {
	e.calls++
	e.model = req.Model
	e.alt = opts.Alt
	e.payload = append(e.payload[:0], req.Payload...)

	// Build a /chat/completions response with the canned text
	text := e.responseText
	if text == "" {
		text = validCustomCompactOutput()
	}
	chatResp := map[string]interface{}{
		"id":     "chatcmpl-test",
		"object": "chat.completion",
		"model":  req.Model,
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": text,
				},
				"finish_reason": "stop",
			},
		},
	}
	respBytes, _ := json.Marshal(chatResp)
	return coreexecutor.Response{Payload: respBytes}, nil
}

func (e *customCompactLLMExecutor) ExecuteStream(context.Context, *coreauth.Auth, coreexecutor.Request, coreexecutor.Options) (*coreexecutor.StreamResult, error) {
	return nil, errors.New("not implemented")
}

func (e *customCompactLLMExecutor) Refresh(ctx context.Context, auth *coreauth.Auth) (*coreauth.Auth, error) {
	return auth, nil
}

func (e *customCompactLLMExecutor) CountTokens(context.Context, *coreauth.Auth, coreexecutor.Request, coreexecutor.Options) (coreexecutor.Response, error) {
	return coreexecutor.Response{}, errors.New("not implemented")
}

func (e *customCompactLLMExecutor) HttpRequest(context.Context, *coreauth.Auth, *http.Request) (*http.Response, error) {
	return nil, errors.New("not implemented")
}

// validCustomCompactOutput returns a text that satisfies all required handoff sections.
func validCustomCompactOutput() string {
	return `Current task: Implementing custom compact feature for CLIProxy
User intent: Add LLM-based context compaction when Codex compact fallback is disabled
Repo / location: /Users/test/Projects/CLIProxy
Current state: Configuration and handler changes in progress
Important files:
- internal/config/sdk_config.go: CustomCompactConfig struct added
- sdk/api/handlers/openai/openai_responses_custom_compact.go: Custom compact handler logic
Changes already made:
- Added CustomCompactConfig struct with model, max-tokens, temperature, max-retries fields
- Implemented extractConversationForCompact() to parse compact input
Known verification: All existing compact tests pass
Unfinished work: Integration tests and documentation update
Next action: Run the full test suite to verify custom compact works end-to-end
Do not do: Do not modify the existing compact-fallback behavior when it is enabled`
}

// TestCustomCompactActivatesWhenFallbackDisabled verifies that when
// compact-fallback is disabled and custom-compact is enabled, the proxy
// routes the compact request through the custom LLM compact path and
// returns the response.compaction format with preserved messages.
func TestCustomCompactActivatesWhenFallbackDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	llmExecutor := &customCompactLLMExecutor{provider: "openai-compatibility"}
	manager := coreauth.NewManager(nil, nil, nil)
	manager.RegisterExecutor(llmExecutor)

	auth := &coreauth.Auth{ID: "custom-compact-auth", Provider: llmExecutor.Identifier(), Status: coreauth.StatusActive}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("Register auth: %v", err)
	}
	registry.GetGlobalRegistry().RegisterClient(auth.ID, auth.Provider, []*registry.ModelInfo{{ID: "deepseek-v4-pro"}})
	t.Cleanup(func() {
		registry.GetGlobalRegistry().UnregisterClient(auth.ID)
	})

	temp := 0.2
	cfg := &sdkconfig.SDKConfig{}
	cfg.CompactFallback.Enabled = false // disabled
	cfg.CustomCompact.Enabled = true
	cfg.CustomCompact.Model = "deepseek-v4-pro"
	cfg.CustomCompact.Temperature = &temp
	base := handlers.NewBaseAPIHandlers(cfg, manager)
	h := NewOpenAIResponsesAPIHandler(base)
	router := gin.New()
	router.POST("/v1/responses/compact", h.Compact)

	body := `{"model":"deepseek-v4-pro","input":[{"type":"message","role":"developer","content":[{"type":"input_text","text":"system instructions"}]},{"type":"message","role":"user","content":[{"type":"input_text","text":"hello world"}]},{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hi there"}]}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/responses/compact", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", resp.Code, http.StatusOK, resp.Body.String())
	}
	if llmExecutor.calls != 1 {
		t.Fatalf("LLM executor calls = %d, want 1", llmExecutor.calls)
	}
	// Verify the executor received a /chat/completions call (alt = "")
	if llmExecutor.alt != "" {
		t.Fatalf("executor alt = %q, want empty (chat completions)", llmExecutor.alt)
	}
	// Verify the response uses response.compaction format
	respBody := resp.Body.String()
	if !gjson.Get(respBody, "id").Exists() {
		t.Fatalf("response missing id field")
	}
	if gjson.Get(respBody, "object").String() != "response.compaction" {
		t.Fatalf("response object = %q, want %q", gjson.Get(respBody, "object").String(), "response.compaction")
	}

	// Verify preserved developer and user messages in output
	outputArr := gjson.Get(respBody, "output")
	if !outputArr.IsArray() {
		t.Fatalf("output should be an array")
	}
	items := outputArr.Array()
	if len(items) < 2 {
		t.Fatalf("output should have at least 2 items (preserved messages + summary), got %d", len(items))
	}

	// Check that developer and user messages are preserved
	foundDeveloper := false
	foundUser := false
	foundSummary := false
	for _, item := range items {
		itemType := item.Get("type").String()
		role := item.Get("role").String()
		if itemType == "message" && role == "developer" {
			foundDeveloper = true
			// Verify developer message content is preserved
			devText := item.Get("content.0.text").String()
			if devText != "system instructions" {
				t.Fatalf("developer message text = %q, want %q", devText, "system instructions")
			}
		}
		if itemType == "message" && role == "user" {
			foundUser = true
		}
		if itemType == "compaction_summary" {
			foundSummary = true
			summaryText := item.Get("encrypted_content").String()
			if !strings.Contains(summaryText, "Current task:") {
				t.Fatalf("compaction_summary text missing 'Current task:' section; got: %s", summaryText[:min(200, len(summaryText))])
			}
		}
	}
	if !foundDeveloper {
		t.Fatalf("output missing preserved developer message")
	}
	if !foundUser {
		t.Fatalf("output missing preserved user message")
	}
	if !foundSummary {
		t.Fatalf("output missing compaction_summary item")
	}
}

// TestCustomCompactSkippedWhenFallbackEnabled verifies that when
// compact-fallback is enabled, custom compact is NOT used even if
// custom-compact is also enabled.
func TestCustomCompactSkippedWhenFallbackEnabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	codexExecutor := &providerCaptureExecutor{provider: "codex"}
	compatExecutor := &customCompactLLMExecutor{provider: "openai-compatibility"}
	manager := coreauth.NewManager(nil, nil, nil)
	manager.RegisterExecutor(codexExecutor)
	manager.RegisterExecutor(compatExecutor)

	compatAuth := &coreauth.Auth{ID: "compat-auth-cc", Provider: compatExecutor.Identifier(), Status: coreauth.StatusActive}
	codexAuth := &coreauth.Auth{ID: "codex-auth-cc", Provider: codexExecutor.Identifier(), Status: coreauth.StatusActive}
	if _, err := manager.Register(context.Background(), compatAuth); err != nil {
		t.Fatalf("Register compat auth: %v", err)
	}
	if _, err := manager.Register(context.Background(), codexAuth); err != nil {
		t.Fatalf("Register codex auth: %v", err)
	}
	registry.GetGlobalRegistry().RegisterClient(compatAuth.ID, compatAuth.Provider, []*registry.ModelInfo{{ID: "deepseek-v4-pro"}})
	registry.GetGlobalRegistry().RegisterClient(codexAuth.ID, codexAuth.Provider, []*registry.ModelInfo{{ID: "gpt-5.5"}})
	t.Cleanup(func() {
		registry.GetGlobalRegistry().UnregisterClient(compatAuth.ID)
		registry.GetGlobalRegistry().UnregisterClient(codexAuth.ID)
	})

	cfg := &sdkconfig.SDKConfig{}
	cfg.CompactFallback.Enabled = true // enabled -> takes priority
	cfg.CompactFallback.Model = "gpt-5.5"
	cfg.CompactFallback.AppliesToProviders = []string{"*"}
	cfg.CustomCompact.Enabled = true
	cfg.CustomCompact.Model = "deepseek-v4-pro"
	base := handlers.NewBaseAPIHandlers(cfg, manager)
	h := NewOpenAIResponsesAPIHandler(base)
	router := gin.New()
	router.POST("/v1/responses/compact", h.Compact)

	req := httptest.NewRequest(http.MethodPost, "/v1/responses/compact", strings.NewReader(`{"model":"deepseek-v4-pro","input":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", resp.Code, http.StatusOK, resp.Body.String())
	}
	// When compact-fallback is enabled, the request should go through the codex
	// executor (via fallback), NOT the custom compact LLM executor.
	if codexExecutor.calls != 1 {
		t.Fatalf("codex executor calls = %d, want 1 (compact fallback should be used)", codexExecutor.calls)
	}
	if compatExecutor.calls != 0 {
		t.Fatalf("compat executor calls = %d, want 0 (custom compact should NOT fire when fallback is enabled)", compatExecutor.calls)
	}
}

// TestCustomCompactSkippedForCodexNativeModel verifies that even when custom
// compact is enabled, models served by a Codex provider use the native compact
// path (no custom compact, no fallback rewrite).
func TestCustomCompactSkippedForCodexNativeModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	codexExecutor := &providerCaptureExecutor{provider: "codex"}
	manager := coreauth.NewManager(nil, nil, nil)
	manager.RegisterExecutor(codexExecutor)

	codexAuth := &coreauth.Auth{ID: "codex-native-cc", Provider: codexExecutor.Identifier(), Status: coreauth.StatusActive}
	if _, err := manager.Register(context.Background(), codexAuth); err != nil {
		t.Fatalf("Register codex auth: %v", err)
	}
	registry.GetGlobalRegistry().RegisterClient(codexAuth.ID, codexAuth.Provider, []*registry.ModelInfo{{ID: "gpt-5-codex"}})
	t.Cleanup(func() {
		registry.GetGlobalRegistry().UnregisterClient(codexAuth.ID)
	})

	cfg := &sdkconfig.SDKConfig{}
	cfg.CompactFallback.Enabled = false
	cfg.CustomCompact.Enabled = true
	cfg.CustomCompact.Model = "gpt-5-codex"
	base := handlers.NewBaseAPIHandlers(cfg, manager)
	h := NewOpenAIResponsesAPIHandler(base)
	router := gin.New()
	router.POST("/v1/responses/compact", h.Compact)

	req := httptest.NewRequest(http.MethodPost, "/v1/responses/compact", strings.NewReader(`{"model":"gpt-5-codex","input":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", resp.Code, http.StatusOK, resp.Body.String())
	}
	// Codex-native model should go directly through the codex executor,
	// NOT through custom compact.
	if codexExecutor.calls != 1 {
		t.Fatalf("codex executor calls = %d, want 1 (native compact should be used)", codexExecutor.calls)
	}
	if codexExecutor.alt != "responses/compact" {
		t.Fatalf("codex alt = %q, want %q", codexExecutor.alt, "responses/compact")
	}
}

// TestCustomCompactSendsCorrectChatPayload verifies that the custom compact
// path builds a proper /chat/completions request with the system prompt and
// extracted conversation content.
func TestCustomCompactSendsCorrectChatPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	llmExecutor := &customCompactLLMExecutor{provider: "openai-compatibility"}
	manager := coreauth.NewManager(nil, nil, nil)
	manager.RegisterExecutor(llmExecutor)

	auth := &coreauth.Auth{ID: "payload-check-auth", Provider: llmExecutor.Identifier(), Status: coreauth.StatusActive}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("Register auth: %v", err)
	}
	registry.GetGlobalRegistry().RegisterClient(auth.ID, auth.Provider, []*registry.ModelInfo{{ID: "test-compact-model"}})
	t.Cleanup(func() {
		registry.GetGlobalRegistry().UnregisterClient(auth.ID)
	})

	temp := 0.3
	cfg := &sdkconfig.SDKConfig{}
	cfg.CompactFallback.Enabled = false
	cfg.CustomCompact.Enabled = true
	cfg.CustomCompact.Model = "test-compact-model"
	cfg.CustomCompact.MaxTokens = 2048
	cfg.CustomCompact.Temperature = &temp
	base := handlers.NewBaseAPIHandlers(cfg, manager)
	h := NewOpenAIResponsesAPIHandler(base)
	router := gin.New()
	router.POST("/v1/responses/compact", h.Compact)

	body := `{"model":"test-compact-model","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"Fix the login bug"}]},{"type":"message","role":"assistant","content":[{"type":"output_text","text":"I will check auth.go"}]}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/responses/compact", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", resp.Code, http.StatusOK, resp.Body.String())
	}

	// Check the payload sent to the LLM
	payload := string(llmExecutor.payload)

	// Verify it's a chat completions request with system and user messages
	if !strings.Contains(payload, `"role":"system"`) {
		t.Fatalf("payload missing system message")
	}
	if !strings.Contains(payload, `"role":"user"`) {
		t.Fatalf("payload missing user message")
	}
	// Verify the system prompt contains the required sections instruction
	if !strings.Contains(payload, "context compaction assistant") {
		t.Fatalf("payload missing system prompt content")
	}
	// Verify conversation content is in the user message
	if !strings.Contains(payload, "Fix the login bug") {
		t.Fatalf("payload missing user conversation content")
	}
	// Verify stream is false
	if !strings.Contains(payload, `"stream":false`) {
		t.Fatalf("payload should have stream=false")
	}
	// Verify model
	if llmExecutor.model != "test-compact-model" {
		t.Fatalf("executor model = %q, want %q", llmExecutor.model, "test-compact-model")
	}
	// Verify max_tokens
	if !strings.Contains(payload, `"max_tokens":2048`) {
		t.Fatalf("payload max_tokens mismatch; payload=%s", payload)
	}
}

// TestCustomCompactPolicyChecksOriginalModelBeforeInternalModel verifies that
// caller policy is enforced on the requested model, not the operator-selected
// custom compact model used for the internal /chat/completions call.
func TestCustomCompactPolicyChecksOriginalModelBeforeInternalModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	llmExecutor := &customCompactLLMExecutor{provider: "openai-compatibility"}
	manager := coreauth.NewManager(nil, nil, nil)
	manager.RegisterExecutor(llmExecutor)

	auth := &coreauth.Auth{ID: "custom-compact-policy", Provider: llmExecutor.Identifier(), Status: coreauth.StatusActive}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("Register auth: %v", err)
	}
	registry.GetGlobalRegistry().RegisterClient(auth.ID, auth.Provider, []*registry.ModelInfo{
		{ID: "deepseek-v4-pro"},
		{ID: "qwen-3-coder"},
	})
	t.Cleanup(func() {
		registry.GetGlobalRegistry().UnregisterClient(auth.ID)
	})

	const apiKey = "test-custom-compact-deepseek-only"
	temp := 0.2
	cfg := &sdkconfig.SDKConfig{}
	cfg.APIKeys = []string{apiKey}
	cfg.APIKeyMetadata = map[string]internalconfig.APIKeyMetadata{
		internalconfig.APIKeyID(apiKey): {
			AllowedModels: []string{"deepseek*"},
		},
	}
	cfg.CompactFallback.Enabled = false
	cfg.CustomCompact.Enabled = true
	cfg.CustomCompact.Model = "qwen-3-coder"
	cfg.CustomCompact.Temperature = &temp
	base := handlers.NewBaseAPIHandlers(cfg, manager)
	h := NewOpenAIResponsesAPIHandler(base)
	router := gin.New()
	router.Use(setUserAPIKeyMiddleware(apiKey))
	router.POST("/v1/responses/compact", h.Compact)

	req := httptest.NewRequest(http.MethodPost, "/v1/responses/compact", strings.NewReader(`{"model":"deepseek-v4-pro","input":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", resp.Code, resp.Body.String())
	}
	if llmExecutor.calls != 1 {
		t.Fatalf("LLM executor calls = %d, want 1", llmExecutor.calls)
	}
	if llmExecutor.model != "qwen-3-coder" {
		t.Fatalf("LLM executor model = %q, want qwen-3-coder", llmExecutor.model)
	}
}

// TestCustomCompactDisabledByDefault verifies that when neither compact-fallback
// nor custom-compact is configured, compact requests go through to the original
// provider's executor unchanged.
func TestCustomCompactDisabledByDefault(t *testing.T) {
	gin.SetMode(gin.TestMode)
	executor := &compactCaptureExecutor{}
	manager := coreauth.NewManager(nil, nil, nil)
	manager.RegisterExecutor(executor)

	auth := &coreauth.Auth{ID: "default-compact-auth", Provider: executor.Identifier(), Status: coreauth.StatusActive}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("Register auth: %v", err)
	}
	registry.GetGlobalRegistry().RegisterClient(auth.ID, auth.Provider, []*registry.ModelInfo{{ID: "test-model"}})
	t.Cleanup(func() {
		registry.GetGlobalRegistry().UnregisterClient(auth.ID)
	})

	cfg := &sdkconfig.SDKConfig{} // both compact-fallback and custom-compact default to disabled
	base := handlers.NewBaseAPIHandlers(cfg, manager)
	h := NewOpenAIResponsesAPIHandler(base)
	router := gin.New()
	router.POST("/v1/responses/compact", h.Compact)

	req := httptest.NewRequest(http.MethodPost, "/v1/responses/compact", strings.NewReader(`{"model":"test-model","input":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", resp.Code, http.StatusOK, resp.Body.String())
	}
	// Should go directly to the original executor with responses/compact alt
	if executor.calls != 1 {
		t.Fatalf("executor calls = %d, want 1", executor.calls)
	}
	if executor.alt != "responses/compact" {
		t.Fatalf("executor alt = %q, want %q", executor.alt, "responses/compact")
	}
}

// TestExtractConversationForCompact verifies the conversation extraction from
// various compact input formats.
func TestExtractConversationForCompact(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains []string
		excludes []string
	}{
		{
			name:     "simple string input",
			input:    `{"model":"m","input":"hello world"}`,
			contains: []string{"User: hello world"},
		},
		{
			name: "message array with reasoning",
			input: `{"model":"m","input":[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"fix bug"}]},
				{"type":"reasoning","summary":[{"type":"summary_text","text":"thinking"}],"encrypted_content":"OPAQUE"},
				{"type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}
			]}`,
			contains: []string{"User: fix bug", "Assistant: done"},
			excludes: []string{"thinking", "OPAQUE"},
		},
		{
			name: "function call and output",
			input: `{"model":"m","input":[
				{"type":"function_call","name":"read_file","arguments":"{\"path\":\"main.go\"}"},
				{"type":"function_call_output","output":"package main"}
			]}`,
			contains: []string{"Function call: read_file", "Function output: package main"},
		},
		{
			name: "custom tool call and output",
			input: `{"model":"m","input":[
				{"type":"custom_tool_call","name":"apply_patch","input":"*** Begin Patch\n*** Update File: auth.go\n"},
				{"type":"custom_tool_call_output","output":"Success. Updated files"}
			]}`,
			contains: []string{
				"Custom tool call: apply_patch",
				"*** Begin Patch",
				"Custom tool output: Success. Updated files",
			},
		},
		{
			name: "with instructions and tools",
			input: `{"model":"m","instructions":"You are a helpful coding agent.","tools":[{"name":"exec_command"},{"name":"read_file"}],"input":[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}
			]}`,
			contains: []string{
				"[System instructions summary]",
				"helpful coding agent",
				"[Available tools: exec_command, read_file]",
				"User: hello",
			},
		},
		{
			name: "with OpenAI function calling tools format",
			input: `{"model":"m","tools":[{"function":{"name":"apply_patch"}},{"function":{"name":"exec_command"}}],"input":[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"test"}]}
			]}`,
			contains: []string{"[Available tools: apply_patch, exec_command]", "User: test"},
		},
		{
			name: "developer messages extracted",
			input: `{"model":"m","input":[
				{"type":"message","role":"developer","content":[{"type":"input_text","text":"system rules here"}]},
				{"type":"message","role":"user","content":[{"type":"input_text","text":"do stuff"}]}
			]}`,
			contains: []string{"Developer: system rules here", "User: do stuff"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractConversationForCompact([]byte(tt.input))
			for _, s := range tt.contains {
				if !strings.Contains(result, s) {
					t.Errorf("result missing %q; got: %s", s, result)
				}
			}
			for _, s := range tt.excludes {
				if strings.Contains(result, s) {
					t.Errorf("result should not contain %q; got: %s", s, result)
				}
			}
		})
	}
}

// TestValidateCustomCompactOutput verifies output validation logic.
func TestValidateCustomCompactOutput(t *testing.T) {
	t.Run("valid output", func(t *testing.T) {
		ok, missing := validateCustomCompactOutput(validCustomCompactOutput())
		if !ok {
			t.Fatalf("expected valid output, missing: %v", missing)
		}
	})

	t.Run("missing sections", func(t *testing.T) {
		ok, missing := validateCustomCompactOutput("Current task: something\nUser intent: something\n" + strings.Repeat("x", 300))
		if ok {
			t.Fatalf("expected invalid output")
		}
		if len(missing) == 0 {
			t.Fatalf("expected missing sections")
		}
	})

	t.Run("too short", func(t *testing.T) {
		ok, _ := validateCustomCompactOutput("short")
		if ok {
			t.Fatalf("expected invalid output for short text")
		}
	})
}

// TestBuildCompactResponseJSON verifies that the compact response matches
// the real Codex response.compaction format with preserved messages.
func TestBuildCompactResponseJSON(t *testing.T) {
	input := `{"model":"m","input":[
		{"type":"message","role":"developer","content":[{"type":"input_text","text":"dev instructions"}]},
		{"type":"message","role":"user","content":[{"type":"input_text","text":"fix the bug"}]},
		{"type":"message","role":"assistant","content":[{"type":"output_text","text":"checking..."}]},
		{"type":"function_call","name":"exec_command","arguments":"{}"},
		{"type":"function_call_output","output":"ok"}
	]}`

	summary := "Current task: fixing a bug\nUser intent: fix it\nRepo / location: /test\nCurrent state: in progress\nImportant files: main.go\nChanges already made: none\nKnown verification: none\nUnfinished work: everything\nNext action: run tests\nDo not do: delete files"

	result, err := buildCompactResponseJSON(summary, []byte(input))
	if err != nil {
		t.Fatalf("buildCompactResponseJSON error: %v", err)
	}

	// Parse and verify structure
	if gjson.GetBytes(result, "object").String() != "response.compaction" {
		t.Fatalf("object = %q, want %q", gjson.GetBytes(result, "object").String(), "response.compaction")
	}

	output := gjson.GetBytes(result, "output")
	if !output.IsArray() {
		t.Fatalf("output should be an array")
	}

	items := output.Array()
	// Should have: developer message, user message, compaction_summary
	// (assistant messages and function calls are NOT preserved)
	if len(items) != 3 {
		t.Fatalf("output length = %d, want 3 (developer + user + summary)", len(items))
	}

	// First item: developer message
	if items[0].Get("type").String() != "message" || items[0].Get("role").String() != "developer" {
		t.Fatalf("output[0] should be developer message; got type=%s role=%s",
			items[0].Get("type").String(), items[0].Get("role").String())
	}
	if items[0].Get("content.0.text").String() != "dev instructions" {
		t.Fatalf("developer content mismatch")
	}

	// Second item: user message
	if items[1].Get("type").String() != "message" || items[1].Get("role").String() != "user" {
		t.Fatalf("output[1] should be user message")
	}

	// Last item: compaction_summary
	last := items[2]
	if last.Get("type").String() != "compaction_summary" {
		t.Fatalf("last item type = %q, want %q", last.Get("type").String(), "compaction_summary")
	}
	if !strings.Contains(last.Get("encrypted_content").String(), "Current task: fixing a bug") {
		t.Fatalf("summary text mismatch")
	}
}

// TestTruncateText verifies the upgraded truncation behavior.
func TestTruncateText(t *testing.T) {
	short := "hello"
	if truncateText(short, 100) != "hello" {
		t.Fatalf("short text should not be truncated")
	}

	long := strings.Repeat("x", 5000)
	result := truncateText(long, 4000)
	if len(result) != 4000+len("...(truncated)") {
		t.Fatalf("truncated text length = %d, want %d", len(result), 4000+len("...(truncated)"))
	}
	if !strings.HasSuffix(result, "...(truncated)") {
		t.Fatalf("truncated text should end with '...(truncated)'")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
