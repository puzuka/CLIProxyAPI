package guideline

import (
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// injectGemini injects content into the systemInstruction field of a
// Gemini generateContent payload.
//
// Gemini schema:
//
//   {
//     "systemInstruction": { "parts": [ { "text": "..." }, ... ], "role": "system" },
//     "contents": [ ... ]
//   }
//
// The injector merges into the first text part when one exists, otherwise
// inserts a fresh systemInstruction object. Non-text parts (e.g. inline_data
// images) are preserved.
func injectGemini(body []byte, content string, position string) []byte {
	si := gjson.GetBytes(body, "systemInstruction")
	if si.Type == gjson.Null {
		// Some clients use the snake_case alias; check that too.
		si = gjson.GetBytes(body, "system_instruction")
	}
	if si.Type == gjson.Null {
		// Insert a fresh systemInstruction.
		patched, err := sjson.SetRawBytes(body, "systemInstruction", []byte(`{"parts":[{"text":`+jsonStringEncode(content)+`}]}`))
		if err != nil {
			return body
		}
		return patched
	}

	// Determine which top-level key to mutate (camelCase vs snake_case).
	key := "systemInstruction"
	if !gjson.GetBytes(body, "systemInstruction").Exists() && gjson.GetBytes(body, "system_instruction").Exists() {
		key = "system_instruction"
	}

	parts := si.Get("parts")
	if !parts.IsArray() || len(parts.Array()) == 0 {
		// systemInstruction exists but has no parts; install one.
		patched, err := sjson.SetRawBytes(body, key+".parts", []byte(`[{"text":`+jsonStringEncode(content)+`}]`))
		if err != nil {
			return body
		}
		return patched
	}

	// Find first part that has a text field; merge there.
	for i, part := range parts.Array() {
		t := part.Get("text")
		if t.Type == gjson.String {
			merged := MergeText(t.String(), content, position)
			path := key + ".parts." + itoa(i) + ".text"
			patched, err := sjson.SetBytes(body, path, merged)
			if err != nil {
				return body
			}
			return patched
		}
	}

	// No text parts present (only inline_data, function_call etc.). Append a
	// new text part rather than disturbing existing parts.
	switch position {
	case PositionPrepend:
		// Rebuild parts array with the new text part at index 0.
		existing := parts.Raw
		if len(existing) < 2 {
			patched, err := sjson.SetRawBytes(body, key+".parts", []byte(`[{"text":`+jsonStringEncode(content)+`}]`))
			if err != nil {
				return body
			}
			return patched
		}
		inner := existing[1 : len(existing)-1]
		newPart := []byte(`{"text":` + jsonStringEncode(content) + `}`)
		var rebuilt []byte
		rebuilt = append(rebuilt, '[')
		rebuilt = append(rebuilt, newPart...)
		if len(trimASCIIWhitespace(inner)) > 0 {
			rebuilt = append(rebuilt, ',')
			rebuilt = append(rebuilt, inner...)
		}
		rebuilt = append(rebuilt, ']')
		patched, err := sjson.SetRawBytes(body, key+".parts", rebuilt)
		if err != nil {
			return body
		}
		return patched
	default:
		patched, err := sjson.SetRawBytes(body, key+".parts.-1", []byte(`{"text":`+jsonStringEncode(content)+`}`))
		if err != nil {
			return body
		}
		return patched
	}
}

// jsonStringEncode returns a JSON-encoded string literal (including
// surrounding quotes) for s. We avoid pulling in encoding/json for this
// because we need raw bytes that compose into a larger raw object literal.
func jsonStringEncode(s string) string {
	const hex = "0123456789abcdef"
	var b []byte
	b = append(b, '"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			b = append(b, '\\', '"')
		case '\\':
			b = append(b, '\\', '\\')
		case '\n':
			b = append(b, '\\', 'n')
		case '\r':
			b = append(b, '\\', 'r')
		case '\t':
			b = append(b, '\\', 't')
		case '\b':
			b = append(b, '\\', 'b')
		case '\f':
			b = append(b, '\\', 'f')
		default:
			if c < 0x20 {
				b = append(b, '\\', 'u', '0', '0', hex[c>>4], hex[c&0xF])
			} else {
				b = append(b, c)
			}
		}
	}
	b = append(b, '"')
	return string(b)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
