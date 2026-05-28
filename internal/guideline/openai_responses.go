package guideline

import (
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// injectOpenAIResponses injects content into the top-level `instructions`
// field of an OpenAI Responses API payload (POST /v1/responses).
//
// The Responses API treats `instructions` as the system prompt. If
// `instructions` is missing, it is created. If present (string), the
// content is merged according to position. Non-string instructions
// (object / array) are not part of the documented schema; we leave them
// alone to avoid corrupting client payloads.
func injectOpenAIResponses(body []byte, content string, position string) []byte {
	field := gjson.GetBytes(body, "instructions")
	switch field.Type {
	case gjson.Null:
		patched, err := sjson.SetBytes(body, "instructions", content)
		if err != nil {
			return body
		}
		return patched
	case gjson.String:
		merged := MergeText(field.String(), content, position)
		patched, err := sjson.SetBytes(body, "instructions", merged)
		if err != nil {
			return body
		}
		return patched
	default:
		return body
	}
}
