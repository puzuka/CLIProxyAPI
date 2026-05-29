package registry

import (
	"reflect"
	"testing"
)

func TestConvertCopilotAPIModelsFiltersUnusableEntries(t *testing.T) {
	enabled := true
	disabled := false

	models := []*CopilotAPIModel{
		{ID: "usable", Name: "Usable", Type: "chat", ModelPickerEnabled: &enabled, PolicyState: "enabled"},
		{ID: "response-only", Name: "Responses Only", Type: "chat", SupportedEndpoints: []string{"/responses", "ws:/responses"}, ModelPickerEnabled: &enabled, PolicyState: "enabled"},
		{ID: "chat-endpoint", Name: "Chat Endpoint", Type: "chat", SupportedEndpoints: []string{"/chat/completions", "/responses"}, ModelPickerEnabled: &enabled, PolicyState: "enabled"},
		{ID: "policy-disabled", Name: "Disabled", Type: "chat", ModelPickerEnabled: &enabled, PolicyState: "disabled"},
		{ID: "picker-hidden", Name: "Hidden", Type: "chat", ModelPickerEnabled: &disabled, PolicyState: "enabled"},
		{ID: "embedding", Name: "Embedding", Type: "embeddings", ModelPickerEnabled: &enabled, PolicyState: "enabled"},
		{ID: "usable", Name: "Duplicate", Type: "chat", ModelPickerEnabled: &enabled, PolicyState: "enabled"},
	}

	got := ConvertCopilotAPIModels(models)
	if len(got) != 2 {
		t.Fatalf("converted %d models, want 2: %#v", len(got), got)
	}
	ids := make([]string, 0, len(got))
	for _, model := range got {
		ids = append(ids, model.ID)
	}
	wantIDs := []string{"usable", "chat-endpoint"}
	if !reflect.DeepEqual(ids, wantIDs) {
		t.Fatalf("converted IDs = %#v, want %#v", ids, wantIDs)
	}
}

func TestMergeCopilotDynamicWithStaticMetadataDoesNotAppendFallbacks(t *testing.T) {
	dynamic := []*ModelInfo{
		{ID: "live-only", DisplayName: "Live Only"},
		{ID: "static-overlap", DisplayName: "Live Overlap"},
	}
	static := []*ModelInfo{
		{ID: "static-overlap", DisplayName: "Static Overlap"},
		{ID: "static-only", DisplayName: "Static Only"},
	}

	got := MergeCopilotDynamicWithStaticMetadata(dynamic, static)
	ids := make([]string, 0, len(got))
	displayByID := make(map[string]string, len(got))
	for _, model := range got {
		ids = append(ids, model.ID)
		displayByID[model.ID] = model.DisplayName
	}

	wantIDs := []string{"live-only", "static-overlap"}
	if !reflect.DeepEqual(ids, wantIDs) {
		t.Fatalf("merged IDs = %#v, want %#v", ids, wantIDs)
	}
	if displayByID["static-overlap"] != "Static Overlap" {
		t.Fatalf("static overlap display = %q, want Static Overlap", displayByID["static-overlap"])
	}
}
