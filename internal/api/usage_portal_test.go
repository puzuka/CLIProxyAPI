package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/managementasset"
)

func TestUsagePortalServesHTML(t *testing.T) {
	server := newTestServer(t)
	writeTestManagementAsset(t, server, "<!doctype html><title>Usage Portal</title><main>/usage/</main>")

	req := httptest.NewRequest(http.MethodGet, "/usage", nil)
	rr := httptest.NewRecorder()
	server.engine.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if contentType := rr.Header().Get("Content-Type"); contentType != "text/html; charset=utf-8" {
		t.Fatalf("content type = %q", contentType)
	}
	if body := rr.Body.String(); !containsAll(body, "Usage Portal", "/usage/") {
		t.Fatalf("usage portal HTML missing expected content")
	}
}

func TestUsagePortalSupportsHead(t *testing.T) {
	server := newTestServer(t)
	writeTestManagementAsset(t, server, "<!doctype html><title>Usage Portal</title>")

	req := httptest.NewRequest(http.MethodHead, "/usage", nil)
	rr := httptest.NewRecorder()
	server.engine.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if contentType := rr.Header().Get("Content-Type"); contentType != "text/html; charset=utf-8" {
		t.Fatalf("content type = %q", contentType)
	}
}

func TestUsagePortalPluralRedirectsToCanonicalPath(t *testing.T) {
	server := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/usages/test-key?window=30d", nil)
	rr := httptest.NewRecorder()
	server.engine.ServeHTTP(rr, req)

	if rr.Code != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d, want %d body=%s", rr.Code, http.StatusTemporaryRedirect, rr.Body.String())
	}
	if location := rr.Header().Get("Location"); location != "/usage/test-key?window=30d" {
		t.Fatalf("location = %q", location)
	}
}

func TestUsagePortalDataValidatesAPIKey(t *testing.T) {
	server := newTestServer(t)

	t.Run("valid key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/usage/test-key/data?window=30d", nil)
		rr := httptest.NewRecorder()
		server.engine.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d body=%s", rr.Code, http.StatusOK, rr.Body.String())
		}
		var payload struct {
			Active                 bool `json:"active"`
			WindowDays             int  `json:"window_days"`
			UsageStatisticsEnabled bool `json:"usage_statistics_enabled"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
			t.Fatalf("unmarshal response: %v body=%s", err, rr.Body.String())
		}
		if !payload.Active {
			t.Fatalf("expected active payload")
		}
		if payload.WindowDays != 30 {
			t.Fatalf("window days = %d, want 30", payload.WindowDays)
		}
		if payload.UsageStatisticsEnabled {
			t.Fatalf("test server should have usage statistics disabled")
		}
	})

	t.Run("invalid key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/usage/bad-key/data", nil)
		rr := httptest.NewRecorder()
		server.engine.ServeHTTP(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d body=%s", rr.Code, http.StatusUnauthorized, rr.Body.String())
		}
	})
}

func containsAll(value string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(value, part) {
			return false
		}
	}
	return true
}

func writeTestManagementAsset(t *testing.T, server *Server, body string) {
	t.Helper()

	path := managementasset.FilePath(server.configFilePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create management asset dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write management asset: %v", err)
	}
}
