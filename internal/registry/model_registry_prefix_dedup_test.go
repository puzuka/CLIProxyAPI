package registry

import "testing"

func modelIDs(models []map[string]any) map[string]bool {
	out := make(map[string]bool, len(models))
	for _, m := range models {
		if id, ok := m["id"].(string); ok {
			out[id] = true
		}
	}
	return out
}

// When applyModelPrefixes emits a bare convenience twin alongside its prefixed
// counterpart, the listing must hide the bare twin and keep only the prefixed
// ID. Routing still works because the bare registration is left intact.
func TestBareTwinSuppressedWhenPrefixedPresent(t *testing.T) {
	r := newTestModelRegistry()
	r.RegisterClient("ocg-auth", "ocg", []*ModelInfo{
		{ID: "glm-5.1", OwnedBy: "ocg", AutoPrefixAliasFor: "ocg/glm-5.1"},
		{ID: "ocg/glm-5.1", OwnedBy: "ocg"},
	})

	got := modelIDs(r.GetAvailableModels("openai"))
	if got["glm-5.1"] {
		t.Fatalf("expected bare twin glm-5.1 to be hidden, got %v", got)
	}
	if !got["ocg/glm-5.1"] {
		t.Fatalf("expected prefixed ocg/glm-5.1 to be listed, got %v", got)
	}
	if len(got) != 1 {
		t.Fatalf("expected exactly one listed model, got %v", got)
	}

	// Routing must still resolve the bare ID even though it is hidden.
	if count := r.GetModelCount("glm-5.1"); count <= 0 {
		t.Fatalf("expected bare twin glm-5.1 to remain routable, got count %d", count)
	}
}

// A model that is genuinely available without a prefix (default no-prefix) must
// stay visible even when a prefixed variant of the same ID exists from another
// provider.
func TestGenuineUnprefixedModelKeptAlongsidePrefixedVariant(t *testing.T) {
	r := newTestModelRegistry()
	// Genuine, no-prefix registration.
	r.RegisterClient("openai-auth", "openai", []*ModelInfo{
		{ID: "gpt-5.5", OwnedBy: "openai"},
	})
	// A prefixed provider that also surfaces gpt-5.5, producing a bare twin.
	r.RegisterClient("mm-auth", "mm", []*ModelInfo{
		{ID: "gpt-5.5", OwnedBy: "mm", AutoPrefixAliasFor: "mm/gpt-5.5"},
		{ID: "mm/gpt-5.5", OwnedBy: "mm"},
	})

	got := modelIDs(r.GetAvailableModels("openai"))
	if !got["gpt-5.5"] {
		t.Fatalf("expected genuine unprefixed gpt-5.5 to remain listed, got %v", got)
	}
	if !got["mm/gpt-5.5"] {
		t.Fatalf("expected prefixed mm/gpt-5.5 to be listed, got %v", got)
	}
}

// If the prefixed counterpart is not registered, the bare twin must not vanish
// from listings; otherwise the model would disappear entirely.
func TestBareTwinKeptWhenPrefixedMissing(t *testing.T) {
	r := newTestModelRegistry()
	r.RegisterClient("ocg-auth", "ocg", []*ModelInfo{
		{ID: "glm-5.1", OwnedBy: "ocg", AutoPrefixAliasFor: "ocg/glm-5.1"},
	})

	got := modelIDs(r.GetAvailableModels("openai"))
	if !got["glm-5.1"] {
		t.Fatalf("expected bare twin to stay visible when prefixed counterpart absent, got %v", got)
	}
}
