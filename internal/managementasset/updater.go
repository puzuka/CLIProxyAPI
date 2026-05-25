package managementasset

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "embed"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

const usageAnalyticsExtensionName = "usage-analytics-extension.js"
const (
	defaultManagementReleaseURL  = "https://api.github.com/repos/tuanle96/Cli-Proxy-API-Management-Center/releases/latest"
	defaultManagementFallbackURL = "https://cpamc.router-for.me/"
	managementAssetName          = "management.html"
	httpUserAgent                = "CLIProxyAPI-management-updater"
	managementSyncMinInterval    = 30 * time.Second
	updateCheckInterval          = 3 * time.Hour
	maxAssetDownloadSize         = 50 << 20 // 10 MB safety limit for management asset downloads
)

// ManagementFileName exposes the control panel asset filename.
const ManagementFileName = managementAssetName

//go:embed static/management.html
var bundledManagementHTML []byte

//go:embed static/usage-analytics-extension.js
var bundledUsageAnalyticsExtensionJS []byte

// SetCurrentConfig is retained for callers that update management asset state after config changes.
// The management panel is bundled locally, so no remote update state is needed.
func SetCurrentConfig(_ *config.Config) {}

// StaticDir resolves the directory for a MANAGEMENT_STATIC_PATH override.
func StaticDir(_ string) string {
	override := strings.TrimSpace(os.Getenv("MANAGEMENT_STATIC_PATH"))
	if override == "" {
		return ""
	}
	cleaned := filepath.Clean(override)
	if strings.EqualFold(filepath.Base(cleaned), managementAssetName) {
		return filepath.Dir(cleaned)
	}
	return cleaned
}

// FilePath resolves the explicit local override path for the management control panel asset.
func FilePath(configFilePath string) string {
	dir := StaticDir(configFilePath)
	if dir == "" {
		return ""
	}
	if strings.EqualFold(filepath.Base(dir), managementAssetName) {
		return dir
	}
	return filepath.Join(dir, ManagementFileName)
}

// ReadManagementHTML returns the local management control panel asset.
// MANAGEMENT_STATIC_PATH may point to a file or directory for local development overrides.
func ReadManagementHTML() ([]byte, error) {
	override := strings.TrimSpace(os.Getenv("MANAGEMENT_STATIC_PATH"))
	if override != "" {
		path := filepath.Clean(override)
		if !strings.EqualFold(filepath.Base(path), managementAssetName) {
			path = filepath.Join(path, ManagementFileName)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return InjectUsageAnalyticsExtension(content), nil
	}
	if len(bundledManagementHTML) == 0 {
		return nil, errors.New("bundled management.html is empty")
	}
	return InjectUsageAnalyticsExtension(bundledManagementHTML), nil
}

// ReadUsageAnalyticsExtensionJS returns the standalone Management Center usage analytics script.
func ReadUsageAnalyticsExtensionJS() ([]byte, error) {
	override := strings.TrimSpace(os.Getenv("MANAGEMENT_STATIC_PATH"))
	if override != "" {
		path := filepath.Clean(override)
		if strings.EqualFold(filepath.Base(path), managementAssetName) {
			path = filepath.Dir(path)
		}
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			path = filepath.Dir(path)
		}
		extensionPath := filepath.Join(path, usageAnalyticsExtensionName)
		if content, err := os.ReadFile(extensionPath); err == nil {
			return content, nil
		}
	}
	if len(bundledUsageAnalyticsExtensionJS) == 0 {
		return nil, errors.New("bundled usage analytics extension is empty")
	}
	return append([]byte(nil), bundledUsageAnalyticsExtensionJS...), nil
}

// ReadUsageAnalyticsHTML returns the Management Center shell with usage analytics page mode enabled.
func ReadUsageAnalyticsHTML() ([]byte, error) {
	content, err := ReadManagementHTML()
	if err != nil {
		return nil, err
	}
	return InjectUsageAnalyticsPageMode(content), nil
}

// InjectUsageAnalyticsExtension adds the independent usage analytics menu/page loader.
func InjectUsageAnalyticsExtension(content []byte) []byte {
	if len(content) == 0 || strings.Contains(string(content), usageAnalyticsExtensionName) {
		return append([]byte(nil), content...)
	}

	tag := []byte(`<script defer src="/usage-analytics-extension.js" data-cpa-extension="usage-analytics"></script>`)
	html := string(content)
	lower := strings.ToLower(html)
	bodyIndex := strings.LastIndex(lower, "</body>")
	if bodyIndex < 0 {
		out := make([]byte, 0, len(content)+len(tag))
		out = append(out, content...)
		out = append(out, tag...)
		return out
	}

	out := make([]byte, 0, len(content)+len(tag))
	out = append(out, content[:bodyIndex]...)
	out = append(out, tag...)
	out = append(out, content[bodyIndex:]...)
	return out
}

// InjectUsageAnalyticsPageMode makes the management shell mount the usage analytics page.
func InjectUsageAnalyticsPageMode(content []byte) []byte {
	if len(content) == 0 || strings.Contains(string(content), "__CPA_USAGE_ANALYTICS_MODE__") {
		return append([]byte(nil), content...)
	}

	tag := []byte(`<script data-cpa-extension="usage-analytics-page-mode">window.__CPA_USAGE_ANALYTICS_MODE__='page';</script>`)
	html := string(content)
	lower := strings.ToLower(html)
	extensionIndex := strings.Index(lower, usageAnalyticsExtensionName)
	if extensionIndex >= 0 {
		scriptIndex := strings.LastIndex(lower[:extensionIndex], "<script")
		if scriptIndex >= 0 {
			out := make([]byte, 0, len(content)+len(tag))
			out = append(out, content[:scriptIndex]...)
			out = append(out, tag...)
			out = append(out, content[scriptIndex:]...)
			return out
		}
	}

	bodyIndex := strings.LastIndex(lower, "</body>")
	if bodyIndex < 0 {
		out := make([]byte, 0, len(content)+len(tag))
		out = append(out, content...)
		out = append(out, tag...)
		return out
	}

	out := make([]byte, 0, len(content)+len(tag))
	out = append(out, content[:bodyIndex]...)
	out = append(out, tag...)
	out = append(out, content[bodyIndex:]...)
	return out
}
