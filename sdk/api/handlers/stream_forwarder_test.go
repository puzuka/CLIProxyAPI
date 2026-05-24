package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestStreamBootstrapKeepAliveWritesHeartbeatOnTick(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	commits := 0
	bootstrap := newStreamBootstrapKeepAlive(c, w, 5*time.Millisecond, func() {
		commits++
		SetSSEHeaders(c)
	}, nil)
	defer bootstrap.Stop()

	select {
	case <-bootstrap.C():
		bootstrap.WriteKeepAlive()
	case <-time.After(250 * time.Millisecond):
		t.Fatal("timed out waiting for bootstrap keep-alive")
	}

	if commits != 1 {
		t.Fatalf("commits = %d, want 1", commits)
	}
	if got := w.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-cache, no-transform" {
		t.Fatalf("Cache-Control = %q, want no-cache, no-transform", got)
	}
	if got := w.Body.String(); got != ": keep-alive\n\n" {
		t.Fatalf("body = %q, want keep-alive comment", got)
	}
	if !bootstrap.Committed() {
		t.Fatal("bootstrap should be committed after keep-alive")
	}

	bootstrap.WriteKeepAlive()
	if commits != 1 {
		t.Fatalf("commits after second keep-alive = %d, want 1", commits)
	}
}

func TestSetSSEHeadersDisablesProxyBuffering(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	SetSSEHeaders(c)

	assertHeader(t, w, "Content-Type", "text/event-stream")
	assertHeader(t, w, "Cache-Control", "no-cache, no-transform")
	assertHeader(t, w, "Connection", "keep-alive")
	assertHeader(t, w, "X-Accel-Buffering", "no")
}

func assertHeader(t *testing.T, w *httptest.ResponseRecorder, key string, want string) {
	t.Helper()
	if got := w.Header().Get(key); got != want {
		t.Fatalf("%s = %q, want %q", key, got, want)
	}
}

var _ http.Flusher = (*httptest.ResponseRecorder)(nil)
