package usageportal

import (
	"context"
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
