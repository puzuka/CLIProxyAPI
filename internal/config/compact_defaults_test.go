package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigExampleEnablesCompactFallbackByDefault(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "config.example.yaml"))
	if err != nil {
		t.Fatalf("read config.example.yaml: %v", err)
	}

	cfg, err := ParseConfigBytes(data)
	if err != nil {
		t.Fatalf("ParseConfigBytes(config.example.yaml): %v", err)
	}

	if !cfg.CompactFallback.Enabled {
		t.Fatal("CompactFallback.Enabled = false, want true")
	}
	if got := cfg.CompactFallback.Model; got != "gpt-5.5" {
		t.Fatalf("CompactFallback.Model = %q, want %q", got, "gpt-5.5")
	}
	if got := cfg.CompactFallback.AppliesToProviders; len(got) != 1 || got[0] != "*" {
		t.Fatalf("CompactFallback.AppliesToProviders = %#v, want []string{\"*\"}", got)
	}
	if !cfg.LoggingToFile {
		t.Fatal("LoggingToFile = false, want true")
	}
	if !cfg.CompactFallback.TriggerLog {
		t.Fatal("CompactFallback.TriggerLog = false, want true")
	}
	if !cfg.CustomCompact.Enabled {
		t.Fatal("CustomCompact.Enabled = false, want true")
	}
	if cfg.CustomCompact.Model != "" {
		t.Fatalf("CustomCompact.Model = %q, want empty default to use request model", cfg.CustomCompact.Model)
	}
	if !cfg.CustomCompact.TriggerLog {
		t.Fatal("CustomCompact.TriggerLog = false, want true")
	}
}
