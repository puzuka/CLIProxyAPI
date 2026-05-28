// Package helps provides Claude-specific tokenizer helpers used by the Kiro executor
// (and any future Claude-format executors that need prompt-token estimation without an
// upstream usage object).
package helps

import (
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tiktoken-go/tokenizer"
)

// CountClaudeChatTokens approximates prompt tokens for Claude API chat payloads.
// It walks the Claude `system` text, `messages[].content` (handling text, image,
// tool_use, tool_result, thinking blocks) and the `tools` array, joins the textual
// segments and counts them with the supplied tokenizer codec. Image tokens are
// estimated heuristically based on width × height when present.
func CountClaudeChatTokens(enc tokenizer.Codec, payload []byte) (int64, error) {
	if enc == nil {
		return 0, fmt.Errorf("encoder is nil")
	}
	if len(payload) == 0 {
		return 0, nil
	}

	root := gjson.ParseBytes(payload)
	segments := make([]string, 0, 32)
	imageTokens := 0

	collectClaudeContent(root.Get("system"), &segments, &imageTokens)
	collectClaudeMessages(root.Get("messages"), &segments, &imageTokens)
	collectClaudeTools(root.Get("tools"), &segments)

	joined := strings.TrimSpace(strings.Join(segments, "\n"))
	if joined == "" {
		return int64(imageTokens), nil
	}
	count, err := enc.Count(joined)
	if err != nil {
		return 0, err
	}
	return int64(count + imageTokens), nil
}

func collectClaudeMessages(messages gjson.Result, segments *[]string, imageTokens *int) {
	if !messages.Exists() || !messages.IsArray() {
		return
	}
	messages.ForEach(func(_, message gjson.Result) bool {
		addIfNotEmpty(segments, message.Get("role").String())
		collectClaudeContent(message.Get("content"), segments, imageTokens)
		return true
	})
}

func collectClaudeContent(content gjson.Result, segments *[]string, imageTokens *int) {
	if !content.Exists() {
		return
	}
	if content.Type == gjson.String {
		addIfNotEmpty(segments, content.String())
		return
	}
	if content.IsArray() {
		content.ForEach(func(_, part gjson.Result) bool {
			partType := part.Get("type").String()
			switch partType {
			case "text":
				addIfNotEmpty(segments, part.Get("text").String())
			case "image":
				source := part.Get("source")
				width := source.Get("width").Float()
				height := source.Get("height").Float()
				if imageTokens != nil {
					*imageTokens += estimateImageTokens(width, height)
				}
			case "tool_use":
				addIfNotEmpty(segments, part.Get("id").String())
				addIfNotEmpty(segments, part.Get("name").String())
				if input := part.Get("input"); input.Exists() {
					addIfNotEmpty(segments, input.Raw)
				}
			case "tool_result":
				addIfNotEmpty(segments, part.Get("tool_use_id").String())
				collectClaudeContent(part.Get("content"), segments, imageTokens)
			case "thinking":
				addIfNotEmpty(segments, part.Get("thinking").String())
			default:
				if part.Type == gjson.String {
					addIfNotEmpty(segments, part.String())
				} else if part.Type == gjson.JSON {
					addIfNotEmpty(segments, part.Raw)
				}
			}
			return true
		})
		return
	}
	if content.Type == gjson.JSON {
		addIfNotEmpty(segments, content.Raw)
	}
}

func collectClaudeTools(tools gjson.Result, segments *[]string) {
	if !tools.Exists() || !tools.IsArray() {
		return
	}
	tools.ForEach(func(_, tool gjson.Result) bool {
		addIfNotEmpty(segments, tool.Get("name").String())
		addIfNotEmpty(segments, tool.Get("description").String())
		if inputSchema := tool.Get("input_schema"); inputSchema.Exists() {
			addIfNotEmpty(segments, inputSchema.Raw)
		}
		return true
	})
}

// estimateImageTokens estimates Claude image-token cost based on dimensions.
// Falls back to a conservative default when dimensions are missing.
func estimateImageTokens(width, height float64) int {
	if width <= 0 || height <= 0 {
		return 1000
	}
	tokens := int(width * height / 750)
	if tokens < 85 {
		return 85
	}
	if tokens > 1590 {
		return 1590
	}
	return tokens
}
