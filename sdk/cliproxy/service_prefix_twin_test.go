package cliproxy

import "testing"

func indexByID(models []*ModelInfo) map[string]*ModelInfo {
	out := make(map[string]*ModelInfo, len(models))
	for _, m := range models {
		if m == nil {
			continue
		}
		out[m.ID] = m
	}
	return out
}

// With force-model-prefix disabled, applyModelPrefixes keeps the bare ID
// routable but tags it as the auto-generated alias of the prefixed entry, while
// the prefixed clone carries no alias marker. This is what lets model listings
// hide the duplicate without dropping routing.
func TestApplyModelPrefixes_MarksBareTwin(t *testing.T) {
	out := applyModelPrefixes([]*ModelInfo{{ID: "glm-5.1", OwnedBy: "ocg"}}, "ocg", false)

	byID := indexByID(out)
	bare, ok := byID["glm-5.1"]
	if !ok {
		t.Fatalf("expected bare twin glm-5.1 to remain present, got %v", byID)
	}
	if bare.AutoPrefixAliasFor != "ocg/glm-5.1" {
		t.Fatalf("expected bare twin tagged with ocg/glm-5.1, got %q", bare.AutoPrefixAliasFor)
	}
	prefixed, ok := byID["ocg/glm-5.1"]
	if !ok {
		t.Fatalf("expected prefixed ocg/glm-5.1 to be present, got %v", byID)
	}
	if prefixed.AutoPrefixAliasFor != "" {
		t.Fatalf("expected prefixed clone to carry no alias marker, got %q", prefixed.AutoPrefixAliasFor)
	}
}

// With force-model-prefix enabled, the bare ID is never emitted (so there is no
// twin to mark) unless the prefix equals the model name.
func TestApplyModelPrefixes_ForceEmitsOnlyPrefixed(t *testing.T) {
	out := applyModelPrefixes([]*ModelInfo{{ID: "glm-5.1", OwnedBy: "ocg"}}, "ocg", true)

	byID := indexByID(out)
	if _, ok := byID["glm-5.1"]; ok {
		t.Fatalf("expected no bare twin under force-model-prefix, got %v", byID)
	}
	prefixed, ok := byID["ocg/glm-5.1"]
	if !ok {
		t.Fatalf("expected prefixed ocg/glm-5.1 to be present, got %v", byID)
	}
	if prefixed.AutoPrefixAliasFor != "" {
		t.Fatalf("expected prefixed clone to carry no alias marker, got %q", prefixed.AutoPrefixAliasFor)
	}
}

// An empty prefix is a no-op: models pass through untouched and unmarked.
func TestApplyModelPrefixes_EmptyPrefixNoOp(t *testing.T) {
	in := []*ModelInfo{{ID: "step-3.7-flash", OwnedBy: "stepfun"}}
	out := applyModelPrefixes(in, "", false)

	if len(out) != 1 {
		t.Fatalf("expected 1 model for empty prefix, got %d", len(out))
	}
	if out[0].ID != "step-3.7-flash" || out[0].AutoPrefixAliasFor != "" {
		t.Fatalf("expected untouched step-3.7-flash with no marker, got %+v", out[0])
	}
}
