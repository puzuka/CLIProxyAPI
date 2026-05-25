package management

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/usageportal"
	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

func TestGetUsageAnalyticsStatsReturnsAggregates(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resetUsageAnalyticsForTest(t)
	publishUsageAnalyticsRecord(t, coreusage.Record{
		Provider:    "openai",
		Model:       "gpt-5.5",
		APIKey:      "sk-test-key-123456",
		Source:      "account-a",
		RequestedAt: time.Now(),
		Detail: coreusage.Detail{
			InputTokens:  3,
			OutputTokens: 4,
			TotalTokens:  7,
		},
	})

	rec := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(rec)
	ginCtx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/usage-analytics/stats?period=today", nil)

	h := &Handler{}
	h.GetUsageAnalyticsStats(ginCtx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var payload struct {
		Totals struct {
			Requests int64 `json:"requests"`
			Tokens   struct {
				TotalTokens int64 `json:"total_tokens"`
			} `json:"tokens"`
		} `json:"totals"`
		ByProvider []struct {
			Provider string `json:"provider"`
		} `json:"by_provider"`
		ByAPIKey []struct {
			APIKeyLabel string `json:"api_key_label"`
		} `json:"by_api_key"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload.Totals.Requests != 1 {
		t.Fatalf("requests = %d, want 1", payload.Totals.Requests)
	}
	if payload.Totals.Tokens.TotalTokens != 7 {
		t.Fatalf("total tokens = %d, want 7", payload.Totals.Tokens.TotalTokens)
	}
	if len(payload.ByProvider) != 1 || payload.ByProvider[0].Provider != "openai" {
		t.Fatalf("by provider = %+v, want openai", payload.ByProvider)
	}
	if len(payload.ByAPIKey) != 1 || payload.ByAPIKey[0].APIKeyLabel != "sk-tes...3456" {
		t.Fatalf("by api key = %+v, want sk-tes...3456", payload.ByAPIKey)
	}
}

func TestGetUsageAnalyticsStatsRejectsInvalidPeriod(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(rec)
	ginCtx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/usage-analytics/stats?period=bad", nil)

	h := &Handler{}
	h.GetUsageAnalyticsStats(ginCtx)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestGetUsageAnalyticsRequestDetailsValidatesPageSize(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(rec)
	ginCtx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/usage-analytics/request-details?page_size=101", nil)

	h := &Handler{}
	h.GetUsageAnalyticsRequestDetails(ginCtx)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestGetUsageAnalyticsAPIKeyResolvesConfigKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resetUsageAnalyticsForTest(t)
	publishUsageAnalyticsRecord(t, coreusage.Record{
		Provider:    "openai",
		Model:       "gpt-5.5",
		APIKey:      "sk-test-key-123456",
		RequestedAt: time.Now(),
		Detail:      coreusage.Detail{TotalTokens: 9},
	})

	rec := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(rec)
	ginCtx.Params = gin.Params{{Key: "id", Value: "0"}}
	ginCtx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/usage-analytics/api-keys/0?period=today", nil)

	h := &Handler{cfg: &config.Config{SDKConfig: config.SDKConfig{APIKeys: []string{"sk-test-key-123456"}}}}
	h.GetUsageAnalyticsAPIKey(ginCtx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var payload struct {
		Stats struct {
			Requests int64 `json:"requests"`
		} `json:"stats"`
		Requests []usageportal.RecentRequest `json:"requests"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload.Stats.Requests != 1 {
		t.Fatalf("api key requests = %d, want 1", payload.Stats.Requests)
	}
	if len(payload.Requests) != 1 {
		t.Fatalf("recent requests = %d, want 1", len(payload.Requests))
	}
}

func TestStreamUsageAnalyticsSendsUpdates(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resetUsageAnalyticsForTest(t)

	router := gin.New()
	h := &Handler{}
	router.GET("/usage-analytics/stream", h.StreamUsageAnalytics)
	server := httptest.NewServer(router)
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/usage-analytics/stream?period=today", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Fatalf("close stream body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := resp.Header.Get("Content-Type"); !strings.Contains(got, "text/event-stream") {
		t.Fatalf("content type = %q, want text/event-stream", got)
	}

	reader := bufio.NewReader(resp.Body)
	initial := readUsageAnalyticsStreamSnapshot(t, reader, 2*time.Second)
	if !initial.UsageStatisticsEnabled {
		t.Fatal("initial stream snapshot should report usage statistics enabled")
	}
	if initial.Totals.Requests != 0 {
		t.Fatalf("initial requests = %d, want 0", initial.Totals.Requests)
	}

	stopPublishing := make(chan struct{})
	defer close(stopPublishing)
	go func() {
		ticker := time.NewTicker(25 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopPublishing:
				return
			case <-ticker.C:
				coreusage.PublishRecord(context.Background(), coreusage.Record{
					Provider:    "openai",
					Model:       "gpt-5.5",
					APIKey:      "sk-test-key-123456",
					RequestedAt: time.Now(),
					Detail:      coreusage.Detail{TotalTokens: 1},
				})
			}
		}
	}()

	updated := readUsageAnalyticsStreamSnapshot(t, reader, 2*time.Second)
	if updated.Totals.Requests == 0 {
		t.Fatal("updated stream snapshot should include published usage")
	}
}

func resetUsageAnalyticsForTest(t *testing.T) {
	t.Helper()
	usageportal.ResetForTesting()
	usageportal.SetEnabled(true)
	t.Cleanup(func() {
		usageportal.ResetForTesting()
	})
}

func publishUsageAnalyticsRecord(t *testing.T, record coreusage.Record) {
	t.Helper()
	coreusage.PublishRecord(context.Background(), record)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if usageportal.Analytics("today", time.Now()).Totals.Requests > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for usage record")
}

func readUsageAnalyticsStreamSnapshot(t *testing.T, reader *bufio.Reader, timeout time.Duration) usageportal.AnalyticsSnapshot {
	t.Helper()
	type result struct {
		snapshot usageportal.AnalyticsSnapshot
		err      error
	}
	ch := make(chan result, 1)
	go func() {
		snapshot, err := readUsageAnalyticsStreamSnapshotLine(reader)
		ch <- result{snapshot: snapshot, err: err}
	}()

	select {
	case result := <-ch:
		if result.err != nil {
			t.Fatalf("read stream snapshot: %v", result.err)
		}
		return result.snapshot
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for usage analytics stream event")
		return usageportal.AnalyticsSnapshot{}
	}
}

func readUsageAnalyticsStreamSnapshotLine(reader *bufio.Reader) (usageportal.AnalyticsSnapshot, error) {
	var data []string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return usageportal.AnalyticsSnapshot{}, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if len(data) == 0 {
				continue
			}
			break
		}
		if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}

	var snapshot usageportal.AnalyticsSnapshot
	if err := json.Unmarshal([]byte(strings.Join(data, "\n")), &snapshot); err != nil {
		return usageportal.AnalyticsSnapshot{}, err
	}
	return snapshot, nil
}
