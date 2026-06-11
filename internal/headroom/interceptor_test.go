package headroom

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func TestHeadroomConfig_ShouldRouteModel(t *testing.T) {
	tests := []struct {
		name        string
		cfg         config.HeadroomConfig
		model       string
		shouldRoute bool
	}{
		{
			name: "disabled config should not route",
			cfg: config.HeadroomConfig{
				Enabled:      false,
				TargetModels: []string{"*"},
			},
			model:       "gpt-4",
			shouldRoute: false,
		},
		{
			name: "wildcard should match all models",
			cfg: config.HeadroomConfig{
				Enabled:      true,
				TargetModels: []string{"*"},
			},
			model:       "gpt-4",
			shouldRoute: true,
		},
		{
			name: "specific model should match",
			cfg: config.HeadroomConfig{
				Enabled:      true,
				TargetModels: []string{"claude-3-5-sonnet-20241022", "gpt-4"},
			},
			model:       "gpt-4",
			shouldRoute: true,
		},
		{
			name: "non-matching model should not route",
			cfg: config.HeadroomConfig{
				Enabled:      true,
				TargetModels: []string{"claude-3-5-sonnet-20241022"},
			},
			model:       "gpt-4",
			shouldRoute: false,
		},
		{
			name: "empty target models with enabled should not route",
			cfg: config.HeadroomConfig{
				Enabled:      true,
				TargetModels: []string{},
			},
			model:       "gpt-4",
			shouldRoute: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cfg.ShouldRouteModel(tt.model)
			if got != tt.shouldRoute {
				t.Errorf("ShouldRouteModel() = %v, want %v", got, tt.shouldRoute)
			}
		})
	}
}

func TestHeadroomConfig_ApplyDefaults(t *testing.T) {
	cfg := &config.HeadroomConfig{}
	cfg.ApplyDefaults()

	if cfg.Endpoint != "http://127.0.0.1:8787" {
		t.Errorf("Expected default endpoint http://127.0.0.1:8787, got %s", cfg.Endpoint)
	}

	if cfg.Command != "headroom proxy --port 8787" {
		t.Errorf("Expected default command 'headroom proxy --port 8787', got %s", cfg.Command)
	}

	if cfg.Timeout != 120 {
		t.Errorf("Expected default timeout 120, got %d", cfg.Timeout)
	}

	if len(cfg.TargetModels) != 1 || cfg.TargetModels[0] != "*" {
		t.Errorf("Expected default target models [\"*\"], got %v", cfg.TargetModels)
	}
}

func TestHeadroomConfig_ApplyDefaults_PreservesExisting(t *testing.T) {
	cfg := &config.HeadroomConfig{
		Endpoint:     "http://custom:9999",
		Command:      "custom-command",
		Timeout:      60,
		TargetModels: []string{"gpt-4"},
	}
	cfg.ApplyDefaults()

	if cfg.Endpoint != "http://custom:9999" {
		t.Errorf("Expected custom endpoint preserved, got %s", cfg.Endpoint)
	}

	if cfg.Command != "custom-command" {
		t.Errorf("Expected custom command preserved, got %s", cfg.Command)
	}

	if cfg.Timeout != 60 {
		t.Errorf("Expected custom timeout preserved, got %d", cfg.Timeout)
	}

	if len(cfg.TargetModels) != 1 || cfg.TargetModels[0] != "gpt-4" {
		t.Errorf("Expected custom target models preserved, got %v", cfg.TargetModels)
	}
}
