// Package registry — Kiro (AWS CodeWhisperer / Amazon Q Developer) static model
// definitions. The remote models.json catalog does not currently ship Kiro
// entries, so the canonical list is held here and keeps the catalog populated
// even before any Kiro auth is registered (or when ListAvailableModels fails).
//
// At runtime the service additionally calls Kiro ListAvailableModels per auth
// (see sdk/cliproxy/service.go registerModelsForAuth) and merges the dynamic
// list with this static metadata so that newly-launched models surface
// automatically without code changes.
package registry

// GetKiroModels returns the static Kiro model catalog. The list mirrors the
// current Kiro IDE ListAvailableModels API response (origin=AI_EDITOR) so the
// catalog stays usable as a fallback when the live API is unreachable.
//
// Keep ContextLength / MaxCompletionTokens in sync with Kiro's tokenLimits;
// the credit multiplier mentioned in Description tracks Kiro's published rate
// table and is subject to change without notice from AWS.
func GetKiroModels() []*ModelInfo {
	thinking := func(maxOutput int) *ThinkingSupport {
		max := 32000
		if maxOutput > 0 && maxOutput < max {
			max = maxOutput
		}
		return &ThinkingSupport{Min: 1024, Max: max, ZeroAllowed: true, DynamicAllowed: true}
	}

	return []*ModelInfo{
		// --- Auto routing ---
		{
			ID: "auto", Object: "model", Created: 1732752000, OwnedBy: "aws", Type: "kiro",
			DisplayName: "Kiro Auto", Description: "Models chosen by task for optimal usage and consistent quality (1.0x credit)",
			ContextLength: 1000000, MaxCompletionTokens: 64000, Thinking: thinking(32000),
		},

		// --- Claude family (served via CodeWhisperer) ---
		{
			ID: "claude-opus-4-8", Object: "model", Created: 1732752000, OwnedBy: "aws", Type: "kiro",
			DisplayName: "Kiro Claude Opus 4.8", Description: "Experimental preview of Claude Opus 4.8 model with 1M context window (2.2x credit)",
			ContextLength: 1000000, MaxCompletionTokens: 128000, Thinking: thinking(128000),
		},
		{
			ID: "claude-opus-4-7", Object: "model", Created: 1732752000, OwnedBy: "aws", Type: "kiro",
			DisplayName: "Kiro Claude Opus 4.7", Description: "Experimental preview of Claude Opus 4.7 model with 1M context window (2.2x credit)",
			ContextLength: 1000000, MaxCompletionTokens: 128000, Thinking: thinking(128000),
		},
		{
			ID: "claude-opus-4-6", Object: "model", Created: 1732752000, OwnedBy: "aws", Type: "kiro",
			DisplayName: "Kiro Claude Opus 4.6", Description: "Claude Opus 4.6 via Kiro (2.2x credit)",
			ContextLength: 1000000, MaxCompletionTokens: 64000, Thinking: thinking(64000),
		},
		{
			ID: "claude-sonnet-4-6", Object: "model", Created: 1732752000, OwnedBy: "aws", Type: "kiro",
			DisplayName: "Kiro Claude Sonnet 4.6", Description: "The latest Claude Sonnet model with 1M context window (1.3x credit)",
			ContextLength: 1000000, MaxCompletionTokens: 64000, Thinking: thinking(64000),
		},
		{
			ID: "claude-opus-4-5", Object: "model", Created: 1732752000, OwnedBy: "aws", Type: "kiro",
			DisplayName: "Kiro Claude Opus 4.5", Description: "Claude Opus 4.5 via Kiro (2.2x credit)",
			ContextLength: 200000, MaxCompletionTokens: 64000, Thinking: thinking(64000),
		},
		{
			ID: "claude-sonnet-4-5", Object: "model", Created: 1732752000, OwnedBy: "aws", Type: "kiro",
			DisplayName: "Kiro Claude Sonnet 4.5", Description: "Claude Sonnet 4.5 via Kiro (1.3x credit)",
			ContextLength: 200000, MaxCompletionTokens: 64000, Thinking: thinking(64000),
		},
		{
			ID: "claude-sonnet-4", Object: "model", Created: 1732752000, OwnedBy: "aws", Type: "kiro",
			DisplayName: "Kiro Claude Sonnet 4", Description: "Hybrid reasoning and coding for regular use (1.3x credit)",
			ContextLength: 200000, MaxCompletionTokens: 64000, Thinking: thinking(64000),
		},
		{
			ID: "claude-haiku-4-5", Object: "model", Created: 1732752000, OwnedBy: "aws", Type: "kiro",
			DisplayName: "Kiro Claude Haiku 4.5", Description: "The latest Claude Haiku model (0.4x credit)",
			ContextLength: 200000, MaxCompletionTokens: 64000, Thinking: thinking(64000),
		},

		// --- Open-weights and partner models also exposed via Kiro chat surface ---
		{
			ID: "deepseek-3-2", Object: "model", Created: 1732752000, OwnedBy: "aws", Type: "kiro",
			DisplayName: "Kiro DeepSeek v3.2", Description: "Experimental preview of DeepSeek V3.2 (0.25x credit)",
			ContextLength: 164000, MaxCompletionTokens: 64000, Thinking: thinking(64000),
		},
		{
			ID: "minimax-m2-5", Object: "model", Created: 1732752000, OwnedBy: "aws", Type: "kiro",
			DisplayName: "Kiro MiniMax M2.5", Description: "The MiniMax M2.5 model (0.25x credit)",
			ContextLength: 196000, MaxCompletionTokens: 64000, Thinking: thinking(64000),
		},
		{
			ID: "minimax-m2-1", Object: "model", Created: 1732752000, OwnedBy: "aws", Type: "kiro",
			DisplayName: "Kiro MiniMax M2.1", Description: "Experimental preview of MiniMax M2.1 (0.15x credit)",
			ContextLength: 196000, MaxCompletionTokens: 64000, Thinking: thinking(64000),
		},
		{
			ID: "glm-5", Object: "model", Created: 1732752000, OwnedBy: "aws", Type: "kiro",
			DisplayName: "Kiro GLM 5", Description: "The GLM-5 model (0.5x credit)",
			ContextLength: 200000, MaxCompletionTokens: 64000, Thinking: thinking(64000),
		},
		{
			ID: "qwen3-coder-next", Object: "model", Created: 1732752000, OwnedBy: "aws", Type: "kiro",
			DisplayName: "Kiro Qwen3 Coder Next", Description: "Experimental preview of Qwen3 Coder Next (0.05x credit)",
			ContextLength: 256000, MaxCompletionTokens: 64000, Thinking: thinking(64000),
		},
	}
}
