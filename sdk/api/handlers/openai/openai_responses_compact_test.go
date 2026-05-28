package openai

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/klauspost/compress/zstd"
	internalconfig "github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/api/handlers"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
)

type compactCaptureExecutor struct {
	alt          string
	sourceFormat string
	calls        int
}

func (e *compactCaptureExecutor) Identifier() string { return "test-provider" }

func (e *compactCaptureExecutor) Execute(ctx context.Context, auth *coreauth.Auth, req coreexecutor.Request, opts coreexecutor.Options) (coreexecutor.Response, error) {
	e.calls++
	e.alt = opts.Alt
	e.sourceFormat = opts.SourceFormat.String()
	return coreexecutor.Response{Payload: []byte(`{"ok":true}`)}, nil
}

func (e *compactCaptureExecutor) ExecuteStream(context.Context, *coreauth.Auth, coreexecutor.Request, coreexecutor.Options) (*coreexecutor.StreamResult, error) {
	return nil, errors.New("not implemented")
}

func (e *compactCaptureExecutor) Refresh(ctx context.Context, auth *coreauth.Auth) (*coreauth.Auth, error) {
	return auth, nil
}

func (e *compactCaptureExecutor) CountTokens(context.Context, *coreauth.Auth, coreexecutor.Request, coreexecutor.Options) (coreexecutor.Response, error) {
	return coreexecutor.Response{}, errors.New("not implemented")
}

func (e *compactCaptureExecutor) HttpRequest(context.Context, *coreauth.Auth, *http.Request) (*http.Response, error) {
	return nil, errors.New("not implemented")
}

func TestOpenAIResponsesCompactRejectsStream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	executor := &compactCaptureExecutor{}
	manager := coreauth.NewManager(nil, nil, nil)
	manager.RegisterExecutor(executor)

	auth := &coreauth.Auth{ID: "auth1", Provider: executor.Identifier(), Status: coreauth.StatusActive}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("Register auth: %v", err)
	}
	registry.GetGlobalRegistry().RegisterClient(auth.ID, auth.Provider, []*registry.ModelInfo{{ID: "test-model"}})
	t.Cleanup(func() {
		registry.GetGlobalRegistry().UnregisterClient(auth.ID)
	})

	base := handlers.NewBaseAPIHandlers(&sdkconfig.SDKConfig{}, manager)
	h := NewOpenAIResponsesAPIHandler(base)
	router := gin.New()
	router.POST("/v1/responses/compact", h.Compact)

	req := httptest.NewRequest(http.MethodPost, "/v1/responses/compact", strings.NewReader(`{"model":"test-model","stream":true}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusBadRequest)
	}
	if executor.calls != 0 {
		t.Fatalf("executor calls = %d, want 0", executor.calls)
	}
}

func TestOpenAIResponsesCompactExecute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	executor := &compactCaptureExecutor{}
	manager := coreauth.NewManager(nil, nil, nil)
	manager.RegisterExecutor(executor)

	auth := &coreauth.Auth{ID: "auth2", Provider: executor.Identifier(), Status: coreauth.StatusActive}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("Register auth: %v", err)
	}
	registry.GetGlobalRegistry().RegisterClient(auth.ID, auth.Provider, []*registry.ModelInfo{{ID: "test-model"}})
	t.Cleanup(func() {
		registry.GetGlobalRegistry().UnregisterClient(auth.ID)
	})

	base := handlers.NewBaseAPIHandlers(&sdkconfig.SDKConfig{}, manager)
	h := NewOpenAIResponsesAPIHandler(base)
	router := gin.New()
	router.POST("/v1/responses/compact", h.Compact)

	req := httptest.NewRequest(http.MethodPost, "/v1/responses/compact", strings.NewReader(`{"model":"test-model","input":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
	}
	if executor.alt != "responses/compact" {
		t.Fatalf("alt = %q, want %q", executor.alt, "responses/compact")
	}
	if executor.sourceFormat != "openai-response" {
		t.Fatalf("source format = %q, want %q", executor.sourceFormat, "openai-response")
	}
	if strings.TrimSpace(resp.Body.String()) != `{"ok":true}` {
		t.Fatalf("body = %s", resp.Body.String())
	}
}

func TestOpenAIResponsesCompactDecodesZstdRequestBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	executor := &compactCaptureExecutor{}
	manager := coreauth.NewManager(nil, nil, nil)
	manager.RegisterExecutor(executor)

	auth := &coreauth.Auth{ID: "auth3", Provider: executor.Identifier(), Status: coreauth.StatusActive}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("Register auth: %v", err)
	}
	registry.GetGlobalRegistry().RegisterClient(auth.ID, auth.Provider, []*registry.ModelInfo{{ID: "test-model"}})
	t.Cleanup(func() {
		registry.GetGlobalRegistry().UnregisterClient(auth.ID)
	})

	base := handlers.NewBaseAPIHandlers(&sdkconfig.SDKConfig{}, manager)
	h := NewOpenAIResponsesAPIHandler(base)
	router := gin.New()
	router.POST("/v1/responses/compact", h.Compact)

	var compressed bytes.Buffer
	encoder, err := zstd.NewWriter(&compressed)
	if err != nil {
		t.Fatalf("zstd.NewWriter: %v", err)
	}
	if _, errWrite := encoder.Write([]byte(`{"model":"test-model","input":"hello"}`)); errWrite != nil {
		t.Fatalf("zstd write: %v", errWrite)
	}
	if errClose := encoder.Close(); errClose != nil {
		t.Fatalf("zstd close: %v", errClose)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/responses/compact", bytes.NewReader(compressed.Bytes()))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "zstd")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", resp.Code, http.StatusOK, resp.Body.String())
	}
	if executor.calls != 1 {
		t.Fatalf("executor calls = %d, want 1", executor.calls)
	}
	if executor.alt != "responses/compact" {
		t.Fatalf("alt = %q, want %q", executor.alt, "responses/compact")
	}
	if strings.TrimSpace(resp.Body.String()) != `{"ok":true}` {
		t.Fatalf("body = %s", resp.Body.String())
	}
}


// providerCaptureExecutor is a compact-aware test executor that records the
// model field, the alt routing key, and the count of executions, scoped to a
// single provider identifier. Two instances (one per provider) let tests
// assert which provider received a compact request after the optional
// CompactFallback model swap.
type providerCaptureExecutor struct {
	provider string
	model    string
	alt      string
	calls    int
}

func (e *providerCaptureExecutor) Identifier() string { return e.provider }

func (e *providerCaptureExecutor) Execute(ctx context.Context, auth *coreauth.Auth, req coreexecutor.Request, opts coreexecutor.Options) (coreexecutor.Response, error) {
	e.calls++
	e.model = req.Model
	e.alt = opts.Alt
	return coreexecutor.Response{Payload: []byte(`{"ok":true}`)}, nil
}

func (e *providerCaptureExecutor) ExecuteStream(context.Context, *coreauth.Auth, coreexecutor.Request, coreexecutor.Options) (*coreexecutor.StreamResult, error) {
	return nil, errors.New("not implemented")
}

func (e *providerCaptureExecutor) Refresh(ctx context.Context, auth *coreauth.Auth) (*coreauth.Auth, error) {
	return auth, nil
}

func (e *providerCaptureExecutor) CountTokens(context.Context, *coreauth.Auth, coreexecutor.Request, coreexecutor.Options) (coreexecutor.Response, error) {
	return coreexecutor.Response{}, errors.New("not implemented")
}

func (e *providerCaptureExecutor) HttpRequest(context.Context, *coreauth.Auth, *http.Request) (*http.Response, error) {
	return nil, errors.New("not implemented")
}

// TestCompactFallbackAppliedForOpenAICompat verifies that when an
// openai-compatibility model is requested for compact and a Codex auth is
// available for the configured fallback model, the proxy rewrites the model
// field and routes the request through the Codex provider.
func TestCompactFallbackAppliedForOpenAICompat(t *testing.T) {
	gin.SetMode(gin.TestMode)
	openaiCompat := &providerCaptureExecutor{provider: "openai-compatibility"}
	codex := &providerCaptureExecutor{provider: "codex"}
	manager := coreauth.NewManager(nil, nil, nil)
	manager.RegisterExecutor(openaiCompat)
	manager.RegisterExecutor(codex)

	openaiAuth := &coreauth.Auth{ID: "openai-auth", Provider: openaiCompat.Identifier(), Status: coreauth.StatusActive}
	if _, err := manager.Register(context.Background(), openaiAuth); err != nil {
		t.Fatalf("Register openai auth: %v", err)
	}
	codexAuth := &coreauth.Auth{ID: "codex-auth", Provider: codex.Identifier(), Status: coreauth.StatusActive}
	if _, err := manager.Register(context.Background(), codexAuth); err != nil {
		t.Fatalf("Register codex auth: %v", err)
	}
	registry.GetGlobalRegistry().RegisterClient(openaiAuth.ID, openaiAuth.Provider, []*registry.ModelInfo{{ID: "deepseek-v4-pro"}})
	registry.GetGlobalRegistry().RegisterClient(codexAuth.ID, codexAuth.Provider, []*registry.ModelInfo{{ID: "gpt-5.5"}})
	t.Cleanup(func() {
		registry.GetGlobalRegistry().UnregisterClient(openaiAuth.ID)
		registry.GetGlobalRegistry().UnregisterClient(codexAuth.ID)
	})

	cfg := &sdkconfig.SDKConfig{}
	cfg.CompactFallback.Enabled = true
	cfg.CompactFallback.Model = "gpt-5.5"
	cfg.CompactFallback.AppliesToProviders = []string{"openai-compatibility"}
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
	if openaiCompat.calls != 0 {
		t.Fatalf("openai-compatibility executor calls = %d, want 0 (fallback should bypass it)", openaiCompat.calls)
	}
	if codex.calls != 1 {
		t.Fatalf("codex executor calls = %d, want 1", codex.calls)
	}
	if codex.model != "gpt-5.5" {
		t.Fatalf("codex received model = %q, want %q", codex.model, "gpt-5.5")
	}
	if codex.alt != "responses/compact" {
		t.Fatalf("codex received alt = %q, want %q", codex.alt, "responses/compact")
	}
}

// TestCompactFallbackSkippedWhenNoCodexAuth verifies that when no Codex auth
// is registered, the fallback is silently skipped and the request continues
// to the original openai-compatibility provider so operators see the real
// upstream error rather than a misleading proxy-rewritten failure.
func TestCompactFallbackSkippedWhenNoCodexAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	openaiCompat := &providerCaptureExecutor{provider: "openai-compatibility"}
	manager := coreauth.NewManager(nil, nil, nil)
	manager.RegisterExecutor(openaiCompat)

	openaiAuth := &coreauth.Auth{ID: "openai-auth-2", Provider: openaiCompat.Identifier(), Status: coreauth.StatusActive}
	if _, err := manager.Register(context.Background(), openaiAuth); err != nil {
		t.Fatalf("Register openai auth: %v", err)
	}
	registry.GetGlobalRegistry().RegisterClient(openaiAuth.ID, openaiAuth.Provider, []*registry.ModelInfo{{ID: "deepseek-v4-pro"}})
	t.Cleanup(func() {
		registry.GetGlobalRegistry().UnregisterClient(openaiAuth.ID)
	})

	cfg := &sdkconfig.SDKConfig{}
	cfg.CompactFallback.Enabled = true
	cfg.CompactFallback.Model = "gpt-5.5" // no codex auth registered for this model
	cfg.CompactFallback.AppliesToProviders = []string{"openai-compatibility"}
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
	if openaiCompat.calls != 1 {
		t.Fatalf("openai-compatibility executor calls = %d, want 1 (fallback should be skipped)", openaiCompat.calls)
	}
	if openaiCompat.model != "deepseek-v4-pro" {
		t.Fatalf("openai-compat received model = %q, want %q (no rewrite)", openaiCompat.model, "deepseek-v4-pro")
	}
}

// TestCompactFallbackPreservesCodexNativeModel verifies that when the
// requested model is already served by a Codex provider, the fallback does
// not rewrite the model — Codex models compact natively and replacing them
// would silently drop the user's chosen model.
func TestCompactFallbackPreservesCodexNativeModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	codex := &providerCaptureExecutor{provider: "codex"}
	manager := coreauth.NewManager(nil, nil, nil)
	manager.RegisterExecutor(codex)

	codexAuth := &coreauth.Auth{ID: "codex-auth-3", Provider: codex.Identifier(), Status: coreauth.StatusActive}
	if _, err := manager.Register(context.Background(), codexAuth); err != nil {
		t.Fatalf("Register codex auth: %v", err)
	}
	// Register both the original conversation model and the configured fallback
	// model under the same Codex provider so the fallback model is reachable but
	// the original is also Codex-native and should NOT be rewritten.
	registry.GetGlobalRegistry().RegisterClient(codexAuth.ID, codexAuth.Provider, []*registry.ModelInfo{
		{ID: "gpt-5-codex"},
		{ID: "gpt-5.5"},
	})
	t.Cleanup(func() {
		registry.GetGlobalRegistry().UnregisterClient(codexAuth.ID)
	})

	cfg := &sdkconfig.SDKConfig{}
	cfg.CompactFallback.Enabled = true
	cfg.CompactFallback.Model = "gpt-5.5"
	cfg.CompactFallback.AppliesToProviders = []string{"openai-compatibility"}
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
	if codex.calls != 1 {
		t.Fatalf("codex executor calls = %d, want 1", codex.calls)
	}
	if codex.model != "gpt-5-codex" {
		t.Fatalf("codex received model = %q, want %q (must not rewrite codex-native models)", codex.model, "gpt-5-codex")
	}
}


// TestCompactFallbackWildcardMatchesCustomCompatName verifies that real-world
// OpenAI-compat configs (where compat.Name becomes the registered provider
// identifier — e.g. "opencode-go") trigger the fallback when the operator uses
// the wildcard "*" or an empty AppliesToProviders. This is the case that
// motivated the feature: deepseek-v4-pro is registered under provider
// "opencode-go" rather than the canonical "openai-compatibility" alias.
func TestCompactFallbackWildcardMatchesCustomCompatName(t *testing.T) {
	gin.SetMode(gin.TestMode)
	customCompat := &providerCaptureExecutor{provider: "opencode-go"}
	codex := &providerCaptureExecutor{provider: "codex"}
	manager := coreauth.NewManager(nil, nil, nil)
	manager.RegisterExecutor(customCompat)
	manager.RegisterExecutor(codex)

	customAuth := &coreauth.Auth{ID: "opencode-auth", Provider: customCompat.Identifier(), Status: coreauth.StatusActive}
	if _, err := manager.Register(context.Background(), customAuth); err != nil {
		t.Fatalf("Register opencode auth: %v", err)
	}
	codexAuth := &coreauth.Auth{ID: "codex-auth-wild", Provider: codex.Identifier(), Status: coreauth.StatusActive}
	if _, err := manager.Register(context.Background(), codexAuth); err != nil {
		t.Fatalf("Register codex auth: %v", err)
	}
	registry.GetGlobalRegistry().RegisterClient(customAuth.ID, customAuth.Provider, []*registry.ModelInfo{{ID: "deepseek-v4-pro"}})
	registry.GetGlobalRegistry().RegisterClient(codexAuth.ID, codexAuth.Provider, []*registry.ModelInfo{{ID: "gpt-5.5"}})
	t.Cleanup(func() {
		registry.GetGlobalRegistry().UnregisterClient(customAuth.ID)
		registry.GetGlobalRegistry().UnregisterClient(codexAuth.ID)
	})

	cfg := &sdkconfig.SDKConfig{}
	cfg.CompactFallback.Enabled = true
	cfg.CompactFallback.Model = "gpt-5.5"
	cfg.CompactFallback.AppliesToProviders = []string{"*"} // wildcard: every non-codex provider
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
	if customCompat.calls != 0 {
		t.Fatalf("opencode-go executor calls = %d, want 0 (wildcard fallback should bypass it)", customCompat.calls)
	}
	if codex.calls != 1 {
		t.Fatalf("codex executor calls = %d, want 1", codex.calls)
	}
	if codex.model != "gpt-5.5" {
		t.Fatalf("codex received model = %q, want %q", codex.model, "gpt-5.5")
	}
}


// TestCompactFallbackStripsReasoningItems verifies that when the fallback
// fires the helper removes any "type":"reasoning" entries from the input
// array before forwarding to Codex. Cross-provider reasoning blocks carry
// signatures Codex cannot verify and would be rejected with
// thinking_signature_invalid; dropping them makes the compact request
// portable while preserving conversation messages.
func TestCompactFallbackStripsReasoningItems(t *testing.T) {
	gin.SetMode(gin.TestMode)
	customCompat := &providerCaptureExecutor{provider: "opencode-go"}
	codex := &payloadCaptureExecutor{providerCaptureExecutor: providerCaptureExecutor{provider: "codex"}}
	manager := coreauth.NewManager(nil, nil, nil)
	manager.RegisterExecutor(customCompat)
	manager.RegisterExecutor(codex)

	customAuth := &coreauth.Auth{ID: "opencode-strip", Provider: customCompat.Identifier(), Status: coreauth.StatusActive}
	if _, err := manager.Register(context.Background(), customAuth); err != nil {
		t.Fatalf("Register opencode auth: %v", err)
	}
	codexAuth := &coreauth.Auth{ID: "codex-strip", Provider: codex.Identifier(), Status: coreauth.StatusActive}
	if _, err := manager.Register(context.Background(), codexAuth); err != nil {
		t.Fatalf("Register codex auth: %v", err)
	}
	registry.GetGlobalRegistry().RegisterClient(customAuth.ID, customAuth.Provider, []*registry.ModelInfo{{ID: "deepseek-v4-pro"}})
	registry.GetGlobalRegistry().RegisterClient(codexAuth.ID, codexAuth.Provider, []*registry.ModelInfo{{ID: "gpt-5.5"}})
	t.Cleanup(func() {
		registry.GetGlobalRegistry().UnregisterClient(customAuth.ID)
		registry.GetGlobalRegistry().UnregisterClient(codexAuth.ID)
	})

	cfg := &sdkconfig.SDKConfig{}
	cfg.CompactFallback.Enabled = true
	cfg.CompactFallback.Model = "gpt-5.5"
	cfg.CompactFallback.AppliesToProviders = []string{"*"}
	base := handlers.NewBaseAPIHandlers(cfg, manager)
	h := NewOpenAIResponsesAPIHandler(base)
	router := gin.New()
	router.POST("/v1/responses/compact", h.Compact)

	// Mixed input: a user message, a reasoning block (provider-private), an
	// assistant message, and another reasoning block. Both reasoning items
	// should be stripped; the two messages should remain in their original
	// order.
	body := `{"model":"deepseek-v4-pro","input":[` +
		`{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]},` +
		`{"type":"reasoning","summary":[{"type":"summary_text","text":"thinking..."}],"encrypted_content":"OPAQUE_FROM_DEEPSEEK_1"},` +
		`{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hi"}]},` +
		`{"type":"reasoning","summary":[{"type":"summary_text","text":"more thinking"}],"encrypted_content":"OPAQUE_FROM_DEEPSEEK_2"}` +
		`]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/responses/compact", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", resp.Code, http.StatusOK, resp.Body.String())
	}
	if codex.calls != 1 {
		t.Fatalf("codex executor calls = %d, want 1", codex.calls)
	}
	if codex.model != "gpt-5.5" {
		t.Fatalf("codex received model = %q, want %q", codex.model, "gpt-5.5")
	}
	// Verify input array no longer contains reasoning items.
	if reasoningCount := strings.Count(string(codex.payload), `"type":"reasoning"`); reasoningCount != 0 {
		t.Fatalf("forwarded payload still contains %d reasoning item(s); payload=%s", reasoningCount, string(codex.payload))
	}
	// Sanity-check that the two messages survived intact.
	if !strings.Contains(string(codex.payload), `"hello"`) || !strings.Contains(string(codex.payload), `"hi"`) {
		t.Fatalf("forwarded payload missing message content; payload=%s", string(codex.payload))
	}
	if strings.Contains(string(codex.payload), "OPAQUE_FROM_DEEPSEEK") {
		t.Fatalf("forwarded payload still contains foreign-provider encrypted_content; payload=%s", string(codex.payload))
	}
}

// payloadCaptureExecutor is a providerCaptureExecutor variant that also
// stores the request payload bytes so tests can assert on the exact JSON
// forwarded upstream (e.g. to verify provider-private fields were stripped).
type payloadCaptureExecutor struct {
	providerCaptureExecutor
	payload []byte
}

func (e *payloadCaptureExecutor) Execute(ctx context.Context, auth *coreauth.Auth, req coreexecutor.Request, opts coreexecutor.Options) (coreexecutor.Response, error) {
	e.calls++
	e.model = req.Model
	e.alt = opts.Alt
	e.payload = append(e.payload[:0], req.Payload...)
	return coreexecutor.Response{Payload: []byte(`{"ok":true}`)}, nil
}


// setUserAPIKeyMiddleware injects an API key into the gin context the way
// the production auth middleware does, so apikeypolicy.CheckRequest can
// resolve metadata for the caller.
func setUserAPIKeyMiddleware(apiKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("userApiKey", apiKey)
		c.Next()
	}
}

// TestCompactPolicyChecksOriginalModelBeforeFallback exercises approach B:
// when an API key is restricted to a specific model family (e.g. "deepseek*")
// and compact-fallback rewrites the user's requested model to a system
// fallback (e.g. "gpt-5.5"), the policy must be evaluated against the
// *original* model the user requested — not the substituted one. Otherwise
// keys with model whitelists silently lose compact support.
func TestCompactPolicyChecksOriginalModelBeforeFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	customCompat := &providerCaptureExecutor{provider: "opencode-go"}
	codex := &providerCaptureExecutor{provider: "codex"}
	manager := coreauth.NewManager(nil, nil, nil)
	manager.RegisterExecutor(customCompat)
	manager.RegisterExecutor(codex)

	customAuth := &coreauth.Auth{ID: "policy-opencode", Provider: customCompat.Identifier(), Status: coreauth.StatusActive}
	if _, err := manager.Register(context.Background(), customAuth); err != nil {
		t.Fatalf("Register opencode auth: %v", err)
	}
	codexAuth := &coreauth.Auth{ID: "policy-codex", Provider: codex.Identifier(), Status: coreauth.StatusActive}
	if _, err := manager.Register(context.Background(), codexAuth); err != nil {
		t.Fatalf("Register codex auth: %v", err)
	}
	registry.GetGlobalRegistry().RegisterClient(customAuth.ID, customAuth.Provider, []*registry.ModelInfo{{ID: "deepseek-v4-pro"}})
	registry.GetGlobalRegistry().RegisterClient(codexAuth.ID, codexAuth.Provider, []*registry.ModelInfo{{ID: "gpt-5.5"}})
	t.Cleanup(func() {
		registry.GetGlobalRegistry().UnregisterClient(customAuth.ID)
		registry.GetGlobalRegistry().UnregisterClient(codexAuth.ID)
	})

	const apiKey = "test-deepseek-only"
	cfg := &sdkconfig.SDKConfig{}
	cfg.APIKeys = []string{apiKey}
	cfg.APIKeyMetadata = map[string]internalconfig.APIKeyMetadata{
		internalconfig.APIKeyID(apiKey): {
			AllowedModels: []string{"deepseek*"},
		},
	}
	cfg.CompactFallback.Enabled = true
	cfg.CompactFallback.Model = "gpt-5.5"
	cfg.CompactFallback.AppliesToProviders = []string{"*"}
	base := handlers.NewBaseAPIHandlers(cfg, manager)
	h := NewOpenAIResponsesAPIHandler(base)
	router := gin.New()
	router.Use(setUserAPIKeyMiddleware(apiKey))
	router.POST("/v1/responses/compact", h.Compact)

	// User requests deepseek-v4-pro (allowed by deepseek*). The proxy will
	// internally substitute the model to gpt-5.5 (which is NOT in the
	// allow-list) before reaching the executor. The pre-check on the original
	// model must pass; the post-substitution check must be skipped.
	req := httptest.NewRequest(http.MethodPost, "/v1/responses/compact", strings.NewReader(`{"model":"deepseek-v4-pro","input":"hi"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", resp.Code, resp.Body.String())
	}
	if codex.calls != 1 {
		t.Fatalf("codex executor calls = %d, want 1 (fallback should fire)", codex.calls)
	}
	if codex.model != "gpt-5.5" {
		t.Fatalf("codex received model = %q, want gpt-5.5", codex.model)
	}
}

// TestCompactPolicyRejectsDisallowedOriginalModel verifies the inverse: when
// the user requests a model that is NOT in the key's allowed-models list, the
// compact endpoint must reject the request with 403 *before* applying any
// fallback substitution. Otherwise a key restricted to "deepseek*" could
// quietly perform compact on any model just by hitting the compact endpoint.
func TestCompactPolicyRejectsDisallowedOriginalModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	customCompat := &providerCaptureExecutor{provider: "opencode-go"}
	codex := &providerCaptureExecutor{provider: "codex"}
	manager := coreauth.NewManager(nil, nil, nil)
	manager.RegisterExecutor(customCompat)
	manager.RegisterExecutor(codex)

	customAuth := &coreauth.Auth{ID: "deny-opencode", Provider: customCompat.Identifier(), Status: coreauth.StatusActive}
	if _, err := manager.Register(context.Background(), customAuth); err != nil {
		t.Fatalf("Register opencode auth: %v", err)
	}
	codexAuth := &coreauth.Auth{ID: "deny-codex", Provider: codex.Identifier(), Status: coreauth.StatusActive}
	if _, err := manager.Register(context.Background(), codexAuth); err != nil {
		t.Fatalf("Register codex auth: %v", err)
	}
	registry.GetGlobalRegistry().RegisterClient(customAuth.ID, customAuth.Provider, []*registry.ModelInfo{
		{ID: "deepseek-v4-pro"},
		{ID: "mimo-v2.5-pro"},
	})
	registry.GetGlobalRegistry().RegisterClient(codexAuth.ID, codexAuth.Provider, []*registry.ModelInfo{{ID: "gpt-5.5"}})
	t.Cleanup(func() {
		registry.GetGlobalRegistry().UnregisterClient(customAuth.ID)
		registry.GetGlobalRegistry().UnregisterClient(codexAuth.ID)
	})

	const apiKey = "test-deepseek-only-deny"
	cfg := &sdkconfig.SDKConfig{}
	cfg.APIKeys = []string{apiKey}
	cfg.APIKeyMetadata = map[string]internalconfig.APIKeyMetadata{
		internalconfig.APIKeyID(apiKey): {
			AllowedModels: []string{"deepseek*"},
		},
	}
	cfg.CompactFallback.Enabled = true
	cfg.CompactFallback.Model = "gpt-5.5"
	cfg.CompactFallback.AppliesToProviders = []string{"*"}
	base := handlers.NewBaseAPIHandlers(cfg, manager)
	h := NewOpenAIResponsesAPIHandler(base)
	router := gin.New()
	router.Use(setUserAPIKeyMiddleware(apiKey))
	router.POST("/v1/responses/compact", h.Compact)

	// User requests mimo-v2.5-pro — not in deepseek* whitelist. Must be
	// rejected before substitution; the error must mention the *original*
	// model name so operators can debug the policy violation, not the
	// internal fallback target.
	req := httptest.NewRequest(http.MethodPost, "/v1/responses/compact", strings.NewReader(`{"model":"mimo-v2.5-pro","input":"hi"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", resp.Code, resp.Body.String())
	}
	if codex.calls != 0 {
		t.Fatalf("codex executor calls = %d, want 0 (request should be denied before execution)", codex.calls)
	}
	if customCompat.calls != 0 {
		t.Fatalf("opencode-go executor calls = %d, want 0", customCompat.calls)
	}
	if !strings.Contains(resp.Body.String(), "mimo-v2.5-pro") {
		t.Fatalf("error body should mention the original requested model; got: %s", resp.Body.String())
	}
	if strings.Contains(resp.Body.String(), "gpt-5.5") {
		t.Fatalf("error body must NOT mention the internal fallback model gpt-5.5; got: %s", resp.Body.String())
	}
}
