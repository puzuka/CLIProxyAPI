// Package registry provides Copilot model conversion utilities.
// This file converts dynamic Copilot /models entries into the internal
// ModelInfo format, decoupled from the copilot auth package wire types.
package registry

import (
	"strings"
	"time"
)

// CopilotAPIModel is a registry-local copy of a Copilot /models entry.
type CopilotAPIModel struct {
	ID                 string
	Name               string
	Type               string // capability type, e.g. "chat" or "embeddings"
	SupportedEndpoints []string
	ContextWindow      int
	MaxOutput          int
	Vision             bool
	ModelPickerEnabled *bool
	PolicyState        string
}

// ConvertCopilotAPIModels converts live Copilot API models to ModelInfo.
// Account-policy-disabled models are skipped. Picker visibility is not treated
// as access control because Copilot Student can hide auto-selected models from
// the picker while the model remains callable through the Responses endpoint.
func ConvertCopilotAPIModels(models []*CopilotAPIModel) []*ModelInfo {
	if len(models) == 0 {
		return nil
	}
	now := time.Now().Unix()
	out := make([]*ModelInfo, 0, len(models))
	seen := make(map[string]struct{}, len(models))
	for _, m := range models {
		if m == nil || strings.TrimSpace(m.ID) == "" {
			continue
		}
		if !isUsableCopilotAPIModel(m) {
			continue
		}
		if _, ok := seen[m.ID]; ok {
			continue
		}
		seen[m.ID] = struct{}{}

		input := []string{"TEXT"}
		if m.Vision {
			input = []string{"TEXT", "IMAGE"}
		}
		name := m.Name
		if name == "" {
			name = m.ID
		}
		out = append(out, &ModelInfo{
			ID:                        m.ID,
			Object:                    "model",
			Created:                   now,
			OwnedBy:                   "github-copilot",
			Type:                      "copilot",
			DisplayName:               "Copilot " + name,
			ContextLength:             m.ContextWindow,
			MaxCompletionTokens:       m.MaxOutput,
			SupportedInputModalities:  input,
			SupportedOutputModalities: []string{"TEXT"},
			SupportedEndpoints:        normalizeCopilotSupportedEndpoints(m.SupportedEndpoints),
		})
	}
	return out
}

func isUsableCopilotAPIModel(m *CopilotAPIModel) bool {
	if m == nil {
		return false
	}
	if modelType := strings.TrimSpace(strings.ToLower(m.Type)); modelType != "" && modelType != "chat" {
		return false
	}
	if len(m.SupportedEndpoints) > 0 && !CopilotSupportsChatCompletions(m.SupportedEndpoints) && !CopilotSupportsResponses(m.SupportedEndpoints) {
		return false
	}
	if state := strings.TrimSpace(strings.ToLower(m.PolicyState)); state != "" && state != "enabled" {
		return false
	}
	return true
}

func CopilotSupportsChatCompletions(endpoints []string) bool {
	for _, endpoint := range endpoints {
		if normalizeCopilotEndpoint(endpoint) == "/chat/completions" {
			return true
		}
	}
	return false
}

func CopilotSupportsResponses(endpoints []string) bool {
	for _, endpoint := range endpoints {
		switch normalizeCopilotEndpoint(endpoint) {
		case "/responses", "ws:/responses":
			return true
		}
	}
	return false
}

func normalizeCopilotSupportedEndpoints(endpoints []string) []string {
	if len(endpoints) == 0 {
		return nil
	}
	out := make([]string, 0, len(endpoints))
	seen := make(map[string]struct{}, len(endpoints))
	for _, endpoint := range endpoints {
		normalized := normalizeCopilotEndpoint(endpoint)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}

func normalizeCopilotEndpoint(endpoint string) string {
	normalized := strings.TrimSpace(strings.ToLower(endpoint))
	if normalized == "" {
		return ""
	}
	if strings.HasPrefix(normalized, "ws:") {
		path := strings.TrimPrefix(normalized, "ws:")
		if path != "" && !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		return "ws:" + path
	}
	if !strings.HasPrefix(normalized, "/") {
		normalized = "/" + normalized
	}
	return normalized
}

// MergeCopilotDynamicWithStaticMetadata enriches live Copilot models with local
// metadata but does not append static fallback models that are absent from the
// live account catalog. When live discovery succeeds, the live catalog is the
// source of truth for what this auth can select.
func MergeCopilotDynamicWithStaticMetadata(dynamicModels, staticModels []*ModelInfo) []*ModelInfo {
	if len(dynamicModels) == 0 {
		return nil
	}

	staticMap := make(map[string]*ModelInfo, len(staticModels))
	for _, sm := range staticModels {
		if sm != nil && sm.ID != "" {
			staticMap[sm.ID] = sm
		}
	}

	seenIDs := make(map[string]struct{}, len(dynamicModels))
	result := make([]*ModelInfo, 0, len(dynamicModels))
	for _, dm := range dynamicModels {
		if dm == nil || dm.ID == "" {
			continue
		}
		if _, seen := seenIDs[dm.ID]; seen {
			continue
		}
		seenIDs[dm.ID] = struct{}{}

		if sm, exists := staticMap[dm.ID]; exists {
			merged := cloneModelInfo(sm)
			if len(dm.SupportedEndpoints) > 0 {
				merged.SupportedEndpoints = append([]string(nil), dm.SupportedEndpoints...)
			}
			if merged.ContextLength == 0 {
				merged.ContextLength = dm.ContextLength
			}
			if merged.MaxCompletionTokens == 0 {
				merged.MaxCompletionTokens = dm.MaxCompletionTokens
			}
			result = append(result, merged)
			continue
		}
		result = append(result, dm)
	}
	return result
}
