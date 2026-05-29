// Package config provides configuration management for the CLI Proxy API server.
// It handles loading and parsing YAML configuration files, and provides structured
// access to application settings including server port, authentication directory,
// debug settings, proxy configuration, and API keys.
package config

const DefaultStreamingKeepAliveSeconds = 15

// SDKConfig represents the application's configuration, loaded from a YAML file.
type SDKConfig struct {
	// ProxyURL is the URL of an optional proxy server to use for outbound requests.
	ProxyURL string `yaml:"proxy-url" json:"proxy-url"`

	// DisableImageGeneration controls whether the built-in image_generation tool is injected/allowed.
	//
	// Supported values:
	//   - false (default): image_generation is enabled everywhere (normal behavior).
	//   - true: image_generation is disabled everywhere. The server stops injecting it, removes it from request payloads,
	//     and returns 404 for /v1/images/generations and /v1/images/edits.
	//   - "chat": disable image_generation injection for all non-images endpoints (e.g. /v1/responses, /v1/chat/completions),
	//     while keeping /v1/images/generations and /v1/images/edits enabled and preserving image_generation there.
	DisableImageGeneration DisableImageGenerationMode `yaml:"disable-image-generation" json:"disable-image-generation"`

	// EnableGeminiCLIEndpoint controls whether Gemini CLI internal endpoints (/v1internal:*) are enabled.
	// Default is false for safety; when false, /v1internal:* requests are rejected.
	EnableGeminiCLIEndpoint bool `yaml:"enable-gemini-cli-endpoint" json:"enable-gemini-cli-endpoint"`

	// ForceModelPrefix requires explicit model prefixes (e.g., "teamA/gemini-3-pro-preview")
	// to target prefixed credentials. When false, unprefixed model requests may use prefixed
	// credentials as well.
	ForceModelPrefix bool `yaml:"force-model-prefix" json:"force-model-prefix"`

	// RequestLog enables or disables detailed request logging functionality.
	RequestLog bool `yaml:"request-log" json:"request-log"`

	// APIKeys is a list of keys for authenticating clients to this proxy server.
	APIKeys []string `yaml:"api-keys" json:"api-keys"`

	// APIKeyMetadata stores operator-owned lifecycle and policy metadata for top-level APIKeys.
	// Entries are keyed by APIKeyID(api-key), so the secret value can rotate without changing
	// the legacy api-keys list shape.
	APIKeyMetadata map[string]APIKeyMetadata `yaml:"api-key-metadata,omitempty" json:"api-key-metadata,omitempty"`

	// PassthroughHeaders controls whether upstream response headers are forwarded to downstream clients.
	// Default is false (disabled).
	PassthroughHeaders bool `yaml:"passthrough-headers" json:"passthrough-headers"`

	// Streaming configures server-side streaming behavior (keep-alives and safe bootstrap retries).
	Streaming StreamingConfig `yaml:"streaming" json:"streaming"`

	// NonStreamKeepAliveInterval controls how often blank lines are emitted for non-streaming responses.
	// <= 0 disables keep-alives. Value is in seconds.
	NonStreamKeepAliveInterval int `yaml:"nonstream-keepalive-interval,omitempty" json:"nonstream-keepalive-interval,omitempty"`

	// CompactFallback rewrites the model field for /v1/responses/compact requests when
	// the requested model is served by a provider that does not natively support the
	// Codex /responses/compact endpoint (e.g. third-party openai-compatibility upstreams
	// like opencode.ai which only expose /chat/completions). When enabled, the proxy
	// substitutes the configured Codex-capable model (e.g. "gpt-5.5") so the request is
	// routed through the Codex executor that hits chatgpt.com/backend-api/codex.
	//
	// Example:
	//   compact-fallback:
	//     enabled: true
	//     model: "gpt-5.5"
	//     applies-to-providers: ["openai-compatibility"]
	CompactFallback CompactFallbackConfig `yaml:"compact-fallback,omitempty" json:"compact-fallback,omitempty"`

	// CustomCompact enables LLM-based context compaction when compact-fallback
	// is disabled. Instead of forwarding the request to the Codex compact
	// endpoint, the proxy extracts conversation context from the compact input,
	// calls /chat/completions with a summarization prompt using the configured
	// model (which must be any model registered in CLIProxy), and wraps the
	// result in the Responses API compact response format.
	//
	// Example:
	//   custom-compact:
	//     enabled: true
	//     model: "deepseek-v4-pro"
	CustomCompact CustomCompactConfig `yaml:"custom-compact,omitempty" json:"custom-compact,omitempty"`

	// GuidelineInjection controls whether a project-level guideline (the
	// agent-harness-kit recommendation by default) is prepended to the system
	// prompt of inbound requests across all four formats (claude / openai-chat
	// / openai-responses / gemini). It defaults to ON when the block is
	// omitted entirely from config.yaml; set `enabled: false` to opt out.
	GuidelineInjection GuidelineInjectionConfig `yaml:"guideline-injection,omitempty" json:"guideline-injection,omitempty"`
}

// GuidelineInjectionConfig toggles and customizes system-prompt guideline
// injection performed by the package internal/guideline. The zero value
// (block omitted from config.yaml) results in injection enabled with the
// built-in agent-harness-kit content prepended to whatever the client sent.
type GuidelineInjectionConfig struct {
	// Enabled is a tri-state pointer so we can distinguish "not set" (default
	// ON) from "explicitly false" (opt-out). Use IsEnabled() to read.
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`

	// Position controls where the content is merged relative to any system
	// prompt the client already sent. Allowed values: "prepend" (default),
	// "append", "replace".
	Position string `yaml:"position,omitempty" json:"position,omitempty"`

	// Content overrides the built-in guideline. When empty the package
	// internal/guideline default (agent-harness-kit recommendation) is used.
	Content string `yaml:"content,omitempty" json:"content,omitempty"`
}

// IsEnabled returns true unless the operator has explicitly set
// `guideline-injection.enabled: false` in config.yaml.
func (c GuidelineInjectionConfig) IsEnabled() bool {
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

// EffectivePosition returns the configured position or the default prepend.
func (c GuidelineInjectionConfig) EffectivePosition() string {
	if c.Position == "" {
		return "prepend"
	}
	return c.Position
}

// CompactFallbackConfig configures model substitution for /v1/responses/compact
// requests when the originating model's provider cannot serve the Codex compact
// endpoint. The substitution is purely an upstream-routing rewrite: response data
// is still returned to the caller verbatim. The fallback is skipped when no Codex
// auth is registered for the substitute model so callers never see a routing error
// they cannot remediate themselves.
type CompactFallbackConfig struct {
	// Enabled toggles the compact fallback behavior. Default false.
	Enabled bool `yaml:"enabled" json:"enabled"`

	// Model is the substitute model name used when fallback fires (e.g. "gpt-5.5").
	// Must resolve to a registered Codex provider model at runtime.
	Model string `yaml:"model" json:"model"`

	// AppliesToProviders is the whitelist of provider identifiers (as returned by
	// util.GetProviderName) that trigger the fallback. The whitelist matches by
	// exact provider identifier; the special token "*" or an empty list matches
	// every non-codex provider so custom OpenAI-compat names (e.g. "opencode-go",
	// "9router") are covered without forcing the operator to enumerate each one.
	// Codex-native models always bypass the fallback regardless of this setting.
	AppliesToProviders []string `yaml:"applies-to-providers,omitempty" json:"applies-to-providers,omitempty"`

	// TriggerLog enables async file logging of compact request input and
	// response output when the compact-fallback path fires. Logs are written
	// to the logs/ directory in JSON format. The write happens in a separate
	// goroutine so it never slows the compact response, and write errors are
	// silently logged without affecting the compact flow.
	TriggerLog bool `yaml:"trigger-log,omitempty" json:"trigger-log,omitempty"`
}

// CustomCompactConfig configures LLM-based context compaction for
// /v1/responses/compact requests. When enabled (and compact-fallback is
// disabled), the proxy does not forward the compact request to a Codex
// endpoint. Instead it extracts the conversation from the compact input,
// builds a summarization prompt, calls /chat/completions with the configured
// model through the proxy's own provider system, and wraps the LLM output
// in the Responses API compact response format.
type CustomCompactConfig struct {
	// Enabled toggles custom compact. Default false.
	Enabled bool `yaml:"enabled" json:"enabled"`

	// Model is the model name to use for the LLM compaction call (e.g.
	// "deepseek-v4-pro"). Must be any model registered in CLIProxy.
	Model string `yaml:"model" json:"model"`

	// MaxTokens limits the LLM response length. Default 4096.
	MaxTokens int `yaml:"max-tokens,omitempty" json:"max-tokens,omitempty"`

	// Temperature controls the LLM sampling temperature. Default 0.2.
	Temperature *float64 `yaml:"temperature,omitempty" json:"temperature,omitempty"`

	// MaxRetries is the maximum number of retry attempts when the LLM
	// response fails validation. Default 1.
	MaxRetries int `yaml:"max-retries,omitempty" json:"max-retries,omitempty"`

	// TriggerLog enables async file logging of custom compact request input
	// and LLM response output. Logs are written to the logs/ directory in
	// JSON format. The write happens in a separate goroutine so it never
	// slows the compact response. Default false.
	TriggerLog bool `yaml:"trigger-log,omitempty" json:"trigger-log,omitempty"`
}

// EffectiveMaxTokens returns the configured max-tokens or the default 4096.
func (c CustomCompactConfig) EffectiveMaxTokens() int {
	if c.MaxTokens > 0 {
		return c.MaxTokens
	}
	return 4096
}

// EffectiveTemperature returns the configured temperature or the default 0.2.
func (c CustomCompactConfig) EffectiveTemperature() float64 {
	if c.Temperature != nil {
		return *c.Temperature
	}
	return 0.2
}

// EffectiveMaxRetries returns the configured max-retries or the default 1.
func (c CustomCompactConfig) EffectiveMaxRetries() int {
	if c.MaxRetries > 0 {
		return c.MaxRetries
	}
	return 1
}

// StreamingConfig holds server streaming behavior configuration.
type StreamingConfig struct {
	// KeepAliveSeconds controls how often the server emits SSE heartbeats (": keep-alive\n\n").
	// <= 0 disables keep-alives. Default is 15.
	KeepAliveSeconds int `yaml:"keepalive-seconds,omitempty" json:"keepalive-seconds,omitempty"`

	// BootstrapRetries controls how many times the server may retry a streaming request before any bytes are sent,
	// to allow auth rotation / transient recovery.
	// <= 0 disables bootstrap retries. Default is 0.
	BootstrapRetries int `yaml:"bootstrap-retries,omitempty" json:"bootstrap-retries,omitempty"`
}
