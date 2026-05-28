// Package registry — Kiro (AWS CodeWhisperer / Amazon Q Developer) static model
// definitions. The remote models.json catalog does not currently ship Kiro
// entries, so the canonical list is held here. The Kiro executor accepts any
// of these IDs verbatim (e.g. "claude-sonnet-4-5") and the embedded translator
// already normalises common aliases such as "claude-sonnet-4.5".
package registry

// GetKiroModels returns the static Kiro model catalog. Availability ultimately
// depends on the user's Kiro subscription tier — the management UI lists the
// full catalog, and the upstream returns a model-not-available error for tiers
// where a particular model isn't authorised. Numbers (context length, max
// completion, credit multiplier mentioned in description) are taken from
// Kiro's own published table and are subject to change without notice from AWS.
func GetKiroModels() []*ModelInfo {
	thinking := func() *ThinkingSupport {
		return &ThinkingSupport{Min: 1024, Max: 32000, ZeroAllowed: true, DynamicAllowed: true}
	}

	return []*ModelInfo{
		// --- Claude family (served via CodeWhisperer) ---
		{
			ID: "auto", Object: "model", Created: 1732752000, OwnedBy: "aws", Type: "kiro",
			DisplayName: "Kiro Auto", Description: "Automatic model selection by Kiro",
			ContextLength: 200000, MaxCompletionTokens: 64000, Thinking: thinking(),
		},
		{
			ID: "claude-sonnet-4-5", Object: "model", Created: 1732752000, OwnedBy: "aws", Type: "kiro",
			DisplayName: "Kiro Claude Sonnet 4.5", Description: "Claude Sonnet 4.5 via Kiro (1.3x credit)",
			ContextLength: 200000, MaxCompletionTokens: 64000, Thinking: thinking(),
		},
		{
			ID: "claude-haiku-4-5", Object: "model", Created: 1732752000, OwnedBy: "aws", Type: "kiro",
			DisplayName: "Kiro Claude Haiku 4.5", Description: "Claude Haiku 4.5 via Kiro (0.4x credit)",
			ContextLength: 200000, MaxCompletionTokens: 64000, Thinking: thinking(),
		},
		{
			ID: "claude-sonnet-4", Object: "model", Created: 1732752000, OwnedBy: "aws", Type: "kiro",
			DisplayName: "Kiro Claude Sonnet 4", Description: "Claude Sonnet 4 via Kiro (1.3x credit)",
			ContextLength: 200000, MaxCompletionTokens: 64000, Thinking: thinking(),
		},
		{
			ID: "claude-opus-4-5", Object: "model", Created: 1732752000, OwnedBy: "aws", Type: "kiro",
			DisplayName: "Kiro Claude Opus 4.5", Description: "Claude Opus 4.5 via Kiro (2.2x credit, paid tier)",
			ContextLength: 200000, MaxCompletionTokens: 64000, Thinking: thinking(),
		},
		{
			ID: "claude-3-7-sonnet", Object: "model", Created: 1732752000, OwnedBy: "aws", Type: "kiro",
			DisplayName: "Kiro Claude 3.7 Sonnet", Description: "Claude 3.7 Sonnet via Kiro",
			ContextLength: 200000, MaxCompletionTokens: 64000, Thinking: thinking(),
		},
		{
			ID: "claude-3-5-sonnet", Object: "model", Created: 1732752000, OwnedBy: "aws", Type: "kiro",
			DisplayName: "Kiro Claude 3.5 Sonnet", Description: "Claude 3.5 Sonnet via Kiro",
			ContextLength: 200000, MaxCompletionTokens: 64000, Thinking: thinking(),
		},
		{
			ID: "claude-3-5-haiku", Object: "model", Created: 1732752000, OwnedBy: "aws", Type: "kiro",
			DisplayName: "Kiro Claude 3.5 Haiku", Description: "Claude 3.5 Haiku via Kiro",
			ContextLength: 200000, MaxCompletionTokens: 64000, Thinking: thinking(),
		},

		// --- Open-weights and partner models also exposed via Kiro chat surface ---
		{
			ID: "deepseek-3-2", Object: "model", Created: 1732752000, OwnedBy: "aws", Type: "kiro",
			DisplayName: "Kiro DeepSeek 3.2", Description: "DeepSeek 3.2 via Kiro",
			ContextLength: 128000, MaxCompletionTokens: 32768, Thinking: thinking(),
		},
		{
			ID: "minimax-m2-1", Object: "model", Created: 1732752000, OwnedBy: "aws", Type: "kiro",
			DisplayName: "Kiro MiniMax M2.1", Description: "MiniMax M2.1 via Kiro",
			ContextLength: 200000, MaxCompletionTokens: 64000, Thinking: thinking(),
		},
		{
			ID: "qwen3-coder-next", Object: "model", Created: 1732752000, OwnedBy: "aws", Type: "kiro",
			DisplayName: "Kiro Qwen3 Coder Next", Description: "Qwen3 Coder Next via Kiro",
			ContextLength: 128000, MaxCompletionTokens: 32768, Thinking: thinking(),
		},
		{
			ID: "glm-5", Object: "model", Created: 1732752000, OwnedBy: "aws", Type: "kiro",
			DisplayName: "Kiro GLM 5", Description: "GLM 5 via Kiro",
			ContextLength: 128000, MaxCompletionTokens: 32768, Thinking: thinking(),
		},
	}
}
