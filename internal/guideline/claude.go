package guideline

import (
	"encoding/json"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// injectClaude injects content into the Anthropic Messages API `system`
// field. The Messages schema accepts two shapes:
//
//   1. string  -> "system": "you are ..."
//   2. array   -> "system": [{"type":"text","text":"...","cache_control":...}, ...]
//
// The injector preserves whichever shape the caller used and leaves any
// existing `cache_control` annotations intact.
func injectClaude(body []byte, content string, position string) []byte {
	system := gjson.GetBytes(body, "system")
	switch system.Type {
	case gjson.Null:
		// No system prompt provided yet; insert a plain string.
		patched, err := sjson.SetBytes(body, "system", content)
		if err != nil {
			return body
		}
		return patched
	case gjson.String:
		merged := MergeText(system.String(), content, position)
		patched, err := sjson.SetBytes(body, "system", merged)
		if err != nil {
			return body
		}
		return patched
	case gjson.JSON:
		// Could be array of system blocks or an object; only arrays are valid
		// per Anthropic spec but we keep the path defensive.
		if !system.IsArray() {
			return body
		}
		return injectClaudeSystemArray(body, system, content, position)
	default:
		return body
	}
}

func injectClaudeSystemArray(body []byte, system gjson.Result, content string, position string) []byte {
	block := map[string]string{"type": "text", "text": content}
	encoded, err := json.Marshal(block)
	if err != nil {
		return body
	}
	switch position {
	case PositionReplace:
		patched, err := sjson.SetRawBytes(body, "system", []byte("["+string(encoded)+"]"))
		if err != nil {
			return body
		}
		return patched
	case PositionAppend:
		// Append a new text block at the end of the system array.
		patched, err := sjson.SetRawBytes(body, "system.-1", encoded)
		if err != nil {
			return body
		}
		return patched
	default: // PositionPrepend
		// Prepend by rebuilding the array with the new block at index 0.
		existing := system.Raw
		// existing looks like "[ ... ]" — strip outer brackets and splice.
		if len(existing) < 2 {
			patched, err := sjson.SetRawBytes(body, "system", []byte("["+string(encoded)+"]"))
			if err != nil {
				return body
			}
			return patched
		}
		inner := existing[1 : len(existing)-1]
		var rebuilt []byte
		rebuilt = append(rebuilt, '[')
		rebuilt = append(rebuilt, encoded...)
		// Only add comma + existing content when the existing array isn't empty.
		trimmedInner := trimASCIIWhitespace(inner)
		if len(trimmedInner) > 0 {
			rebuilt = append(rebuilt, ',')
			rebuilt = append(rebuilt, inner...)
		}
		rebuilt = append(rebuilt, ']')
		patched, err := sjson.SetRawBytes(body, "system", rebuilt)
		if err != nil {
			return body
		}
		return patched
	}
}

func trimASCIIWhitespace(s string) string {
	start := 0
	end := len(s)
	for start < end {
		c := s[start]
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			break
		}
		start++
	}
	for end > start {
		c := s[end-1]
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			break
		}
		end--
	}
	return s[start:end]
}
