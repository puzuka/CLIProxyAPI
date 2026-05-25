package usageportal

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	internallogging "github.com/router-for-me/CLIProxyAPI/v7/internal/logging"
	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

func TestStoreSnapshotAggregatesByAPIKey(t *testing.T) {
	store := newStore()
	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.Local)
	ctx := internallogging.WithEndpoint(context.Background(), "/v1/chat/completions")
	ctx = internallogging.WithRequestID(ctx, "req_123")

	store.HandleUsage(ctx, coreusage.Record{
		Provider:    "openai",
		Model:       "gpt-5.5",
		Alias:       "fast",
		APIKey:      "sk-test-key-123456",
		RequestedAt: now,
		Latency:     1200 * time.Millisecond,
		Detail: coreusage.Detail{
			InputTokens:  10,
			OutputTokens: 5,
			CachedTokens: 3,
			TotalTokens:  15,
		},
	})
	store.HandleUsage(ctx, coreusage.Record{
		Provider:    "openai",
		Model:       "gpt-5.5",
		APIKey:      "other-key",
		RequestedAt: now,
		Detail:      coreusage.Detail{TotalTokens: 99},
	})

	snapshot := store.Snapshot("sk-test-key-123456", 7, true, now)
	if !snapshot.Active {
		t.Fatalf("expected active snapshot")
	}
	if snapshot.KeyLabel != "sk-tes...3456" {
		t.Fatalf("key label = %q", snapshot.KeyLabel)
	}
	if snapshot.Totals.Requests != 1 {
		t.Fatalf("requests = %d, want 1", snapshot.Totals.Requests)
	}
	if snapshot.Totals.Tokens.TotalTokens != 15 {
		t.Fatalf("total tokens = %d, want 15", snapshot.Totals.Tokens.TotalTokens)
	}
	if len(snapshot.RecentRequests) != 1 {
		t.Fatalf("recent requests = %d, want 1", len(snapshot.RecentRequests))
	}
	recent := snapshot.RecentRequests[0]
	if recent.Endpoint != "/v1/chat/completions" {
		t.Fatalf("endpoint = %q", recent.Endpoint)
	}
	if recent.RequestID != "req_123" {
		t.Fatalf("request id = %q", recent.RequestID)
	}
	if recent.TotalTokens != 15 || recent.CachedTokens != 3 {
		t.Fatalf("recent tokens = total %d cached %d, want 15/3", recent.TotalTokens, recent.CachedTokens)
	}
}

func TestStoreDisabledDropsRecords(t *testing.T) {
	store := newStore()
	store.SetEnabled(false)
	store.HandleUsage(context.Background(), coreusage.Record{
		APIKey:      "sk-test-key",
		RequestedAt: time.Now(),
		Detail:      coreusage.Detail{TotalTokens: 42},
	})

	snapshot := store.Snapshot("sk-test-key", 7, true, time.Now())
	if snapshot.UsageStatisticsEnabled {
		t.Fatalf("expected usage statistics to be disabled")
	}
	if snapshot.Totals.Requests != 0 {
		t.Fatalf("requests = %d, want 0", snapshot.Totals.Requests)
	}
}

func TestStoreAnalyticsAggregatesManagementDimensions(t *testing.T) {
	store := newStore()
	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.Local)
	ctx := internallogging.WithEndpoint(context.Background(), "/v1/chat/completions")

	store.HandleUsage(ctx, coreusage.Record{
		Provider:    "openai",
		Model:       "gpt-5.5",
		Alias:       "client-model",
		APIKey:      "sk-test-key-123456",
		Source:      "account-a",
		RequestedAt: now,
		Detail: coreusage.Detail{
			InputTokens:     12,
			OutputTokens:    8,
			ReasoningTokens: 3,
			CachedTokens:    2,
			TotalTokens:     23,
		},
	})

	snapshot := store.Analytics("today", now.Add(time.Minute))
	if snapshot.Period != "today" {
		t.Fatalf("period = %q, want today", snapshot.Period)
	}
	if snapshot.Totals.Requests != 1 {
		t.Fatalf("requests = %d, want 1", snapshot.Totals.Requests)
	}
	if snapshot.Totals.Tokens.TotalTokens != 23 {
		t.Fatalf("total tokens = %d, want 23", snapshot.Totals.Tokens.TotalTokens)
	}
	if len(snapshot.Series) != 24 {
		t.Fatalf("series buckets = %d, want 24", len(snapshot.Series))
	}
	if len(snapshot.ByProvider) != 1 || snapshot.ByProvider[0].Provider != "openai" {
		t.Fatalf("by provider = %+v, want openai", snapshot.ByProvider)
	}
	if len(snapshot.ByModel) != 1 || snapshot.ByModel[0].Model != "gpt-5.5" {
		t.Fatalf("by model = %+v, want gpt-5.5", snapshot.ByModel)
	}
	if len(snapshot.ByAccount) != 1 || snapshot.ByAccount[0].AccountLabel != "account-a" {
		t.Fatalf("by account = %+v, want account-a", snapshot.ByAccount)
	}
	if len(snapshot.ByAPIKey) != 1 || snapshot.ByAPIKey[0].APIKeyLabel != "sk-tes...3456" {
		t.Fatalf("by api key = %+v, want masked key", snapshot.ByAPIKey)
	}
	if len(snapshot.ByEndpoint) != 1 || snapshot.ByEndpoint[0].Endpoint != "/v1/chat/completions" {
		t.Fatalf("by endpoint = %+v, want endpoint", snapshot.ByEndpoint)
	}
}

func TestStoreRequestDetailsFiltersAndPaginates(t *testing.T) {
	store := newStore()
	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.Local)

	store.HandleUsage(context.Background(), coreusage.Record{
		Provider:    "openai",
		Model:       "gpt-5.5",
		APIKey:      "sk-one-123456",
		RequestedAt: now,
		Detail:      coreusage.Detail{InputTokens: 1, TotalTokens: 1},
	})
	store.HandleUsage(context.Background(), coreusage.Record{
		Provider:    "claude",
		Model:       "claude-sonnet",
		APIKey:      "sk-two-123456",
		RequestedAt: now.Add(time.Minute),
		Failed:      true,
		Fail:        coreusage.Failure{StatusCode: 429},
		Detail:      coreusage.Detail{OutputTokens: 2, TotalTokens: 2},
	})

	result := store.RequestDetails(RequestDetailsFilter{
		Page:     1,
		PageSize: 1,
		Status:   "failed",
	}, now.Add(2*time.Minute))
	if result.Pagination.TotalItems != 1 {
		t.Fatalf("total items = %d, want 1", result.Pagination.TotalItems)
	}
	if result.Totals.Requests != 1 || result.Totals.Failed != 1 || result.Totals.Tokens.OutputTokens != 2 || result.Totals.Tokens.TotalTokens != 2 {
		t.Fatalf("failed totals = %+v, want failed output token summary", result.Totals)
	}
	if len(result.Details) != 1 || result.Details[0].Provider != "claude" {
		t.Fatalf("details = %+v, want claude failure", result.Details)
	}
	if result.Details[0].StatusCode != 429 {
		t.Fatalf("status code = %d, want 429", result.Details[0].StatusCode)
	}

	result = store.RequestDetails(RequestDetailsFilter{
		Page:     1,
		PageSize: 10,
		APIKey:   "sk-one-123456",
	}, now.Add(2*time.Minute))
	if result.Pagination.TotalItems != 1 {
		t.Fatalf("api key total items = %d, want 1", result.Pagination.TotalItems)
	}
	if result.Totals.Requests != 1 || result.Totals.Success != 1 || result.Totals.Tokens.InputTokens != 1 || result.Totals.Tokens.TotalTokens != 1 {
		t.Fatalf("api key totals = %+v, want success input token summary", result.Totals)
	}
	if len(result.Details) != 1 || result.Details[0].APIKeyLabel != "sk-one...3456" {
		t.Fatalf("api key details = %+v, want sk-one...3456", result.Details)
	}
}

func TestStoreRequestDetailsStoresSanitizedCapturedBodies(t *testing.T) {
	store := newStore()
	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.Local)
	ctx := internallogging.WithEndpoint(context.Background(), "/v1/chat/completions")
	ctx = internallogging.WithRequestID(ctx, "req_detail")
	ctx = WithHTTPRequestDetail(ctx, HTTPRequestDetail{
		URL:            "/v1/chat/completions",
		Method:         "POST",
		RequestID:      "req_detail",
		RequestHeaders: map[string][]string{"Authorization": {"Bearer super-secret-token"}, "X-Trace": {"trace-ok"}},
		RequestBody:    []byte(`{"api_key":"super-secret-key","messages":[{"content":"hello"}]}`),
		StatusCode:     201,
		ResponseHeaders: map[string][]string{
			"Content-Type": {"application/json"},
			"Set-Cookie":   {"session=super-secret-cookie"},
		},
		ResponseBody: []byte(`{"output":"done","access_token":"super-secret-access-token"}`),
		APIRequest:   []byte(`{"token":"super-secret-provider-token","prompt":"hello"}`),
		APIResponse:  bytes.Repeat([]byte("x"), maxDetailFieldBytes+10),
	})

	store.HandleUsage(ctx, coreusage.Record{
		Provider:    "openai",
		Model:       "gpt-5.5",
		APIKey:      "sk-detail-123456",
		RequestedAt: now,
		Detail:      coreusage.Detail{InputTokens: 1, OutputTokens: 2, TotalTokens: 3},
	})

	result := store.RequestDetails(RequestDetailsFilter{Page: 1, PageSize: 10}, now.Add(time.Minute))
	if len(result.Details) != 1 {
		t.Fatalf("details = %d, want 1", len(result.Details))
	}
	detail := result.Details[0]
	request, ok := detail.Request.(*HTTPMessageDetail)
	if !ok {
		t.Fatalf("request detail type = %T, want *HTTPMessageDetail", detail.Request)
	}
	if request.Headers["Authorization"][0] != "[REDACTED]" {
		t.Fatalf("authorization header = %q, want redacted", request.Headers["Authorization"][0])
	}
	requestBody, ok := request.Body.(map[string]any)
	if !ok {
		t.Fatalf("request body type = %T, want map", request.Body)
	}
	if requestBody["api_key"] != "[REDACTED]" {
		t.Fatalf("request api_key = %v, want redacted", requestBody["api_key"])
	}
	response, ok := detail.Response.(*HTTPResponseDetail)
	if !ok {
		t.Fatalf("response detail type = %T, want *HTTPResponseDetail", detail.Response)
	}
	if response.Headers["Set-Cookie"][0] != "[REDACTED]" {
		t.Fatalf("set-cookie header = %q, want redacted", response.Headers["Set-Cookie"][0])
	}
	responseBody, ok := response.Body.(map[string]any)
	if !ok {
		t.Fatalf("response body type = %T, want map", response.Body)
	}
	if responseBody["access_token"] != "[REDACTED]" {
		t.Fatalf("response access_token = %v, want redacted", responseBody["access_token"])
	}
	if _, ok := detail.ProviderResponse.(TruncatedField); !ok {
		t.Fatalf("provider response type = %T, want TruncatedField", detail.ProviderResponse)
	}

	raw, err := json.Marshal(detail)
	if err != nil {
		t.Fatalf("marshal detail: %v", err)
	}
	for _, secret := range []string{"super-secret-token", "super-secret-key", "super-secret-provider-token", "super-secret-cookie", "super-secret-access-token"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("detail leaked secret %q in %s", secret, raw)
		}
	}
	if !strings.Contains(string(raw), `"_truncated":true`) {
		t.Fatalf("detail missing truncation marker: %s", raw)
	}
}

func TestStoreRequestDetailsMergesHTTPDetailRecordedBeforeUsage(t *testing.T) {
	store := newStore()
	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.Local)
	store.RecordHTTPRequestDetail(HTTPRequestDetail{
		URL:                  "/v1/responses",
		Method:               "POST",
		RequestID:            "req_pending",
		StatusCode:           200,
		ResponseBody:         []byte(`{"output":"late response"}`),
		RequestTimestamp:     now,
		APIResponseTimestamp: now.Add(175 * time.Millisecond),
	})

	ctx := internallogging.WithRequestID(context.Background(), "req_pending")
	store.HandleUsage(ctx, coreusage.Record{
		Provider:    "openai",
		Model:       "gpt-5.5",
		RequestedAt: now,
		Detail:      coreusage.Detail{TotalTokens: 1},
	})

	result := store.RequestDetails(RequestDetailsFilter{Page: 1, PageSize: 10}, now.Add(time.Minute))
	if len(result.Details) != 1 {
		t.Fatalf("details = %d, want 1", len(result.Details))
	}
	response, ok := result.Details[0].Response.(*HTTPResponseDetail)
	if !ok {
		t.Fatalf("response detail type = %T, want *HTTPResponseDetail", result.Details[0].Response)
	}
	body, ok := response.Body.(map[string]any)
	if !ok || body["output"] != "late response" {
		t.Fatalf("response body = %#v, want late response", response.Body)
	}
	if result.Details[0].Latency.TTFTMs != 175 {
		t.Fatalf("ttft = %d, want 175", result.Details[0].Latency.TTFTMs)
	}
}

func TestStoreSubscribeReceivesUsageUpdate(t *testing.T) {
	store := newStore()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates := store.Subscribe(ctx)

	store.HandleUsage(context.Background(), coreusage.Record{
		Provider:    "openai",
		Model:       "gpt-5.5",
		RequestedAt: time.Now(),
		Detail:      coreusage.Detail{TotalTokens: 1},
	})

	select {
	case <-updates:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for usage update notification")
	}
}
