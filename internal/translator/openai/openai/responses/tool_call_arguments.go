package responses

import (
	"encoding/json"
	"io"
	"strings"
	"unicode/utf8"
)

const malformedToolArgumentsPreviewLimit = 240

func normalizeFunctionCallArguments(toolName, arguments string) (string, bool) {
	trimmed := strings.TrimSpace(arguments)
	if trimmed == "" {
		return "{}", true
	}
	if isJSONObject(trimmed) {
		return arguments, true
	}
	return malformedFunctionCallArguments(toolName, arguments), false
}

func isJSONObject(raw string) bool {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()

	var obj map[string]any
	if err := decoder.Decode(&obj); err != nil || obj == nil {
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return false
	}
	return true
}

func malformedFunctionCallArguments(toolName, arguments string) string {
	payload := map[string]any{
		"_cliproxy_error":       "malformed_tool_call_arguments",
		"message":               "Upstream model emitted malformed JSON tool arguments; CLIProxy sanitized the call to prevent parse-invalid history.",
		"raw_arguments_length":  len(arguments),
		"raw_arguments_preview": previewToolArguments(arguments),
		"tool_name":             toolName,
	}
	encoded, _ := json.Marshal(payload)
	return string(encoded)
}

func previewToolArguments(arguments string) string {
	preview := strings.TrimSpace(arguments)
	preview = strings.ReplaceAll(preview, "\r", "\\r")
	preview = strings.ReplaceAll(preview, "\n", "\\n")

	if len(preview) <= malformedToolArgumentsPreviewLimit {
		return preview
	}

	limit := malformedToolArgumentsPreviewLimit
	for limit > 0 && !utf8.ValidString(preview[:limit]) {
		limit--
	}
	return preview[:limit] + "..."
}
