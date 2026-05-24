package config

import "testing"

func TestParseConfigBytes_DefaultsStreamingKeepAlive(t *testing.T) {
	cfg, err := ParseConfigBytes([]byte("port: 8080\n"))
	if err != nil {
		t.Fatalf("ParseConfigBytes() error = %v", err)
	}
	if cfg.Streaming.KeepAliveSeconds != DefaultStreamingKeepAliveSeconds {
		t.Fatalf("keepalive-seconds = %d, want %d", cfg.Streaming.KeepAliveSeconds, DefaultStreamingKeepAliveSeconds)
	}
}

func TestParseConfigBytes_AllowsStreamingKeepAliveDisable(t *testing.T) {
	cfg, err := ParseConfigBytes([]byte("streaming:\n  keepalive-seconds: 0\n"))
	if err != nil {
		t.Fatalf("ParseConfigBytes() error = %v", err)
	}
	if cfg.Streaming.KeepAliveSeconds != 0 {
		t.Fatalf("keepalive-seconds = %d, want 0", cfg.Streaming.KeepAliveSeconds)
	}
}
