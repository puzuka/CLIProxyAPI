package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSaveConfigPreserveComments_PrunesClearedAPIKeyMetadataFields verifies that
// fields cleared via the management API (e.g. allowed-providers, scopes, name)
// are removed from the persisted YAML. Without the explicit prune pass these
// keys would linger forever because mergeMappingPreserve only updates existing
// keys and omitempty drops cleared scalars/slices from the generated YAML.
func TestSaveConfigPreserveComments_PrunesClearedAPIKeyMetadataFields(t *testing.T) {
	t.Parallel()

	key := "sk-prune-test"
	id := APIKeyID(key)

	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yaml")

	initialYAML := []byte(`api-keys:
  - sk-prune-test
api-key-metadata:
  ` + id + `:
    name: CI runner
    scopes:
      - chat
      - responses
    allowed-providers:
      - opencode
      - kiro
    allowed-models:
      - deepseek*
`)
	if err := os.WriteFile(configFile, initialYAML, 0o600); err != nil {
		t.Fatalf("write initial config: %v", err)
	}

	cfg, err := LoadConfigOptional(configFile, false)
	if err != nil {
		t.Fatalf("LoadConfigOptional: %v", err)
	}

	meta := cfg.APIKeyMetadata[id]
	if len(meta.AllowedProviders) != 2 || meta.Name != "CI runner" || len(meta.Scopes) != 2 {
		t.Fatalf("unexpected initial metadata: %#v", meta)
	}

	// Simulate UI clearing allowed-providers, scopes, and name. allowed-models
	// is kept so we can assert it is still present after save.
	meta.AllowedProviders = nil
	meta.Scopes = nil
	meta.Name = ""
	cfg.APIKeyMetadata[id] = meta

	if err := SaveConfigPreserveComments(configFile, cfg); err != nil {
		t.Fatalf("SaveConfigPreserveComments: %v", err)
	}

	saved, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	got := string(saved)

	if strings.Contains(got, "allowed-providers") {
		t.Errorf("allowed-providers should be pruned from YAML, got:\n%s", got)
	}
	if strings.Contains(got, "scopes") {
		t.Errorf("scopes should be pruned from YAML, got:\n%s", got)
	}
	if strings.Contains(got, "name: CI runner") {
		t.Errorf("name field should be pruned, got:\n%s", got)
	}
	if !strings.Contains(got, "allowed-models") || !strings.Contains(got, "deepseek*") {
		t.Errorf("allowed-models should still be present, got:\n%s", got)
	}

	// Round-trip: re-load and verify the in-memory state matches what we wrote.
	roundTrip, err := LoadConfigOptional(configFile, false)
	if err != nil {
		t.Fatalf("reload saved config: %v", err)
	}
	got2 := roundTrip.APIKeyMetadata[id]
	if len(got2.AllowedProviders) != 0 {
		t.Errorf("allowed-providers should round-trip empty, got %#v", got2.AllowedProviders)
	}
	if len(got2.Scopes) != 0 {
		t.Errorf("scopes should round-trip empty, got %#v", got2.Scopes)
	}
	if got2.Name != "" {
		t.Errorf("name should round-trip empty, got %q", got2.Name)
	}
	if len(got2.AllowedModels) != 1 || got2.AllowedModels[0] != "deepseek*" {
		t.Errorf("allowed-models should round-trip intact, got %#v", got2.AllowedModels)
	}
}

// TestSaveConfigPreserveComments_PrunesRemovedAPIKeyMetadataEntries verifies
// that an entire metadata entry is removed from YAML when its api-key is
// dropped from cfg.APIKeyMetadata (e.g. via SanitizeAPIKeyMetadata after a
// key delete/rotate).
func TestSaveConfigPreserveComments_PrunesRemovedAPIKeyMetadataEntries(t *testing.T) {
	t.Parallel()

	keptKey := "sk-kept"
	staleKey := "sk-stale"
	keptID := APIKeyID(keptKey)
	staleID := APIKeyID(staleKey)

	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yaml")

	initialYAML := []byte(`api-keys:
  - sk-kept
  - sk-stale
api-key-metadata:
  ` + keptID + `:
    owner: kept-owner
  ` + staleID + `:
    owner: stale-owner
`)
	if err := os.WriteFile(configFile, initialYAML, 0o600); err != nil {
		t.Fatalf("write initial config: %v", err)
	}

	cfg, err := LoadConfigOptional(configFile, false)
	if err != nil {
		t.Fatalf("LoadConfigOptional: %v", err)
	}

	// Simulate deleting the stale key (and its metadata) via the management API.
	cfg.APIKeys = []string{keptKey}
	delete(cfg.APIKeyMetadata, staleID)
	cfg.SanitizeAPIKeyMetadata()

	if err := SaveConfigPreserveComments(configFile, cfg); err != nil {
		t.Fatalf("SaveConfigPreserveComments: %v", err)
	}

	saved, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	got := string(saved)

	if strings.Contains(got, staleID) {
		t.Errorf("stale entry %s should be pruned, got:\n%s", staleID, got)
	}
	if strings.Contains(got, "stale-owner") {
		t.Errorf("stale owner value should be pruned, got:\n%s", got)
	}
	if !strings.Contains(got, keptID) {
		t.Errorf("kept entry %s should remain, got:\n%s", keptID, got)
	}
	if !strings.Contains(got, "kept-owner") {
		t.Errorf("kept owner should remain, got:\n%s", got)
	}
}

// TestSaveConfigPreserveComments_DropsAPIKeyMetadataSectionWhenEmpty verifies
// that the api-key-metadata top-level section is removed when the in-memory
// config no longer has any entries (e.g. all api-keys were deleted).
func TestSaveConfigPreserveComments_DropsAPIKeyMetadataSectionWhenEmpty(t *testing.T) {
	t.Parallel()

	key := "sk-only"
	id := APIKeyID(key)

	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yaml")

	initialYAML := []byte(`api-keys:
  - sk-only
api-key-metadata:
  ` + id + `:
    owner: solo
`)
	if err := os.WriteFile(configFile, initialYAML, 0o600); err != nil {
		t.Fatalf("write initial config: %v", err)
	}

	cfg, err := LoadConfigOptional(configFile, false)
	if err != nil {
		t.Fatalf("LoadConfigOptional: %v", err)
	}

	cfg.APIKeys = nil
	cfg.APIKeyMetadata = nil

	if err := SaveConfigPreserveComments(configFile, cfg); err != nil {
		t.Fatalf("SaveConfigPreserveComments: %v", err)
	}

	saved, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	got := string(saved)

	if strings.Contains(got, "api-key-metadata") {
		t.Errorf("api-key-metadata section should be dropped when empty, got:\n%s", got)
	}
	if strings.Contains(got, id) || strings.Contains(got, "solo") {
		t.Errorf("metadata content should be gone, got:\n%s", got)
	}
}
