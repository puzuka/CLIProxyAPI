package usageportal

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	internallogging "github.com/router-for-me/CLIProxyAPI/v7/internal/logging"
	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

const (
	retentionDays     = 60
	maxRecentRequests = 200
)

type tokenUsage struct {
	InputTokens         int64 `json:"input_tokens"`
	OutputTokens        int64 `json:"output_tokens"`
	ReasoningTokens     int64 `json:"reasoning_tokens"`
	CachedTokens        int64 `json:"cached_tokens"`
	CacheReadTokens     int64 `json:"cache_read_tokens"`
	CacheCreationTokens int64 `json:"cache_creation_tokens"`
	TotalTokens         int64 `json:"total_tokens"`
}

type Aggregate struct {
	Requests int64      `json:"requests"`
	Success  int64      `json:"success"`
	Failed   int64      `json:"failed"`
	Tokens   tokenUsage `json:"tokens"`
}

type DailyPoint struct {
	Date string `json:"date"`
	Aggregate
}

type RecentRequest struct {
	Time            time.Time `json:"time"`
	Provider        string    `json:"provider"`
	Model           string    `json:"model"`
	Alias           string    `json:"alias,omitempty"`
	Endpoint        string    `json:"endpoint,omitempty"`
	RequestID       string    `json:"request_id,omitempty"`
	ReasoningEffort string    `json:"reasoning_effort,omitempty"`
	InputTokens     int64     `json:"input_tokens"`
	OutputTokens    int64     `json:"output_tokens"`
	CachedTokens    int64     `json:"cached_tokens"`
	TotalTokens     int64     `json:"total_tokens"`
	LatencyMs       int64     `json:"latency_ms"`
	StatusCode      int       `json:"status_code"`
	Failed          bool      `json:"failed"`
}

type Snapshot struct {
	KeyLabel               string          `json:"key_label"`
	Active                 bool            `json:"active"`
	UsageStatisticsEnabled bool            `json:"usage_statistics_enabled"`
	RetentionDays          int             `json:"retention_days"`
	WindowDays             int             `json:"window_days"`
	UpdatedAt              *time.Time      `json:"updated_at,omitempty"`
	Totals                 Aggregate       `json:"totals"`
	Series                 []DailyPoint    `json:"series"`
	RecentRequests         []RecentRequest `json:"recent_requests"`
}

type keyUsage struct {
	daily       map[string]Aggregate
	recent      []RecentRequest
	lastUpdated time.Time
}

type Store struct {
	enabled atomic.Bool
	mu      sync.Mutex
	byKey   map[string]*keyUsage
}

var defaultStore = newStore()

func init() {
	coreusage.RegisterPlugin(defaultStore)
}

func newStore() *Store {
	store := &Store{
		byKey: make(map[string]*keyUsage),
	}
	store.enabled.Store(true)
	return store
}

func SetEnabled(enabled bool) {
	defaultStore.SetEnabled(enabled)
}

func Enabled() bool {
	return defaultStore.Enabled()
}

func SnapshotForKey(apiKey string, windowDays int, active bool, now time.Time) Snapshot {
	return defaultStore.Snapshot(apiKey, windowDays, active, now)
}

func ResetForTesting() {
	defaultStore.Reset()
}

func (s *Store) SetEnabled(enabled bool) {
	if s == nil {
		return
	}
	s.enabled.Store(enabled)
	if !enabled {
		s.Reset()
	}
}

func (s *Store) Enabled() bool {
	return s != nil && s.enabled.Load()
}

func (s *Store) Reset() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.byKey = make(map[string]*keyUsage)
	s.mu.Unlock()
}

func (s *Store) HandleUsage(ctx context.Context, record coreusage.Record) {
	if s == nil || !s.enabled.Load() {
		return
	}
	apiKey := strings.TrimSpace(record.APIKey)
	if apiKey == "" {
		return
	}

	timestamp := record.RequestedAt
	if timestamp.IsZero() {
		timestamp = time.Now()
	}
	tokens := normalizeTokens(record.Detail)
	failed := record.Failed
	statusCode := record.Fail.StatusCode
	if failed {
		if statusCode <= 0 {
			statusCode = http.StatusInternalServerError
		}
	} else {
		statusCode = http.StatusOK
	}

	request := RecentRequest{
		Time:            timestamp.UTC(),
		Provider:        cleanText(record.Provider, "unknown"),
		Model:           cleanText(record.Model, "unknown"),
		Alias:           strings.TrimSpace(record.Alias),
		Endpoint:        strings.TrimSpace(internallogging.GetEndpoint(ctx)),
		RequestID:       strings.TrimSpace(internallogging.GetRequestID(ctx)),
		ReasoningEffort: strings.TrimSpace(record.ReasoningEffort),
		InputTokens:     tokens.InputTokens,
		OutputTokens:    tokens.OutputTokens,
		CachedTokens:    tokens.CachedTokens,
		TotalTokens:     tokens.TotalTokens,
		LatencyMs:       record.Latency.Milliseconds(),
		StatusCode:      statusCode,
		Failed:          failed,
	}

	aggregate := Aggregate{
		Requests: 1,
		Tokens:   tokens,
	}
	if failed {
		aggregate.Failed = 1
	} else {
		aggregate.Success = 1
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	entry := s.byKey[apiKey]
	if entry == nil {
		entry = &keyUsage{daily: make(map[string]Aggregate)}
		s.byKey[apiKey] = entry
	}
	day := localDay(timestamp)
	existing := entry.daily[day]
	existing.add(aggregate)
	entry.daily[day] = existing
	entry.recent = append(entry.recent, request)
	if len(entry.recent) > maxRecentRequests {
		entry.recent = append([]RecentRequest(nil), entry.recent[len(entry.recent)-maxRecentRequests:]...)
	}
	entry.lastUpdated = timestamp.UTC()
	s.pruneLocked(time.Now())
}

func (s *Store) Snapshot(apiKey string, windowDays int, active bool, now time.Time) Snapshot {
	if s == nil {
		return Snapshot{}
	}
	apiKey = strings.TrimSpace(apiKey)
	windowDays = normalizeWindowDays(windowDays)
	if now.IsZero() {
		now = time.Now()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)

	out := Snapshot{
		KeyLabel:               MaskAPIKey(apiKey),
		Active:                 active,
		UsageStatisticsEnabled: s.enabled.Load(),
		RetentionDays:          retentionDays,
		WindowDays:             windowDays,
		Series:                 make([]DailyPoint, 0, windowDays),
	}

	entry := s.byKey[apiKey]
	today := startOfLocalDay(now)
	start := today.AddDate(0, 0, -(windowDays - 1))
	for i := 0; i < windowDays; i++ {
		day := start.AddDate(0, 0, i)
		key := day.Format("2006-01-02")
		aggregate := Aggregate{}
		if entry != nil {
			aggregate = entry.daily[key]
		}
		out.Totals.add(aggregate)
		out.Series = append(out.Series, DailyPoint{
			Date:      key,
			Aggregate: aggregate,
		})
	}

	if entry == nil {
		return out
	}
	if !entry.lastUpdated.IsZero() {
		updated := entry.lastUpdated
		out.UpdatedAt = &updated
	}
	for i := len(entry.recent) - 1; i >= 0; i-- {
		request := entry.recent[i]
		if request.Time.Before(start.UTC()) {
			continue
		}
		out.RecentRequests = append(out.RecentRequests, request)
	}
	return out
}

func (s *Store) pruneLocked(now time.Time) {
	if s == nil {
		return
	}
	if now.IsZero() {
		now = time.Now()
	}
	cutoff := startOfLocalDay(now).AddDate(0, 0, -(retentionDays - 1))
	for key, entry := range s.byKey {
		if entry == nil {
			delete(s.byKey, key)
			continue
		}
		for day := range entry.daily {
			parsed, err := time.ParseInLocation("2006-01-02", day, time.Local)
			if err != nil || parsed.Before(cutoff) {
				delete(entry.daily, day)
			}
		}
		recent := entry.recent[:0]
		for _, request := range entry.recent {
			if !request.Time.Before(cutoff.UTC()) {
				recent = append(recent, request)
			}
		}
		entry.recent = recent
		if len(entry.daily) == 0 && len(entry.recent) == 0 {
			delete(s.byKey, key)
		}
	}
}

func normalizeTokens(detail coreusage.Detail) tokenUsage {
	total := detail.TotalTokens
	if total == 0 {
		total = detail.InputTokens + detail.OutputTokens + detail.ReasoningTokens
	}
	if total == 0 {
		total = detail.InputTokens + detail.OutputTokens + detail.ReasoningTokens + detail.CachedTokens
	}
	return tokenUsage{
		InputTokens:         detail.InputTokens,
		OutputTokens:        detail.OutputTokens,
		ReasoningTokens:     detail.ReasoningTokens,
		CachedTokens:        detail.CachedTokens,
		CacheReadTokens:     detail.CacheReadTokens,
		CacheCreationTokens: detail.CacheCreationTokens,
		TotalTokens:         total,
	}
}

func (a *Aggregate) add(other Aggregate) {
	a.Requests += other.Requests
	a.Success += other.Success
	a.Failed += other.Failed
	a.Tokens.InputTokens += other.Tokens.InputTokens
	a.Tokens.OutputTokens += other.Tokens.OutputTokens
	a.Tokens.ReasoningTokens += other.Tokens.ReasoningTokens
	a.Tokens.CachedTokens += other.Tokens.CachedTokens
	a.Tokens.CacheReadTokens += other.Tokens.CacheReadTokens
	a.Tokens.CacheCreationTokens += other.Tokens.CacheCreationTokens
	a.Tokens.TotalTokens += other.Tokens.TotalTokens
}

func normalizeWindowDays(days int) int {
	switch days {
	case 1, 7, 30, 60:
		return days
	default:
		return 7
	}
}

func localDay(t time.Time) string {
	return startOfLocalDay(t).Format("2006-01-02")
}

func startOfLocalDay(t time.Time) time.Time {
	local := t.In(time.Local)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.Local)
}

func cleanText(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func MaskAPIKey(apiKey string) string {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return ""
	}
	if len(apiKey) <= 10 {
		return "****"
	}
	return apiKey[:6] + "..." + apiKey[len(apiKey)-4:]
}
