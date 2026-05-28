// Package common — Kiro tool input_schema sanitizer.
//
// Kiro / AWS CodeWhisperer's request validator (issue kirodotdev/Kiro#3431)
// rejects any tool input_schema that contains nested objects, oneOf/anyOf/
// allOf, $ref, $defs, complex unions, etc. with a generic "Improperly formed
// request" error. Modern coding clients (Codex Desktop, Antigravity, Cline,
// etc.) ship tool definitions whose JSON Schemas trigger this — for example
// the Codex Desktop `exec_command`, `automation_update`, and
// `request_user_input` tools all carry deeply nested `properties` objects
// with arrays of objects holding their own typed fields and required arrays.
//
// Rather than dropping the whole tools list (which strips agent capabilities)
// we aggressively flatten: keep top-level field names, types, descriptions,
// and required lists; replace anything Kiro can't serialise with simple
// scalar placeholders or remove it. The semantic loss is acceptable because
// the tool name + description + a sketch of its parameters is enough for
// the model to decide whether to invoke the tool — the actual call goes
// back through the proxy which then sees the full schema-bound payload.
//
// Constants are tunable; defaults were chosen empirically from observed
// Kiro 400 responses on Codex Desktop traffic.
package common

import (
	"encoding/json"
)

// kiroMaxSchemaJSONBytes caps the marshaled length of a single tool's input
// schema. Anything bigger gets replaced with the empty object schema.
const kiroMaxSchemaJSONBytes = 32 * 1024

// kiroMaxSchemaDepth is the maximum nesting depth retained in a property tree
// before scalar-fallback kicks in. The Codex Desktop request bodies that
// triggered the original "Improperly formed request" had `properties` 3-4
// levels deep, so 2 is intentionally conservative.
const kiroMaxSchemaDepth = 2

// SanitizeKiroToolSchema returns a copy of the supplied JSON Schema fragment
// that Kiro CodeWhisperer's tool validator will accept. The returned value
// is always a JSON-marshallable map[string]interface{} or nil.
//
// Nil input maps to {type:"object", properties:{}} — Kiro requires a schema
// even for parameter-less tools.
func SanitizeKiroToolSchema(parameters interface{}) interface{} {
	clean := sanitizeNode(parameters, 0)
	cleanMap, ok := clean.(map[string]interface{})
	if !ok || cleanMap == nil {
		cleanMap = map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}
	}

	// If the schema is still too large after flattening (very rare), fall
	// back to the empty-object schema so Kiro doesn't blow up parsing.
	if encoded, err := json.Marshal(cleanMap); err == nil && len(encoded) > kiroMaxSchemaJSONBytes {
		return map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}
	}

	// Kiro requires `type:"object"` at the top level. If the caller supplied
	// a primitive (rare, but possible), upgrade it.
	if t, _ := cleanMap["type"].(string); t == "" {
		cleanMap["type"] = "object"
	}
	if _, ok := cleanMap["properties"]; !ok && cleanMap["type"] == "object" {
		cleanMap["properties"] = map[string]interface{}{}
	}
	return cleanMap
}

// sanitizeNode recursively cleans a schema node, replacing any construct that
// Kiro is known to choke on with a flat equivalent.
func sanitizeNode(node interface{}, depth int) interface{} {
	switch n := node.(type) {
	case map[string]interface{}:
		return sanitizeObject(n, depth)
	case []interface{}:
		return sanitizeArray(n, depth)
	default:
		// Primitives (string, number, bool, nil) round-trip unchanged.
		return n
	}
}

// sanitizeObject strips complex JSON Schema constructs and recurses.
func sanitizeObject(obj map[string]interface{}, depth int) map[string]interface{} {
	out := make(map[string]interface{}, len(obj))

	// Constructs that Kiro's serialiser does not accept. Drop them outright.
	// "$schema" is a no-op metadata key but some validators still reject it.
	skipKeys := map[string]struct{}{
		"$ref":        {},
		"$defs":       {},
		"$schema":     {},
		"$id":         {},
		"$comment":    {},
		"definitions": {},
		"oneOf":       {},
		"anyOf":       {},
		"allOf":       {},
		"not":         {},
		"if":          {},
		"then":        {},
		"else":        {},
		// Keyword pattern/format-style constraints that Kiro treats as
		// unknown extensions on object types.
		"patternProperties":    {},
		"additionalProperties": {},
		"unevaluatedProperties": {},
		"propertyNames":        {},
		"dependencies":         {},
		"dependentRequired":    {},
		"dependentSchemas":     {},
		"contains":             {},
		"contentEncoding":      {},
		"contentMediaType":     {},
		"contentSchema":        {},
	}

	for k, v := range obj {
		if _, drop := skipKeys[k]; drop {
			continue
		}

		switch k {
		case "properties":
			// Recurse into each property; clamp depth to flatten deep trees.
			propsMap, ok := v.(map[string]interface{})
			if !ok {
				continue
			}
			cleanProps := make(map[string]interface{}, len(propsMap))
			for propName, propSchema := range propsMap {
				cleanProps[propName] = sanitizeProperty(propSchema, depth+1)
			}
			out["properties"] = cleanProps

		case "items":
			// Array items: recurse the same way as a property, depth+1.
			out["items"] = sanitizeProperty(v, depth+1)

		case "enum":
			// Keep enum arrays only when they carry primitive values; strip
			// object/array enums (some clients enum'd whole structs).
			if arr, ok := v.([]interface{}); ok {
				clean := make([]interface{}, 0, len(arr))
				for _, e := range arr {
					switch e.(type) {
					case string, float64, int, int64, bool, nil:
						clean = append(clean, e)
					}
				}
				if len(clean) > 0 {
					out["enum"] = clean
				}
			}

		default:
			// All other keys (type, description, required, default, etc.)
			// are passed through after recursive cleaning.
			out[k] = sanitizeNode(v, depth)
		}
	}
	return out
}

// sanitizeArray recurses into each array element.
func sanitizeArray(arr []interface{}, depth int) []interface{} {
	out := make([]interface{}, len(arr))
	for i, v := range arr {
		out[i] = sanitizeNode(v, depth)
	}
	return out
}

// sanitizeProperty handles a single named property's schema. When the depth
// budget is exceeded we replace the subtree with a string scalar so the model
// still sees the field name and description but Kiro's parser sees a flat type.
func sanitizeProperty(schema interface{}, depth int) interface{} {
	if depth > kiroMaxSchemaDepth {
		return collapseToScalar(schema)
	}
	cleaned := sanitizeNode(schema, depth)
	cleanMap, ok := cleaned.(map[string]interface{})
	if !ok {
		return cleaned
	}

	// If this property is still an object/array beyond the depth budget,
	// flatten it. We allow exactly one level of array-of-string or
	// object-with-primitive-properties; otherwise scalarise.
	if t, _ := cleanMap["type"].(string); t == "object" && depth >= kiroMaxSchemaDepth {
		return collapseToScalar(cleanMap)
	}
	if t, _ := cleanMap["type"].(string); t == "array" && depth >= kiroMaxSchemaDepth {
		// Replace nested array items with string scalars to be safe.
		cleanMap["items"] = map[string]interface{}{"type": "string"}
	}
	return cleanMap
}

// collapseToScalar replaces a nested schema with a string scalar that keeps
// the original description (if any) so the model still has context.
func collapseToScalar(node interface{}) map[string]interface{} {
	out := map[string]interface{}{"type": "string"}
	if m, ok := node.(map[string]interface{}); ok {
		if d, _ := m["description"].(string); d != "" {
			out["description"] = d
		}
	}
	return out
}
