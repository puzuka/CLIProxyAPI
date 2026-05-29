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
// Entries hidden from the Copilot model picker or disabled by account policy
// are skipped because they are not callable choices for this auth.
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
	if len(m.SupportedEndpoints) > 0 && !copilotSupportsChatCompletions(m.SupportedEndpoints) {
		return false
	}
	if m.ModelPickerEnabled != nil && !*m.ModelPickerEnabled {
		return false
	}
	if state := strings.TrimSpace(strings.ToLower(m.PolicyState)); state != "" && state != "enabled" {
		return false
	}
	return true
}

func copilotSupportsChatCompletions(endpoints []string) bool {
	for _, endpoint := range endpoints {
		normalized := strings.TrimSpace(strings.ToLower(endpoint))
		if normalized == "" {
			continue
		}
		if !strings.HasPrefix(normalized, "/") {
			normalized = "/" + normalized
		}
		if normalized == "/chat/completions" {
			return true
		}
	}
	return false
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
			result = append(result, sm)
			continue
		}
		result = append(result, dm)
	}
	return result
}
