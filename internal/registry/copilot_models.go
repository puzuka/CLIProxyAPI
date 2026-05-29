package registry

// GetCopilotModels returns the static GitHub Copilot model catalog used as a
// startup fallback. GitHub can change the live catalog per account, but the
// proxy needs a usable baseline immediately after OAuth login.
func GetCopilotModels() []*ModelInfo {
	created := int64(1704067200)
	textModalities := []string{"TEXT", "IMAGE"}
	outputModalities := []string{"TEXT"}
	thinking := func(maxOutput int) *ThinkingSupport {
		max := 64000
		if maxOutput > 0 && maxOutput < max {
			max = maxOutput
		}
		return &ThinkingSupport{Min: 1024, Max: max, ZeroAllowed: true, DynamicAllowed: true}
	}
	return []*ModelInfo{
		{
			ID: "gpt-5", Object: "model", Created: created, OwnedBy: "github-copilot", Type: "copilot",
			DisplayName: "Copilot GPT-5", Description: "GPT-5 via GitHub Copilot",
			ContextLength: 272000, MaxCompletionTokens: 128000, SupportedInputModalities: textModalities, SupportedOutputModalities: outputModalities, Thinking: thinking(128000),
		},
		{
			ID: "gpt-5-mini", Object: "model", Created: created, OwnedBy: "github-copilot", Type: "copilot",
			DisplayName: "Copilot GPT-5 Mini", Description: "GPT-5 mini via GitHub Copilot",
			ContextLength: 272000, MaxCompletionTokens: 128000, SupportedInputModalities: textModalities, SupportedOutputModalities: outputModalities, Thinking: thinking(128000),
		},
		{
			ID: "gpt-4.1", Object: "model", Created: created, OwnedBy: "github-copilot", Type: "copilot",
			DisplayName: "Copilot GPT-4.1", Description: "GPT-4.1 via GitHub Copilot",
			ContextLength: 128000, MaxCompletionTokens: 16000, SupportedInputModalities: textModalities, SupportedOutputModalities: outputModalities,
		},
		{
			ID: "gpt-4o", Object: "model", Created: created, OwnedBy: "github-copilot", Type: "copilot",
			DisplayName: "Copilot GPT-4o", Description: "GPT-4o via GitHub Copilot",
			ContextLength: 128000, MaxCompletionTokens: 16000, SupportedInputModalities: textModalities, SupportedOutputModalities: outputModalities,
		},
		{
			ID: "gemini-2.5-pro", Object: "model", Created: created, OwnedBy: "github-copilot", Type: "copilot",
			DisplayName: "Copilot Gemini 2.5 Pro", Description: "Gemini 2.5 Pro via GitHub Copilot",
			ContextLength: 1000000, MaxCompletionTokens: 64000, SupportedInputModalities: textModalities, SupportedOutputModalities: outputModalities, Thinking: thinking(64000),
		},
		{
			ID: "grok-code-fast-1", Object: "model", Created: created, OwnedBy: "github-copilot", Type: "copilot",
			DisplayName: "Copilot Grok Code Fast 1", Description: "Grok Code Fast 1 via GitHub Copilot",
			ContextLength: 256000, MaxCompletionTokens: 64000, SupportedInputModalities: textModalities, SupportedOutputModalities: outputModalities,
		},
	}
}
