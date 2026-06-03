package helps

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	internallogging "github.com/router-for-me/CLIProxyAPI/v7/internal/logging"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/usageportal"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

func TestParseOpenAIUsageChatCompletions(t *testing.T) {
	data := []byte(`{"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3,"prompt_tokens_details":{"cached_tokens":4},"completion_tokens_details":{"reasoning_tokens":5}}}`)
	detail := ParseOpenAIUsage(data)
	if detail.InputTokens != 1 {
		t.Fatalf("input tokens = %d, want %d", detail.InputTokens, 1)
	}
	if detail.OutputTokens != 2 {
		t.Fatalf("output tokens = %d, want %d", detail.OutputTokens, 2)
	}
	if detail.TotalTokens != 3 {
		t.Fatalf("total tokens = %d, want %d", detail.TotalTokens, 3)
	}
	if detail.CachedTokens != 4 {
		t.Fatalf("cached tokens = %d, want %d", detail.CachedTokens, 4)
	}
	if detail.ReasoningTokens != 5 {
		t.Fatalf("reasoning tokens = %d, want %d", detail.ReasoningTokens, 5)
	}
}

func TestParseOpenAIUsageResponses(t *testing.T) {
	data := []byte(`{"usage":{"input_tokens":10,"output_tokens":20,"total_tokens":30,"input_tokens_details":{"cached_tokens":7},"output_tokens_details":{"reasoning_tokens":9}}}`)
	detail := ParseOpenAIUsage(data)
	if detail.InputTokens != 10 {
		t.Fatalf("input tokens = %d, want %d", detail.InputTokens, 10)
	}
	if detail.OutputTokens != 20 {
		t.Fatalf("output tokens = %d, want %d", detail.OutputTokens, 20)
	}
	if detail.TotalTokens != 30 {
		t.Fatalf("total tokens = %d, want %d", detail.TotalTokens, 30)
	}
	if detail.CachedTokens != 7 {
		t.Fatalf("cached tokens = %d, want %d", detail.CachedTokens, 7)
	}
	if detail.ReasoningTokens != 9 {
		t.Fatalf("reasoning tokens = %d, want %d", detail.ReasoningTokens, 9)
	}
}

func TestParseOpenAIUsageIgnoresNullUsage(t *testing.T) {
	data := []byte(`{"usage":null}`)
	detail := ParseOpenAIUsage(data)
	if detail != (usage.Detail{}) {
		t.Fatalf("detail = %+v, want zero detail", detail)
	}
}

func TestParseOpenAIStreamUsageIgnoresNullUsage(t *testing.T) {
	line := []byte(`data: {"id":"chunk_1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":null}],"usage":null}`)
	if detail, ok := ParseOpenAIStreamUsage(line); ok {
		t.Fatalf("ParseOpenAIStreamUsage() = (%+v, true), want false for null usage", detail)
	}
}

func TestParseOpenAIStreamUsageResponsesFields(t *testing.T) {
	line := []byte(`data: {"id":"chunk_1","object":"chat.completion.chunk","choices":[],"usage":{"input_tokens":8,"output_tokens":5,"total_tokens":13,"input_tokens_details":{"cached_tokens":3},"output_tokens_details":{"reasoning_tokens":2}}}`)
	detail, ok := ParseOpenAIStreamUsage(line)
	if !ok {
		t.Fatal("ParseOpenAIStreamUsage() ok = false, want true")
	}
	if detail.InputTokens != 8 {
		t.Fatalf("input tokens = %d, want %d", detail.InputTokens, 8)
	}
	if detail.OutputTokens != 5 {
		t.Fatalf("output tokens = %d, want %d", detail.OutputTokens, 5)
	}
	if detail.TotalTokens != 13 {
		t.Fatalf("total tokens = %d, want %d", detail.TotalTokens, 13)
	}
	if detail.CachedTokens != 3 {
		t.Fatalf("cached tokens = %d, want %d", detail.CachedTokens, 3)
	}
	if detail.ReasoningTokens != 2 {
		t.Fatalf("reasoning tokens = %d, want %d", detail.ReasoningTokens, 2)
	}
}

func TestParseGeminiCLIUsage_TopLevelUsageMetadata(t *testing.T) {
	data := []byte(`{"usageMetadata":{"promptTokenCount":11,"candidatesTokenCount":7,"thoughtsTokenCount":3,"totalTokenCount":21,"cachedContentTokenCount":5}}`)
	detail := ParseGeminiCLIUsage(data)
	if detail.InputTokens != 11 {
		t.Fatalf("input tokens = %d, want %d", detail.InputTokens, 11)
	}
	if detail.OutputTokens != 7 {
		t.Fatalf("output tokens = %d, want %d", detail.OutputTokens, 7)
	}
	if detail.ReasoningTokens != 3 {
		t.Fatalf("reasoning tokens = %d, want %d", detail.ReasoningTokens, 3)
	}
	if detail.TotalTokens != 21 {
		t.Fatalf("total tokens = %d, want %d", detail.TotalTokens, 21)
	}
	if detail.CachedTokens != 5 {
		t.Fatalf("cached tokens = %d, want %d", detail.CachedTokens, 5)
	}
}

func TestParseGeminiCLIStreamUsage_ResponseSnakeCaseUsageMetadata(t *testing.T) {
	line := []byte(`data: {"response":{"usage_metadata":{"promptTokenCount":13,"candidatesTokenCount":2,"totalTokenCount":15}}}`)
	detail, ok := ParseGeminiCLIStreamUsage(line)
	if !ok {
		t.Fatal("ParseGeminiCLIStreamUsage() ok = false, want true")
	}
	if detail.InputTokens != 13 {
		t.Fatalf("input tokens = %d, want %d", detail.InputTokens, 13)
	}
	if detail.OutputTokens != 2 {
		t.Fatalf("output tokens = %d, want %d", detail.OutputTokens, 2)
	}
	if detail.TotalTokens != 15 {
		t.Fatalf("total tokens = %d, want %d", detail.TotalTokens, 15)
	}
}

func TestParseGeminiCLIStreamUsage_IgnoresTrafficTypeOnlyUsageMetadata(t *testing.T) {
	line := []byte(`data: {"response":{"usageMetadata":{"trafficType":"ON_DEMAND"}}}`)
	if detail, ok := ParseGeminiCLIStreamUsage(line); ok {
		t.Fatalf("ParseGeminiCLIStreamUsage() = (%+v, true), want false for traffic-only usage metadata", detail)
	}
}

func TestUsageReporterBuildRecordIncludesLatency(t *testing.T) {
	reporter := &UsageReporter{
		provider:    "openai",
		model:       "gpt-5.4",
		requestedAt: time.Now().Add(-1500 * time.Millisecond),
	}

	record := reporter.buildRecord(usage.Detail{TotalTokens: 3}, false)
	if record.Latency < time.Second {
		t.Fatalf("latency = %v, want >= 1s", record.Latency)
	}
	if record.Latency > 3*time.Second {
		t.Fatalf("latency = %v, want <= 3s", record.Latency)
	}
}

func TestUsageReporterTracksActiveRequestLifecycle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	usageportal.ResetForTesting()
	usageportal.SetEnabled(true)
	t.Cleanup(usageportal.ResetForTesting)

	rec := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(rec)
	ginCtx.Set("userApiKey", "sk-client-123456")
	ctx := context.WithValue(context.Background(), "gin", ginCtx)

	reporter := NewUsageReporter(ctx, "openai", "gpt-5.4", nil)
	active := usageportal.Analytics("today", time.Now()).ActiveRequests
	if len(active) != 1 {
		t.Fatalf("active requests = %d, want 1", len(active))
	}
	if active[0].Provider != "openai" || active[0].Model != "gpt-5.4" {
		t.Fatalf("active request = %+v, want openai/gpt-5.4", active[0])
	}
	if active[0].APIKeyLabel != "sk-cli...3456" {
		t.Fatalf("active api key label = %q, want sk-cli...3456", active[0].APIKeyLabel)
	}

	reporter.Publish(ctx, usage.Detail{TotalTokens: 1})
	var snapshot usageportal.AnalyticsSnapshot
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snapshot = usageportal.Analytics("today", time.Now())
		if len(snapshot.ByAPIKey) == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	active = snapshot.ActiveRequests
	if len(active) != 0 {
		t.Fatalf("active requests after publish = %d, want 0", len(active))
	}
	if len(snapshot.ByAPIKey) != 1 || snapshot.ByAPIKey[0].APIKeyLabel != "sk-cli...3456" {
		t.Fatalf("by api key = %+v, want sk-cli...3456", snapshot.ByAPIKey)
	}
}

func TestUsageReporterPublishesCapturedRequestDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	usageportal.ResetForTesting()
	usageportal.SetEnabled(true)
	t.Cleanup(usageportal.ResetForTesting)

	rec := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions?api_key=super-secret-query", nil)
	req.Header.Set("Authorization", "Bearer super-secret-header")
	req.Header.Set("X-Trace", "trace-ok")
	ginCtx.Request = req
	internallogging.SetGinRequestID(ginCtx, "req_usage_detail")
	ginCtx.Set("userApiKey", "sk-user-123456")
	ginCtx.Set(requestBodyOverrideKey, []byte(`{"api_key":"super-secret-body","input":"hello"}`))
	ginCtx.Set(apiRequestKey, []byte(`{"token":"super-secret-provider","prompt":"hello"}`))
	ginCtx.Set(apiResponseKey, []byte(`{"output":"ok"}`))
	ginCtx.Writer.Header().Set("Content-Type", "application/json")

	ctx := context.WithValue(context.Background(), "gin", ginCtx)
	ctx = internallogging.WithRequestID(ctx, "req_usage_detail")
	ctx = internallogging.WithEndpoint(ctx, "/v1/chat/completions")
	reporter := NewUsageReporter(ctx, "openai", "gpt-5.4", nil)
	reporter.Publish(ctx, usage.Detail{InputTokens: 1, OutputTokens: 2, TotalTokens: 3})

	snapshot := waitForUsageDetails(t, "req_usage_detail")
	detail := snapshot.Details[0]
	if detail.RequestID != "req_usage_detail" {
		t.Fatalf("request id = %q, want req_usage_detail", detail.RequestID)
	}
	if detail.ProviderRequest == nil || detail.ProviderResponse == nil || detail.Request == nil {
		t.Fatalf("captured detail missing payloads: %+v", detail)
	}
	raw, err := json.Marshal(detail)
	if err != nil {
		t.Fatalf("marshal detail: %v", err)
	}
	text := string(raw)
	for _, secret := range []string{"super-secret-query", "super-secret-header", "super-secret-body", "super-secret-provider"} {
		if strings.Contains(text, secret) {
			t.Fatalf("detail leaked %q in %s", secret, text)
		}
	}
	if !strings.Contains(text, "[REDACTED]") {
		t.Fatalf("detail missing redaction marker: %s", text)
	}
}

func TestUsageReporterBuildRecordIncludesRequestedModelAlias(t *testing.T) {
	ctx := usage.WithRequestedModelAlias(context.Background(), "client-gpt")
	reporter := NewUsageReporter(ctx, "openai", "gpt-5.4", nil)

	record := reporter.buildRecord(usage.Detail{TotalTokens: 3}, false)
	if record.Model != "gpt-5.4" {
		t.Fatalf("model = %q, want %q", record.Model, "gpt-5.4")
	}
	if record.Alias != "client-gpt" {
		t.Fatalf("alias = %q, want %q", record.Alias, "client-gpt")
	}
}

func TestUsageReporterBuildRecordIncludesReasoningEffort(t *testing.T) {
	ctx := usage.WithReasoningEffort(context.Background(), "medium")
	reporter := NewUsageReporter(ctx, "openai", "gpt-5.4", nil)

	record := reporter.buildRecord(usage.Detail{TotalTokens: 3}, false)
	if record.ReasoningEffort != "medium" {
		t.Fatalf("reasoning effort = %q, want %q", record.ReasoningEffort, "medium")
	}
}

func TestUsageReporterBuildAdditionalModelRecordSkipsZeroTokens(t *testing.T) {
	reporter := &UsageReporter{
		provider:    "codex",
		model:       "gpt-5.4",
		requestedAt: time.Now(),
	}

	if _, ok := reporter.buildAdditionalModelRecord("gpt-image-2", usage.Detail{}); ok {
		t.Fatalf("expected all-zero token usage to be skipped")
	}
	if _, ok := reporter.buildAdditionalModelRecord("gpt-image-2", usage.Detail{InputTokens: 2}); !ok {
		t.Fatalf("expected non-zero input token usage to be recorded")
	}
	if _, ok := reporter.buildAdditionalModelRecord("gpt-image-2", usage.Detail{CachedTokens: 2}); !ok {
		t.Fatalf("expected non-zero cached token usage to be recorded")
	}
}

func TestParseGeminiStreamUsage_ResponseSnakeCaseUsageMetadata(t *testing.T) {
	line := []byte(`data: {"usageMetadata":{"promptTokenCount":13,"candidatesTokenCount":2,"totalTokenCount":15}}`)
	detail, ok := ParseGeminiStreamUsage(line)
	if !ok {
		t.Fatal("ParseGeminiStreamUsage() ok = false, want true")
	}
	if detail.InputTokens != 13 {
		t.Fatalf("input tokens = %d, want %d", detail.InputTokens, 13)
	}
	if detail.OutputTokens != 2 {
		t.Fatalf("output tokens = %d, want %d", detail.OutputTokens, 2)
	}
	if detail.TotalTokens != 15 {
		t.Fatalf("total tokens = %d, want %d", detail.TotalTokens, 15)
	}
}

func TestParseGeminiStreamUsage_IgnoresTrafficTypeOnlyUsageMetadata(t *testing.T) {
	line := []byte(`data: {"usageMetadata":{"trafficType":"ON_DEMAND"}}`)
	if detail, ok := ParseGeminiStreamUsage(line); ok {
		t.Fatalf("ParseGeminiStreamUsage() = (%+v, true), want false for traffic-only usage metadata", detail)
	}
}

func waitForUsageDetails(t *testing.T, requestID string) usageportal.RequestDetailsSnapshot {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snapshot := usageportal.RequestDetails(usageportal.RequestDetailsFilter{Page: 1, PageSize: 10}, time.Now())
		for _, detail := range snapshot.Details {
			if detail.RequestID == requestID {
				return usageportal.RequestDetailsSnapshot{
					Details:    []usageportal.RequestDetail{detail},
					Pagination: snapshot.Pagination,
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for usage detail %q", requestID)
	return usageportal.RequestDetailsSnapshot{}
}
