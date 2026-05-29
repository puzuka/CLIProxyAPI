package openai

import "testing"

func TestCodexClientModelsResponsePrefixesDisplayNameAndSortsAlphabetically(t *testing.T) {
	resp := CodexClientModelsResponse([]map[string]any{
		{
			"id":           "custom-z-model",
			"owned_by":     "zeta",
			"display_name": "Z Model",
			"description":  "Z model description",
		},
		{
			"id":           "gpt-5.5",
			"owned_by":     "openai",
			"display_name": "GPT 5.5",
		},
		{
			"id":           "custom-a-model",
			"owned_by":     "alpha",
			"display_name": "A Model",
			"description":  "A model description",
		},
	})

	models, ok := resp["models"].([]map[string]any)
	if !ok {
		t.Fatalf("models has type %T, want []map[string]any", resp["models"])
	}
	if len(models) != 3 {
		t.Fatalf("models length = %d, want 3", len(models))
	}

	wantSlugs := []string{"custom-a-model", "gpt-5.5", "custom-z-model"}
	wantDisplayNames := []string{"Alpha / A Model", "OpenAI / GPT-5.5", "Zeta / Z Model"}
	for i := range wantSlugs {
		if got, _ := models[i]["slug"].(string); got != wantSlugs[i] {
			t.Fatalf("models[%d].slug = %q, want %q", i, got, wantSlugs[i])
		}
		if got, _ := models[i]["display_name"].(string); got != wantDisplayNames[i] {
			t.Fatalf("models[%d].display_name = %q, want %q", i, got, wantDisplayNames[i])
		}
		if got, _ := models[i]["priority"].(int); got != i {
			t.Fatalf("models[%d].priority = %d, want %d", i, got, i)
		}
	}
}
