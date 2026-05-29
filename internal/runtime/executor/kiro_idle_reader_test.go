package executor

import (
	"io"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

// TestIdleTimeoutReaderAbortsStalledRead verifies the watchdog closes the body
// when no bytes arrive within the idle window, unblocking a stalled read.
func TestIdleTimeoutReaderAbortsStalledRead(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()

	r := newIdleTimeoutReader(pr, 50*time.Millisecond, 0)
	defer r.stop()

	start := time.Now()
	if _, err := r.Read(make([]byte, 16)); err == nil {
		t.Fatal("expected read to abort on idle timeout, got nil error")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("watchdog did not fire promptly (blocked %v)", elapsed)
	}
}

// TestIdleTimeoutReaderFirstTokenApplies verifies the (shorter) first-token
// budget bounds the first read even when the idle timeout is large.
func TestIdleTimeoutReaderFirstTokenApplies(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()

	r := newIdleTimeoutReader(pr, 10*time.Second, 50*time.Millisecond)
	defer r.stop()

	start := time.Now()
	if _, err := r.Read(make([]byte, 16)); err == nil {
		t.Fatal("expected first-token timeout to abort read, got nil error")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("first-token timeout did not fire promptly (blocked %v)", elapsed)
	}
}

func TestKiroTimeoutResolvers(t *testing.T) {
	def := NewKiroExecutor(&config.Config{})
	if got := def.streamingReadTimeout(); got != kiroStreamingReadTimeout {
		t.Errorf("default streamingReadTimeout = %v, want %v", got, kiroStreamingReadTimeout)
	}
	if got := def.firstTokenTimeout(); got != 0 {
		t.Errorf("default firstTokenTimeout = %v, want 0 (disabled)", got)
	}

	custom := NewKiroExecutor(&config.Config{KiroStreamingReadTimeout: 120, KiroFirstTokenTimeout: 30})
	if got := custom.streamingReadTimeout(); got != 120*time.Second {
		t.Errorf("custom streamingReadTimeout = %v, want 120s", got)
	}
	if got := custom.firstTokenTimeout(); got != 30*time.Second {
		t.Errorf("custom firstTokenTimeout = %v, want 30s", got)
	}
}
