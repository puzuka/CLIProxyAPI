package copilot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProbeChatCompletionModelCallable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("path = %s, want /chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Fatalf("Authorization = %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if got := body["model"]; got != "callable-model" {
			t.Fatalf("model = %v, want callable-model", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_1","choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer server.Close()

	result, err := NewCopilotAuth(nil).ProbeChatCompletionModel(context.Background(), server.URL, "token", "callable-model")
	if err != nil {
		t.Fatalf("ProbeChatCompletionModel returned error: %v", err)
	}
	if result == nil || !result.Callable {
		t.Fatalf("expected callable result, got %#v", result)
	}
	if result.ModelNotSupported {
		t.Fatalf("did not expect model_not_supported result")
	}
}

func TestProbeChatCompletionModelUnsupported(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"The requested model is not supported.","code":"model_not_supported"}}`))
	}))
	defer server.Close()

	result, err := NewCopilotAuth(nil).ProbeChatCompletionModel(context.Background(), server.URL, "token", "claude-sonnet-4.5")
	if err != nil {
		t.Fatalf("ProbeChatCompletionModel returned error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result")
	}
	if result.Callable {
		t.Fatalf("expected non-callable result")
	}
	if !result.ModelNotSupported {
		t.Fatalf("expected model_not_supported result, got %#v", result)
	}
	if result.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", result.StatusCode, http.StatusBadRequest)
	}
}

func TestProbeResponsesModelCallable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Fatalf("path = %s, want /responses", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Fatalf("Authorization = %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if got := body["model"]; got != "gpt-5.3-codex" {
			t.Fatalf("model = %v, want gpt-5.3-codex", got)
		}
		if got := body["max_output_tokens"]; got != float64(16) {
			t.Fatalf("max_output_tokens = %v, want 16", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}]}`))
	}))
	defer server.Close()

	result, err := NewCopilotAuth(nil).ProbeResponsesModel(context.Background(), server.URL, "token", "gpt-5.3-codex")
	if err != nil {
		t.Fatalf("ProbeResponsesModel returned error: %v", err)
	}
	if result == nil || !result.Callable {
		t.Fatalf("expected callable result, got %#v", result)
	}
	if result.ModelNotSupported {
		t.Fatalf("did not expect model_not_supported result")
	}
}
