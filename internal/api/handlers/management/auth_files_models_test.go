package management

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func TestPatchAuthFileModels_PersistsManualModelsOverride(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	gin.SetMode(gin.TestMode)

	store := &memoryAuthStore{}
	manager := coreauth.NewManager(store, nil, nil)
	record := &coreauth.Auth{
		ID:       "manual.json",
		FileName: "manual.json",
		Provider: "copilot",
		Attributes: map[string]string{
			"path": "/tmp/manual.json",
		},
		Metadata: map[string]any{
			"type": "copilot",
		},
	}
	if _, errRegister := manager.Register(context.Background(), record); errRegister != nil {
		t.Fatalf("failed to register auth record: %v", errRegister)
	}

	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: t.TempDir()}, manager)

	body := `{"name":"manual.json","models":[{"id":" gpt-manual ","display_name":"Manual GPT","type":"copilot","owned_by":"operator"},{"id":"GPT-MANUAL","display_name":"duplicate"}]}`
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPatch, "/v0/management/auth-files/models", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req
	h.PatchAuthFileModels(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	updated, ok := manager.GetByID("manual.json")
	if !ok || updated == nil {
		t.Fatal("expected auth record to exist after patch")
	}

	rawModels, ok := updated.Metadata["models"].([]map[string]any)
	if !ok {
		t.Fatalf("metadata.models = %T, want []map[string]any", updated.Metadata["models"])
	}
	if len(rawModels) != 1 {
		t.Fatalf("metadata models len = %d, want 1", len(rawModels))
	}
	if got := rawModels[0]["id"]; got != "gpt-manual" {
		t.Fatalf("stored model id = %#v, want gpt-manual", got)
	}
	if got := rawModels[0]["display_name"]; got != "Manual GPT" {
		t.Fatalf("stored display_name = %#v, want Manual GPT", got)
	}
	if got := rawModels[0]["owned_by"]; got != "operator" {
		t.Fatalf("stored owned_by = %#v, want operator", got)
	}

	getRec := httptest.NewRecorder()
	getCtx, _ := gin.CreateTestContext(getRec)
	getReq := httptest.NewRequest(http.MethodGet, "/v0/management/auth-files/models?name=manual.json", nil)
	getCtx.Request = getReq
	h.GetAuthFileModels(getCtx)

	if getRec.Code != http.StatusOK {
		t.Fatalf("expected GET status %d, got %d with body %s", http.StatusOK, getRec.Code, getRec.Body.String())
	}

	var response struct {
		Manual bool `json:"manual"`
		Models []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
			Type        string `json:"type"`
			OwnedBy     string `json:"owned_by"`
		} `json:"models"`
	}
	if err := json.Unmarshal(getRec.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if !response.Manual {
		t.Fatal("expected manual=true")
	}
	if len(response.Models) != 1 || response.Models[0].ID != "gpt-manual" {
		t.Fatalf("response models = %#v, want one gpt-manual", response.Models)
	}
	if response.Models[0].DisplayName != "Manual GPT" {
		t.Fatalf("response display_name = %q, want Manual GPT", response.Models[0].DisplayName)
	}
}
