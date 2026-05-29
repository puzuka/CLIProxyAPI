package registry

import (
	"strings"
	"time"
)

// ManualModelsFromMetadata parses an auth-file-local models override.
// The second return value is true when the override key is present, including
// an intentionally empty list.
func ManualModelsFromMetadata(metadata map[string]any, defaultOwnedBy, defaultType string) ([]*ModelInfo, bool) {
	if metadata == nil {
		return nil, false
	}
	raw, ok := metadata["models"]
	if !ok {
		raw, ok = metadata["manual_models"]
	}
	if !ok {
		return nil, false
	}
	return manualModelsFromRaw(raw, defaultOwnedBy, defaultType), true
}

func manualModelsFromRaw(raw any, defaultOwnedBy, defaultType string) []*ModelInfo {
	switch value := raw.(type) {
	case []any:
		return manualModelsFromSlice(value, defaultOwnedBy, defaultType)
	case []map[string]any:
		items := make([]any, 0, len(value))
		for _, item := range value {
			items = append(items, item)
		}
		return manualModelsFromSlice(items, defaultOwnedBy, defaultType)
	case []string:
		items := make([]any, 0, len(value))
		for _, item := range value {
			items = append(items, item)
		}
		return manualModelsFromSlice(items, defaultOwnedBy, defaultType)
	case string:
		return manualModelsFromSlice([]any{value}, defaultOwnedBy, defaultType)
	default:
		return nil
	}
}

func manualModelsFromSlice(items []any, defaultOwnedBy, defaultType string) []*ModelInfo {
	now := time.Now().Unix()
	seen := make(map[string]struct{}, len(items))
	out := make([]*ModelInfo, 0, len(items))

	for _, item := range items {
		model := manualModelFromItem(item, defaultOwnedBy, defaultType, now)
		if model == nil {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(model.ID))
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, model)
	}

	return out
}

func manualModelFromItem(item any, defaultOwnedBy, defaultType string, created int64) *ModelInfo {
	switch value := item.(type) {
	case string:
		id := strings.TrimSpace(value)
		if id == "" {
			return nil
		}
		return &ModelInfo{
			ID:          id,
			Object:      "model",
			Created:     created,
			OwnedBy:     strings.TrimSpace(defaultOwnedBy),
			Type:        strings.TrimSpace(defaultType),
			DisplayName: id,
			UserDefined: true,
		}
	case map[string]any:
		id := firstManualModelString(value, "id", "alias", "name", "model")
		if id == "" {
			return nil
		}
		displayName := firstManualModelString(value, "display_name", "displayName", "name")
		if displayName == "" {
			displayName = id
		}
		modelType := firstManualModelString(value, "type")
		if modelType == "" {
			modelType = strings.TrimSpace(defaultType)
		}
		ownedBy := firstManualModelString(value, "owned_by", "ownedBy")
		if ownedBy == "" {
			ownedBy = strings.TrimSpace(defaultOwnedBy)
		}
		return &ModelInfo{
			ID:          id,
			Object:      "model",
			Created:     created,
			OwnedBy:     ownedBy,
			Type:        modelType,
			DisplayName: displayName,
			UserDefined: true,
		}
	default:
		return nil
	}
}

func firstManualModelString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		raw, ok := values[key]
		if !ok || raw == nil {
			continue
		}
		if value := strings.TrimSpace(toManualModelString(raw)); value != "" {
			return value
		}
	}
	return ""
}

func toManualModelString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	default:
		return ""
	}
}
