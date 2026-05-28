package guideline

import (
	"encoding/json"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// injectOpenAIChat injects content into the system message of an
// OpenAI Chat Completions payload (POST /v1/chat/completions).
//
// Behaviors:
//   - If messages[0].role is "system" or "developer", merge content into its
//     `content` field (string form preserved if string, otherwise prepend a
//     new text block to the structured-content array).
//   - Otherwise, prepend a new system message {role:"system", content:...}.
//
// Messages with content arrays (multi-modal text+image) are handled by
// inserting/appending a leading {type:"text",text:content} block.
func injectOpenAIChat(body []byte, content string, position string) []byte {
	if !gjson.GetBytes(body, "messages").IsArray() {
		// No messages array — bail out rather than corrupt the request.
		return body
	}
	first := gjson.GetBytes(body, "messages.0")
	role := first.Get("role").String()
	if role == "system" || role == "developer" {
		return mergeOpenAIChatSystemMessage(body, first, content, position)
	}
	return prependOpenAIChatSystemMessage(body, content)
}

func mergeOpenAIChatSystemMessage(body []byte, first gjson.Result, content string, position string) []byte {
	c := first.Get("content")
	switch c.Type {
	case gjson.String:
		merged := MergeText(c.String(), content, position)
		patched, err := sjson.SetBytes(body, "messages.0.content", merged)
		if err != nil {
			return body
		}
		return patched
	case gjson.JSON:
		if !c.IsArray() {
			// Unknown structured shape (object) — fall back to prepending a
			// fresh system message rather than risk corrupting the payload.
			return prependOpenAIChatSystemMessage(body, content)
		}
		return mergeOpenAIChatContentArray(body, c, content, position)
	case gjson.Null:
		patched, err := sjson.SetBytes(body, "messages.0.content", content)
		if err != nil {
			return body
		}
		return patched
	default:
		return body
	}
}

func mergeOpenAIChatContentArray(body []byte, contentArr gjson.Result, content string, position string) []byte {
	block := map[string]string{"type": "text", "text": content}
	encoded, err := json.Marshal(block)
	if err != nil {
		return body
	}
	switch position {
	case PositionReplace:
		patched, err := sjson.SetRawBytes(body, "messages.0.content", []byte("["+string(encoded)+"]"))
		if err != nil {
			return body
		}
		return patched
	case PositionAppend:
		patched, err := sjson.SetRawBytes(body, "messages.0.content.-1", encoded)
		if err != nil {
			return body
		}
		return patched
	default:
		existing := contentArr.Raw
		if len(existing) < 2 {
			patched, err := sjson.SetRawBytes(body, "messages.0.content", []byte("["+string(encoded)+"]"))
			if err != nil {
				return body
			}
			return patched
		}
		inner := existing[1 : len(existing)-1]
		var rebuilt []byte
		rebuilt = append(rebuilt, '[')
		rebuilt = append(rebuilt, encoded...)
		if len(trimASCIIWhitespace(inner)) > 0 {
			rebuilt = append(rebuilt, ',')
			rebuilt = append(rebuilt, inner...)
		}
		rebuilt = append(rebuilt, ']')
		patched, err := sjson.SetRawBytes(body, "messages.0.content", rebuilt)
		if err != nil {
			return body
		}
		return patched
	}
}

func prependOpenAIChatSystemMessage(body []byte, content string) []byte {
	msg := map[string]string{"role": "system", "content": content}
	encoded, err := json.Marshal(msg)
	if err != nil {
		return body
	}
	// sjson supports prepend semantics via the special "-1" path on arrays
	// (which appends), but does not have a "0" insert. Rebuild messages
	// array manually so the system message lands at index 0.
	existing := gjson.GetBytes(body, "messages").Raw
	if len(existing) < 2 {
		patched, err := sjson.SetRawBytes(body, "messages", []byte("["+string(encoded)+"]"))
		if err != nil {
			return body
		}
		return patched
	}
	inner := existing[1 : len(existing)-1]
	var rebuilt []byte
	rebuilt = append(rebuilt, '[')
	rebuilt = append(rebuilt, encoded...)
	if len(trimASCIIWhitespace(inner)) > 0 {
		rebuilt = append(rebuilt, ',')
		rebuilt = append(rebuilt, inner...)
	}
	rebuilt = append(rebuilt, ']')
	patched, err := sjson.SetRawBytes(body, "messages", rebuilt)
	if err != nil {
		return body
	}
	return patched
}
