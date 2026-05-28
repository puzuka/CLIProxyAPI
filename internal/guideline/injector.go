package guideline

import (
	"strings"
)

// Format identifies the inbound request format that the injector should
// operate on. The constants intentionally match the strings already used in
// CLIProxyAPI handler routing so call sites can pass through the same value
// they use elsewhere.
const (
	FormatClaude          = "claude"
	FormatOpenAIChat      = "openai-chat"
	FormatOpenAIResponses = "openai-responses"
	FormatGemini          = "gemini"
)

// Position controls where guideline content is inserted relative to any
// system prompt the client already supplied.
const (
	PositionPrepend = "prepend"
	PositionAppend  = "append"
	PositionReplace = "replace"
)

// Inject inserts guideline content into the system-prompt slot of body
// according to format-specific schema rules.
//
// Behavior:
//   - body and content are returned untouched if either is empty.
//   - position defaults to PositionPrepend when blank.
//   - format must be one of the Format* constants; unknown formats are a
//     no-op so unrelated transports (count-tokens, list-models, etc.)
//     never accidentally rewrite payloads.
//
// Returns a new []byte; the input slice is not mutated.
func Inject(format string, body []byte, content string, position string) []byte {
	if len(body) == 0 || strings.TrimSpace(content) == "" {
		return body
	}
	if position == "" {
		position = PositionPrepend
	}
	switch format {
	case FormatClaude:
		return injectClaude(body, content, position)
	case FormatOpenAIChat:
		return injectOpenAIChat(body, content, position)
	case FormatOpenAIResponses:
		return injectOpenAIResponses(body, content, position)
	case FormatGemini:
		return injectGemini(body, content, position)
	default:
		return body
	}
}

// MergeText merges existing and new prompt fragments according to position.
// It is exported because format-specific injectors share the same merge
// rules but each implements its own JSON structure handling.
func MergeText(existing, addition, position string) string {
	if existing == "" {
		return addition
	}
	switch position {
	case PositionAppend:
		return existing + "\n\n" + addition
	case PositionReplace:
		return addition
	default: // PositionPrepend
		return addition + "\n\n" + existing
	}
}
