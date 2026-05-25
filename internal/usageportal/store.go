package usageportal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	internallogging "github.com/router-for-me/CLIProxyAPI/v7/internal/logging"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/util"
	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

const (
	retentionDays            = 60
	maxRecentRequests        = 200
	maxAnalyticsRecentEvents = 5000
	maxRequestDetails        = 1000
	maxDetailFieldBytes      = 5 * 1024
	maxPendingHTTPDetails    = 1000
	activeRequestTTL         = 90 * time.Second
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
	CostUSD  float64    `json:"cost_usd"`
}

type DailyPoint struct {
	Date string `json:"date"`
	Aggregate
}

type ChartPoint struct {
	Label    string  `json:"label"`
	Date     string  `json:"date,omitempty"`
	Requests int64   `json:"requests"`
	Tokens   int64   `json:"tokens"`
	CostUSD  float64 `json:"cost_usd"`
}

type RecentRequest struct {
	Time            time.Time `json:"time"`
	Provider        string    `json:"provider"`
	Model           string    `json:"model"`
	Alias           string    `json:"alias,omitempty"`
	Source          string    `json:"source,omitempty"`
	AccountLabel    string    `json:"account_label,omitempty"`
	APIKeyLabel     string    `json:"api_key_label,omitempty"`
	AuthIndex       string    `json:"auth_index,omitempty"`
	AuthType        string    `json:"auth_type,omitempty"`
	Endpoint        string    `json:"endpoint,omitempty"`
	RequestID       string    `json:"request_id,omitempty"`
	ReasoningEffort string    `json:"reasoning_effort,omitempty"`
	InputTokens     int64     `json:"input_tokens"`
	OutputTokens    int64     `json:"output_tokens"`
	ReasoningTokens int64     `json:"reasoning_tokens"`
	CachedTokens    int64     `json:"cached_tokens"`
	TotalTokens     int64     `json:"total_tokens"`
	CostUSD         float64   `json:"cost_usd"`
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

type AnalyticsGroup struct {
	Key             string     `json:"key"`
	Provider        string     `json:"provider,omitempty"`
	Model           string     `json:"model,omitempty"`
	Alias           string     `json:"alias,omitempty"`
	AccountLabel    string     `json:"account_label,omitempty"`
	APIKeyLabel     string     `json:"api_key_label,omitempty"`
	Endpoint        string     `json:"endpoint,omitempty"`
	LastUsed        *time.Time `json:"last_used,omitempty"`
	Requests        int64      `json:"requests"`
	Success         int64      `json:"success"`
	Failed          int64      `json:"failed"`
	InputTokens     int64      `json:"input_tokens"`
	OutputTokens    int64      `json:"output_tokens"`
	ReasoningTokens int64      `json:"reasoning_tokens"`
	CachedTokens    int64      `json:"cached_tokens"`
	TotalTokens     int64      `json:"total_tokens"`
	CostUSD         float64    `json:"cost_usd"`
}

type ActiveRequest struct {
	ID              string    `json:"id"`
	Provider        string    `json:"provider"`
	Model           string    `json:"model"`
	Alias           string    `json:"alias,omitempty"`
	AccountLabel    string    `json:"account_label,omitempty"`
	APIKeyLabel     string    `json:"api_key_label,omitempty"`
	Endpoint        string    `json:"endpoint,omitempty"`
	RequestID       string    `json:"request_id,omitempty"`
	ReasoningEffort string    `json:"reasoning_effort,omitempty"`
	StartedAt       time.Time `json:"started_at"`
	AgeMs           int64     `json:"age_ms"`
}

type AnalyticsSnapshot struct {
	Period                 string           `json:"period"`
	UsageStatisticsEnabled bool             `json:"usage_statistics_enabled"`
	RetentionDays          int              `json:"retention_days"`
	UpdatedAt              *time.Time       `json:"updated_at,omitempty"`
	Totals                 Aggregate        `json:"totals"`
	Series                 []ChartPoint     `json:"series"`
	ActiveRequests         []ActiveRequest  `json:"active_requests"`
	RecentRequests         []RecentRequest  `json:"recent_requests"`
	ByProvider             []AnalyticsGroup `json:"by_provider"`
	ByModel                []AnalyticsGroup `json:"by_model"`
	ByAccount              []AnalyticsGroup `json:"by_account"`
	ByAPIKey               []AnalyticsGroup `json:"by_api_key"`
	ByEndpoint             []AnalyticsGroup `json:"by_endpoint"`
}

type RequestDetailsFilter struct {
	Page      int
	PageSize  int
	Provider  string
	Model     string
	APIKey    string
	Endpoint  string
	Status    string
	StartTime time.Time
	EndTime   time.Time
}

type RequestDetailsSnapshot struct {
	Details    []RequestDetail `json:"details"`
	Totals     Aggregate       `json:"totals"`
	Pagination Pagination      `json:"pagination"`
}

type Pagination struct {
	Page       int  `json:"page"`
	PageSize   int  `json:"page_size"`
	TotalItems int  `json:"total_items"`
	TotalPages int  `json:"total_pages"`
	HasNext    bool `json:"has_next"`
	HasPrev    bool `json:"has_prev"`
}

type keyUsage struct {
	daily       map[string]Aggregate
	recent      []RecentRequest
	lastUpdated time.Time
}

type dayUsage struct {
	Aggregate
	ByProvider map[string]*AnalyticsGroup
	ByModel    map[string]*AnalyticsGroup
	ByAccount  map[string]*AnalyticsGroup
	ByAPIKey   map[string]*AnalyticsGroup
	ByEndpoint map[string]*AnalyticsGroup
}

type DetailLatency struct {
	TTFTMs  int64 `json:"ttft,omitempty"`
	TotalMs int64 `json:"total,omitempty"`
}

type RequestDetail struct {
	ID                string        `json:"id"`
	Timestamp         time.Time     `json:"timestamp"`
	Provider          string        `json:"provider,omitempty"`
	Model             string        `json:"model,omitempty"`
	Alias             string        `json:"alias,omitempty"`
	AccountLabel      string        `json:"account_label,omitempty"`
	APIKeyLabel       string        `json:"api_key_label,omitempty"`
	Endpoint          string        `json:"endpoint,omitempty"`
	RequestID         string        `json:"request_id,omitempty"`
	ReasoningEffort   string        `json:"reasoning_effort,omitempty"`
	Status            string        `json:"status"`
	StatusCode        int           `json:"status_code"`
	Failed            bool          `json:"failed"`
	Latency           DetailLatency `json:"latency"`
	Tokens            tokenUsage    `json:"tokens"`
	CostUSD           float64       `json:"cost_usd"`
	Request           any           `json:"request,omitempty"`
	ProviderRequest   any           `json:"provider_request,omitempty"`
	ProviderResponse  any           `json:"provider_response,omitempty"`
	Response          any           `json:"response,omitempty"`
	WebsocketTimeline any           `json:"websocket_timeline,omitempty"`
}

type HTTPMessageDetail struct {
	URL       string              `json:"url,omitempty"`
	Method    string              `json:"method,omitempty"`
	Headers   map[string][]string `json:"headers,omitempty"`
	Body      any                 `json:"body,omitempty"`
	Timestamp *time.Time          `json:"timestamp,omitempty"`
}

type HTTPResponseDetail struct {
	StatusCode int                 `json:"status_code,omitempty"`
	Headers    map[string][]string `json:"headers,omitempty"`
	Body       any                 `json:"body,omitempty"`
	Timestamp  *time.Time          `json:"timestamp,omitempty"`
}

type TruncatedField struct {
	Truncated    bool   `json:"_truncated"`
	OriginalSize int    `json:"_original_size"`
	Preview      string `json:"_preview"`
}

type HTTPRequestDetail struct {
	URL                  string
	Method               string
	RequestHeaders       map[string][]string
	RequestBody          []byte
	StatusCode           int
	ResponseHeaders      map[string][]string
	ResponseBody         []byte
	WebsocketTimeline    []byte
	APIRequest           []byte
	APIResponse          []byte
	APIWebsocketTimeline []byte
	RequestID            string
	RequestTimestamp     time.Time
	APIResponseTimestamp time.Time
}

type Store struct {
	enabled                   atomic.Bool
	mu                        sync.Mutex
	byKey                     map[string]*keyUsage
	daily                     map[string]*dayUsage
	recent                    []RecentRequest
	details                   []RequestDetail
	detailIndex               map[string]int
	requestDetailsByRequestID map[string][]int
	pendingHTTPDetails        map[string]HTTPRequestDetail
	active                    map[string]ActiveRequest
	subscribers               map[chan struct{}]struct{}
	activeSeq                 uint64
	updatedAt                 time.Time
}

type httpRequestDetailContextKey struct{}

var defaultStore = newStore()

func init() {
	coreusage.RegisterPlugin(defaultStore)
}

func newStore() *Store {
	store := &Store{
		byKey:                     make(map[string]*keyUsage),
		daily:                     make(map[string]*dayUsage),
		detailIndex:               make(map[string]int),
		requestDetailsByRequestID: make(map[string][]int),
		pendingHTTPDetails:        make(map[string]HTTPRequestDetail),
		active:                    make(map[string]ActiveRequest),
		subscribers:               make(map[chan struct{}]struct{}),
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

func Analytics(period string, now time.Time) AnalyticsSnapshot {
	return defaultStore.Analytics(period, now)
}

func RequestDetails(filter RequestDetailsFilter, now time.Time) RequestDetailsSnapshot {
	return defaultStore.RequestDetails(filter, now)
}

func RecordHTTPRequestDetail(detail HTTPRequestDetail) {
	defaultStore.RecordHTTPRequestDetail(detail)
}

func Subscribe(ctx context.Context) <-chan struct{} {
	return defaultStore.Subscribe(ctx)
}

func TrackActiveStart(ctx context.Context, record coreusage.Record) string {
	return defaultStore.TrackActiveStart(ctx, record)
}

func TrackActiveFinish(id string) {
	defaultStore.TrackActiveFinish(id)
}

func ResetForTesting() {
	defaultStore.Reset()
}

func WithHTTPRequestDetail(ctx context.Context, detail HTTPRequestDetail) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if !detail.hasCapturedData() {
		return ctx
	}
	return context.WithValue(ctx, httpRequestDetailContextKey{}, detail.clone())
}

func HTTPRequestDetailFromContext(ctx context.Context) HTTPRequestDetail {
	if ctx == nil {
		return HTTPRequestDetail{}
	}
	if detail, ok := ctx.Value(httpRequestDetailContextKey{}).(HTTPRequestDetail); ok {
		return detail.clone()
	}
	return HTTPRequestDetail{}
}

func (s *Store) SetEnabled(enabled bool) {
	if s == nil {
		return
	}
	s.enabled.Store(enabled)
	if !enabled {
		s.Reset()
	}
	s.notifySubscribers()
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
	s.daily = make(map[string]*dayUsage)
	s.recent = nil
	s.details = nil
	s.detailIndex = make(map[string]int)
	s.requestDetailsByRequestID = make(map[string][]int)
	s.pendingHTTPDetails = make(map[string]HTTPRequestDetail)
	s.active = make(map[string]ActiveRequest)
	s.activeSeq = 0
	s.updatedAt = time.Time{}
	s.mu.Unlock()
	s.notifySubscribers()
}

func (s *Store) Subscribe(ctx context.Context) <-chan struct{} {
	ch := make(chan struct{}, 1)
	if s == nil {
		close(ch)
		return ch
	}
	s.mu.Lock()
	if s.subscribers == nil {
		s.subscribers = make(map[chan struct{}]struct{})
	}
	s.subscribers[ch] = struct{}{}
	s.mu.Unlock()

	go func() {
		<-ctx.Done()
		s.mu.Lock()
		delete(s.subscribers, ch)
		s.mu.Unlock()
		close(ch)
	}()

	return ch
}

func (s *Store) notifySubscribers() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.subscribers {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (s *Store) HandleUsage(ctx context.Context, record coreusage.Record) {
	if s == nil || !s.enabled.Load() {
		return
	}

	request, aggregate := requestFromRecord(ctx, record)
	httpDetail := HTTPRequestDetailFromContext(ctx)

	s.mu.Lock()

	apiKey := strings.TrimSpace(record.APIKey)
	if apiKey != "" {
		entry := s.byKey[apiKey]
		if entry == nil {
			entry = &keyUsage{daily: make(map[string]Aggregate)}
			s.byKey[apiKey] = entry
		}
		day := localDay(request.Time)
		existing := entry.daily[day]
		existing.add(aggregate)
		entry.daily[day] = existing
		entry.recent = append(entry.recent, request)
		if len(entry.recent) > maxRecentRequests {
			entry.recent = append([]RecentRequest(nil), entry.recent[len(entry.recent)-maxRecentRequests:]...)
		}
		entry.lastUpdated = request.Time.UTC()
	}

	day := localDay(request.Time)
	daily := s.daily[day]
	if daily == nil {
		daily = newDayUsage()
		s.daily[day] = daily
	}
	daily.add(request, aggregate)
	s.recent = append(s.recent, request)
	if len(s.recent) > maxAnalyticsRecentEvents {
		s.recent = append([]RecentRequest(nil), s.recent[len(s.recent)-maxAnalyticsRecentEvents:]...)
	}
	if request.RequestID != "" {
		if pending, ok := s.pendingHTTPDetails[request.RequestID]; ok {
			httpDetail = mergeHTTPRequestDetails(httpDetail, pending)
			delete(s.pendingHTTPDetails, request.RequestID)
		}
	}
	s.appendRequestDetailLocked(requestDetailFromRecord(request, record, httpDetail))
	s.updatedAt = request.Time.UTC()
	s.pruneLocked(time.Now())
	s.mu.Unlock()
	s.notifySubscribers()
}

func (s *Store) RecordHTTPRequestDetail(detail HTTPRequestDetail) {
	if s == nil || !s.enabled.Load() {
		return
	}
	detail = detail.clone()
	if !detail.hasCapturedData() {
		return
	}
	requestID := strings.TrimSpace(detail.RequestID)
	if requestID == "" {
		return
	}

	updated := false
	s.mu.Lock()
	if len(s.requestDetailsByRequestID[requestID]) == 0 {
		existing := s.pendingHTTPDetails[requestID]
		s.pendingHTTPDetails[requestID] = mergeHTTPRequestDetails(existing, detail)
		s.trimPendingHTTPDetailsLocked()
	} else {
		for _, idx := range s.requestDetailsByRequestID[requestID] {
			if idx < 0 || idx >= len(s.details) {
				continue
			}
			applyHTTPRequestDetail(&s.details[idx], detail)
			updated = true
		}
	}
	s.mu.Unlock()
	if updated {
		s.notifySubscribers()
	}
}

func (s *Store) TrackActiveStart(ctx context.Context, record coreusage.Record) string {
	if s == nil || !s.enabled.Load() {
		return ""
	}
	request, _ := requestFromRecord(ctx, record)

	s.mu.Lock()

	s.activeSeq++
	id := fmt.Sprintf("active-%d", s.activeSeq)
	s.active[id] = ActiveRequest{
		ID:              id,
		Provider:        request.Provider,
		Model:           request.Model,
		Alias:           request.Alias,
		AccountLabel:    request.AccountLabel,
		APIKeyLabel:     request.APIKeyLabel,
		Endpoint:        request.Endpoint,
		RequestID:       request.RequestID,
		ReasoningEffort: request.ReasoningEffort,
		StartedAt:       request.Time,
	}
	s.mu.Unlock()
	s.notifySubscribers()
	return id
}

func (s *Store) TrackActiveFinish(id string) {
	if s == nil || strings.TrimSpace(id) == "" {
		return
	}
	s.mu.Lock()
	delete(s.active, id)
	s.mu.Unlock()
	s.notifySubscribers()
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

func (s *Store) Analytics(period string, now time.Time) AnalyticsSnapshot {
	if s == nil {
		return AnalyticsSnapshot{}
	}
	if now.IsZero() {
		now = time.Now()
	}
	window := normalizeAnalyticsPeriod(period, now)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)
	s.pruneActiveLocked(now)

	out := AnalyticsSnapshot{
		Period:                 window.period,
		UsageStatisticsEnabled: s.enabled.Load(),
		RetentionDays:          retentionDays,
		Series:                 make([]ChartPoint, 0, window.bucketCount),
		ActiveRequests:         s.activeRequestsLocked(now),
		RecentRequests:         s.recentRequestsLocked(window),
	}
	if !s.updatedAt.IsZero() {
		updated := s.updatedAt
		out.UpdatedAt = &updated
	}

	if window.hourly {
		out = s.analyticsFromRecentLocked(out, window)
	} else {
		out = s.analyticsFromDailyLocked(out, window)
	}
	return out
}

func (s *Store) RequestDetails(filter RequestDetailsFilter, now time.Time) RequestDetailsSnapshot {
	if s == nil {
		return RequestDetailsSnapshot{}
	}
	if now.IsZero() {
		now = time.Now()
	}
	filter.normalize()

	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)

	matches := make([]RequestDetail, 0, len(s.details))
	var totals Aggregate
	for i := len(s.details) - 1; i >= 0; i-- {
		detail := s.details[i]
		if !filter.matchesDetail(detail) {
			continue
		}
		totals.add(aggregateFromDetail(detail))
		matches = append(matches, detail)
	}

	totalItems := len(matches)
	totalPages := 0
	if totalItems > 0 {
		totalPages = (totalItems + filter.PageSize - 1) / filter.PageSize
	}
	start := (filter.Page - 1) * filter.PageSize
	if start > totalItems {
		start = totalItems
	}
	end := start + filter.PageSize
	if end > totalItems {
		end = totalItems
	}

	return RequestDetailsSnapshot{
		Details: matches[start:end],
		Totals:  totals,
		Pagination: Pagination{
			Page:       filter.Page,
			PageSize:   filter.PageSize,
			TotalItems: totalItems,
			TotalPages: totalPages,
			HasNext:    totalPages > 0 && filter.Page < totalPages,
			HasPrev:    filter.Page > 1 && totalPages > 0,
		},
	}
}

func (s *Store) analyticsFromDailyLocked(out AnalyticsSnapshot, window analyticsPeriod) AnalyticsSnapshot {
	providers := make(map[string]*AnalyticsGroup)
	models := make(map[string]*AnalyticsGroup)
	accounts := make(map[string]*AnalyticsGroup)
	apiKeys := make(map[string]*AnalyticsGroup)
	endpoints := make(map[string]*AnalyticsGroup)

	buckets := make(map[string]*ChartPoint, window.bucketCount)
	for _, day := range window.days {
		buckets[day] = &ChartPoint{Label: shortDateLabel(day), Date: day}
	}

	for _, day := range sortedDayKeys(s.daily) {
		if !window.includesDay(day) {
			continue
		}
		daily := s.daily[day]
		if daily == nil {
			continue
		}
		out.Totals.add(daily.Aggregate)
		if bucket := buckets[day]; bucket != nil {
			bucket.Requests += daily.Requests
			bucket.Tokens += daily.Tokens.TotalTokens
			bucket.CostUSD += daily.CostUSD
		}
		mergeGroupMap(providers, daily.ByProvider)
		mergeGroupMap(models, daily.ByModel)
		mergeGroupMap(accounts, daily.ByAccount)
		mergeGroupMap(apiKeys, daily.ByAPIKey)
		mergeGroupMap(endpoints, daily.ByEndpoint)
	}

	for _, day := range window.days {
		if bucket := buckets[day]; bucket != nil {
			out.Series = append(out.Series, *bucket)
		}
	}
	out.ByProvider = sortedGroups(providers)
	out.ByModel = sortedGroups(models)
	out.ByAccount = sortedGroups(accounts)
	out.ByAPIKey = sortedGroups(apiKeys)
	out.ByEndpoint = sortedGroups(endpoints)
	return out
}

func (s *Store) analyticsFromRecentLocked(out AnalyticsSnapshot, window analyticsPeriod) AnalyticsSnapshot {
	providers := make(map[string]*AnalyticsGroup)
	models := make(map[string]*AnalyticsGroup)
	accounts := make(map[string]*AnalyticsGroup)
	apiKeys := make(map[string]*AnalyticsGroup)
	endpoints := make(map[string]*AnalyticsGroup)
	buckets := window.chartBuckets()

	for _, request := range s.recent {
		if !window.includesTime(request.Time) {
			continue
		}
		aggregate := aggregateFromRequest(request)
		out.Totals.add(aggregate)
		if bucket := window.bucketFor(request.Time, buckets); bucket != nil {
			bucket.Requests += 1
			bucket.Tokens += request.TotalTokens
			bucket.CostUSD += request.CostUSD
		}
		addRequestGroup(providers, providerGroupKey(request), request, aggregate)
		addRequestGroup(models, modelGroupKey(request), request, aggregate)
		addRequestGroup(accounts, accountGroupKey(request), request, aggregate)
		addRequestGroup(apiKeys, apiKeyGroupKey(request), request, aggregate)
		addRequestGroup(endpoints, endpointGroupKey(request), request, aggregate)
	}

	for i := range buckets {
		out.Series = append(out.Series, buckets[i])
	}
	out.ByProvider = sortedGroups(providers)
	out.ByModel = sortedGroups(models)
	out.ByAccount = sortedGroups(accounts)
	out.ByAPIKey = sortedGroups(apiKeys)
	out.ByEndpoint = sortedGroups(endpoints)
	return out
}

func (s *Store) recentRequestsLocked(window analyticsPeriod) []RecentRequest {
	out := make([]RecentRequest, 0, 20)
	for i := len(s.recent) - 1; i >= 0 && len(out) < 20; i-- {
		request := s.recent[i]
		if !window.includesTime(request.Time) {
			continue
		}
		out = append(out, request)
	}
	return out
}

func (s *Store) activeRequestsLocked(now time.Time) []ActiveRequest {
	out := make([]ActiveRequest, 0, len(s.active))
	for _, request := range s.active {
		request.AgeMs = now.Sub(request.StartedAt).Milliseconds()
		if request.AgeMs < 0 {
			request.AgeMs = 0
		}
		out = append(out, request)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].StartedAt.Before(out[j].StartedAt)
	})
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
	for day := range s.daily {
		parsed, err := time.ParseInLocation("2006-01-02", day, time.Local)
		if err != nil || parsed.Before(cutoff) {
			delete(s.daily, day)
		}
	}
	recent := s.recent[:0]
	for _, request := range s.recent {
		if !request.Time.Before(cutoff.UTC()) {
			recent = append(recent, request)
		}
	}
	s.recent = recent
	if len(s.recent) > maxAnalyticsRecentEvents {
		s.recent = append([]RecentRequest(nil), s.recent[len(s.recent)-maxAnalyticsRecentEvents:]...)
	}
	details := s.details[:0]
	for _, detail := range s.details {
		if !detail.Timestamp.Before(cutoff.UTC()) {
			details = append(details, detail)
		}
	}
	s.details = details
	if len(s.details) > maxRequestDetails {
		s.details = append([]RequestDetail(nil), s.details[len(s.details)-maxRequestDetails:]...)
	}
	s.rebuildDetailIndexesLocked()
	s.pruneActiveLocked(now)
}

func (s *Store) pruneActiveLocked(now time.Time) {
	for id, request := range s.active {
		if now.Sub(request.StartedAt) > activeRequestTTL {
			delete(s.active, id)
		}
	}
}

func newDayUsage() *dayUsage {
	return &dayUsage{
		ByProvider: make(map[string]*AnalyticsGroup),
		ByModel:    make(map[string]*AnalyticsGroup),
		ByAccount:  make(map[string]*AnalyticsGroup),
		ByAPIKey:   make(map[string]*AnalyticsGroup),
		ByEndpoint: make(map[string]*AnalyticsGroup),
	}
}

func (s *Store) appendRequestDetailLocked(detail RequestDetail) {
	if s == nil || detail.ID == "" {
		return
	}
	s.details = append(s.details, detail)
	if len(s.details) > maxRequestDetails {
		s.details = append([]RequestDetail(nil), s.details[len(s.details)-maxRequestDetails:]...)
	}
	s.rebuildDetailIndexesLocked()
}

func (s *Store) rebuildDetailIndexesLocked() {
	if s == nil {
		return
	}
	s.detailIndex = make(map[string]int, len(s.details))
	s.requestDetailsByRequestID = make(map[string][]int)
	for idx, detail := range s.details {
		if detail.ID != "" {
			s.detailIndex[detail.ID] = idx
		}
		if detail.RequestID != "" {
			s.requestDetailsByRequestID[detail.RequestID] = append(s.requestDetailsByRequestID[detail.RequestID], idx)
		}
	}
}

func (s *Store) trimPendingHTTPDetailsLocked() {
	if s == nil || len(s.pendingHTTPDetails) <= maxPendingHTTPDetails {
		return
	}
	for requestID := range s.pendingHTTPDetails {
		delete(s.pendingHTTPDetails, requestID)
		if len(s.pendingHTTPDetails) <= maxPendingHTTPDetails {
			return
		}
	}
}

func requestDetailFromRecord(request RecentRequest, record coreusage.Record, httpDetail HTTPRequestDetail) RequestDetail {
	tokens := normalizeTokens(record.Detail)
	status := "success"
	if request.Failed {
		status = "failed"
	}
	detail := RequestDetail{
		ID:              detailIDForRequest(request),
		Timestamp:       request.Time,
		Provider:        request.Provider,
		Model:           request.Model,
		Alias:           request.Alias,
		AccountLabel:    request.AccountLabel,
		APIKeyLabel:     request.APIKeyLabel,
		Endpoint:        request.Endpoint,
		RequestID:       request.RequestID,
		ReasoningEffort: request.ReasoningEffort,
		Status:          status,
		StatusCode:      request.StatusCode,
		Failed:          request.Failed,
		Latency: DetailLatency{
			TotalMs: request.LatencyMs,
		},
		Tokens:  tokens,
		CostUSD: request.CostUSD,
	}
	applyHTTPRequestDetail(&detail, httpDetail)
	return detail
}

func detailIDForRequest(request RecentRequest) string {
	requestID := strings.TrimSpace(request.RequestID)
	if requestID != "" {
		return requestID + ":" + cleanDetailIDPart(request.Provider) + ":" + cleanDetailIDPart(request.Model)
	}
	return fmt.Sprintf("%d:%s:%s", request.Time.UnixNano(), cleanDetailIDPart(request.Provider), cleanDetailIDPart(request.Model))
}

func cleanDetailIDPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			builder.WriteRune(r)
		default:
			builder.WriteByte('-')
		}
	}
	return builder.String()
}

func applyHTTPRequestDetail(detail *RequestDetail, httpDetail HTTPRequestDetail) {
	if detail == nil || !httpDetail.hasCapturedData() {
		return
	}
	if ttft := ttftFromHTTPDetail(httpDetail); ttft > 0 {
		detail.Latency.TTFTMs = ttft
	}
	if requestPayload := requestPayloadFromHTTPDetail(httpDetail); requestPayload != nil {
		detail.Request = requestPayload
	}
	if providerRequestPayload := bodyPayload(httpDetail.APIRequest); providerRequestPayload != nil {
		detail.ProviderRequest = providerRequestPayload
	}
	if providerResponsePayload := bodyPayload(httpDetail.APIResponse); providerResponsePayload != nil {
		detail.ProviderResponse = providerResponsePayload
	}
	if responsePayload := responsePayloadFromHTTPDetail(httpDetail); responsePayload != nil {
		detail.Response = responsePayload
	}
	if websocketPayload := bodyPayload(httpDetail.WebsocketTimeline); websocketPayload != nil {
		detail.WebsocketTimeline = websocketPayload
	}
	if apiWebsocketPayload := bodyPayload(httpDetail.APIWebsocketTimeline); apiWebsocketPayload != nil {
		detail.ProviderResponse = apiWebsocketPayload
	}
}

func ttftFromHTTPDetail(detail HTTPRequestDetail) int64 {
	if detail.RequestTimestamp.IsZero() || detail.APIResponseTimestamp.IsZero() {
		return 0
	}
	ttft := detail.APIResponseTimestamp.Sub(detail.RequestTimestamp).Milliseconds()
	if ttft < 0 {
		return 0
	}
	return ttft
}

func requestPayloadFromHTTPDetail(detail HTTPRequestDetail) *HTTPMessageDetail {
	if strings.TrimSpace(detail.URL) == "" &&
		strings.TrimSpace(detail.Method) == "" &&
		len(detail.RequestHeaders) == 0 &&
		len(detail.RequestBody) == 0 {
		return nil
	}
	payload := &HTTPMessageDetail{
		URL:     strings.TrimSpace(detail.URL),
		Method:  strings.TrimSpace(detail.Method),
		Headers: sanitizeHeaders(detail.RequestHeaders),
		Body:    bodyPayload(detail.RequestBody),
	}
	if !detail.RequestTimestamp.IsZero() {
		ts := detail.RequestTimestamp.UTC()
		payload.Timestamp = &ts
	}
	return payload
}

func responsePayloadFromHTTPDetail(detail HTTPRequestDetail) *HTTPResponseDetail {
	if detail.StatusCode == 0 &&
		len(detail.ResponseHeaders) == 0 &&
		len(detail.ResponseBody) == 0 {
		return nil
	}
	payload := &HTTPResponseDetail{
		StatusCode: detail.StatusCode,
		Headers:    sanitizeHeaders(detail.ResponseHeaders),
		Body:       bodyPayload(detail.ResponseBody),
	}
	if !detail.APIResponseTimestamp.IsZero() {
		ts := detail.APIResponseTimestamp.UTC()
		payload.Timestamp = &ts
	}
	return payload
}

func bodyPayload(raw []byte) any {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil
	}
	if !utf8.Valid(raw) {
		return truncatedPayload(raw)
	}
	if json.Valid(raw) {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		var parsed any
		if err := decoder.Decode(&parsed); err == nil {
			sanitized := sanitizeJSONValue(parsed)
			if encoded, errMarshal := json.Marshal(sanitized); errMarshal == nil {
				if len(encoded) > maxDetailFieldBytes {
					return truncatedPayload(encoded)
				}
			}
			return sanitized
		}
	}
	if len(raw) > maxDetailFieldBytes {
		return truncatedPayload(raw)
	}
	return string(raw)
}

func truncatedPayload(raw []byte) TruncatedField {
	preview := raw
	if len(preview) > maxDetailFieldBytes {
		preview = preview[:maxDetailFieldBytes]
	}
	return TruncatedField{
		Truncated:    true,
		OriginalSize: len(raw),
		Preview:      string(preview),
	}
}

func sanitizeHeaders(headers map[string][]string) map[string][]string {
	if len(headers) == 0 {
		return nil
	}
	sanitized := make(map[string][]string, len(headers))
	for key, values := range headers {
		cleanKey := strings.TrimSpace(key)
		if cleanKey == "" {
			continue
		}
		copied := make([]string, 0, len(values))
		for _, value := range values {
			if isSensitiveKey(cleanKey) {
				copied = append(copied, "[REDACTED]")
			} else {
				copied = append(copied, util.MaskSensitiveHeaderValue(cleanKey, value))
			}
		}
		sanitized[cleanKey] = copied
	}
	return sanitized
}

func sanitizeJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			if isSensitiveKey(key) {
				out[key] = "[REDACTED]"
				continue
			}
			out[key] = sanitizeJSONValue(child)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for idx, child := range typed {
			out[idx] = sanitizeJSONValue(child)
		}
		return out
	default:
		return value
	}
}

func isSensitiveKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" {
		return false
	}
	key = strings.ReplaceAll(key, "_", "-")
	switch key {
	case "authorization", "cookie", "set-cookie", "api-key", "apikey", "key", "token", "secret", "password", "credential":
		return true
	}
	return strings.Contains(key, "authorization") ||
		strings.Contains(key, "api-key") ||
		strings.Contains(key, "apikey") ||
		strings.Contains(key, "token") ||
		strings.Contains(key, "secret") ||
		strings.Contains(key, "password") ||
		strings.Contains(key, "credential") ||
		strings.Contains(key, "cookie")
}

func mergeHTTPRequestDetails(base, patch HTTPRequestDetail) HTTPRequestDetail {
	if strings.TrimSpace(patch.URL) != "" {
		base.URL = patch.URL
	}
	if strings.TrimSpace(patch.Method) != "" {
		base.Method = patch.Method
	}
	if len(patch.RequestHeaders) > 0 {
		base.RequestHeaders = cloneHeaderMap(patch.RequestHeaders)
	}
	if len(patch.RequestBody) > 0 {
		base.RequestBody = bytes.Clone(patch.RequestBody)
	}
	if patch.StatusCode > 0 {
		base.StatusCode = patch.StatusCode
	}
	if len(patch.ResponseHeaders) > 0 {
		base.ResponseHeaders = cloneHeaderMap(patch.ResponseHeaders)
	}
	if len(patch.ResponseBody) > 0 {
		base.ResponseBody = bytes.Clone(patch.ResponseBody)
	}
	if len(patch.WebsocketTimeline) > 0 {
		base.WebsocketTimeline = bytes.Clone(patch.WebsocketTimeline)
	}
	if len(patch.APIRequest) > 0 {
		base.APIRequest = bytes.Clone(patch.APIRequest)
	}
	if len(patch.APIResponse) > 0 {
		base.APIResponse = bytes.Clone(patch.APIResponse)
	}
	if len(patch.APIWebsocketTimeline) > 0 {
		base.APIWebsocketTimeline = bytes.Clone(patch.APIWebsocketTimeline)
	}
	if strings.TrimSpace(patch.RequestID) != "" {
		base.RequestID = patch.RequestID
	}
	if !patch.RequestTimestamp.IsZero() {
		base.RequestTimestamp = patch.RequestTimestamp
	}
	if !patch.APIResponseTimestamp.IsZero() {
		base.APIResponseTimestamp = patch.APIResponseTimestamp
	}
	return base
}

func (detail HTTPRequestDetail) clone() HTTPRequestDetail {
	return HTTPRequestDetail{
		URL:                  detail.URL,
		Method:               detail.Method,
		RequestHeaders:       cloneHeaderMap(detail.RequestHeaders),
		RequestBody:          bytes.Clone(detail.RequestBody),
		StatusCode:           detail.StatusCode,
		ResponseHeaders:      cloneHeaderMap(detail.ResponseHeaders),
		ResponseBody:         bytes.Clone(detail.ResponseBody),
		WebsocketTimeline:    bytes.Clone(detail.WebsocketTimeline),
		APIRequest:           bytes.Clone(detail.APIRequest),
		APIResponse:          bytes.Clone(detail.APIResponse),
		APIWebsocketTimeline: bytes.Clone(detail.APIWebsocketTimeline),
		RequestID:            detail.RequestID,
		RequestTimestamp:     detail.RequestTimestamp,
		APIResponseTimestamp: detail.APIResponseTimestamp,
	}
}

func (detail HTTPRequestDetail) hasCapturedData() bool {
	return strings.TrimSpace(detail.URL) != "" ||
		strings.TrimSpace(detail.Method) != "" ||
		len(detail.RequestHeaders) > 0 ||
		len(detail.RequestBody) > 0 ||
		detail.StatusCode > 0 ||
		len(detail.ResponseHeaders) > 0 ||
		len(detail.ResponseBody) > 0 ||
		len(detail.WebsocketTimeline) > 0 ||
		len(detail.APIRequest) > 0 ||
		len(detail.APIResponse) > 0 ||
		len(detail.APIWebsocketTimeline) > 0 ||
		strings.TrimSpace(detail.RequestID) != ""
}

func cloneHeaderMap(src map[string][]string) map[string][]string {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string][]string, len(src))
	for key, values := range src {
		dst[key] = append([]string(nil), values...)
	}
	return dst
}

func (d *dayUsage) add(request RecentRequest, aggregate Aggregate) {
	if d == nil {
		return
	}
	d.Aggregate.add(aggregate)
	addRequestGroup(d.ByProvider, providerGroupKey(request), request, aggregate)
	addRequestGroup(d.ByModel, modelGroupKey(request), request, aggregate)
	addRequestGroup(d.ByAccount, accountGroupKey(request), request, aggregate)
	addRequestGroup(d.ByAPIKey, apiKeyGroupKey(request), request, aggregate)
	addRequestGroup(d.ByEndpoint, endpointGroupKey(request), request, aggregate)
}

func requestFromRecord(ctx context.Context, record coreusage.Record) (RecentRequest, Aggregate) {
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
	provider := cleanText(record.Provider, "unknown")
	model := cleanText(record.Model, "unknown")
	cost := estimateCostUSD(provider, model, tokens)
	apiKey := strings.TrimSpace(record.APIKey)
	source := strings.TrimSpace(record.Source)
	accountLabel := source
	if accountLabel == "" {
		accountLabel = strings.TrimSpace(record.AuthIndex)
	}
	if accountLabel == "" {
		accountLabel = strings.TrimSpace(record.AuthID)
	}
	if accountLabel == "" {
		accountLabel = "local / no account"
	}

	request := RecentRequest{
		Time:            timestamp.UTC(),
		Provider:        provider,
		Model:           model,
		Alias:           strings.TrimSpace(record.Alias),
		Source:          source,
		AccountLabel:    accountLabel,
		APIKeyLabel:     MaskAPIKey(apiKey),
		AuthIndex:       strings.TrimSpace(record.AuthIndex),
		AuthType:        strings.TrimSpace(record.AuthType),
		Endpoint:        strings.TrimSpace(internallogging.GetEndpoint(ctx)),
		RequestID:       strings.TrimSpace(internallogging.GetRequestID(ctx)),
		ReasoningEffort: strings.TrimSpace(record.ReasoningEffort),
		InputTokens:     tokens.InputTokens,
		OutputTokens:    tokens.OutputTokens,
		ReasoningTokens: tokens.ReasoningTokens,
		CachedTokens:    tokens.CachedTokens,
		TotalTokens:     tokens.TotalTokens,
		CostUSD:         cost,
		LatencyMs:       record.Latency.Milliseconds(),
		StatusCode:      statusCode,
		Failed:          failed,
	}
	if request.Alias == "" {
		request.Alias = model
	}
	if request.Endpoint == "" {
		request.Endpoint = "unknown"
	}
	if request.AuthType == "" {
		request.AuthType = "unknown"
	}

	aggregate := Aggregate{
		Requests: 1,
		Tokens:   tokens,
		CostUSD:  cost,
	}
	if failed {
		aggregate.Failed = 1
	} else {
		aggregate.Success = 1
	}
	return request, aggregate
}

func aggregateFromRequest(request RecentRequest) Aggregate {
	tokens := tokenUsage{
		InputTokens:     request.InputTokens,
		OutputTokens:    request.OutputTokens,
		ReasoningTokens: request.ReasoningTokens,
		CachedTokens:    request.CachedTokens,
		TotalTokens:     request.TotalTokens,
	}
	aggregate := Aggregate{Requests: 1, Tokens: tokens, CostUSD: request.CostUSD}
	if request.Failed {
		aggregate.Failed = 1
	} else {
		aggregate.Success = 1
	}
	return aggregate
}

func aggregateFromDetail(detail RequestDetail) Aggregate {
	aggregate := Aggregate{Requests: 1, Tokens: detail.Tokens, CostUSD: detail.CostUSD}
	if detail.Failed {
		aggregate.Failed = 1
	} else {
		aggregate.Success = 1
	}
	return aggregate
}

func addRequestGroup(groups map[string]*AnalyticsGroup, key string, request RecentRequest, aggregate Aggregate) {
	if groups == nil || key == "" {
		return
	}
	group := groups[key]
	if group == nil {
		group = &AnalyticsGroup{
			Key:          key,
			Provider:     request.Provider,
			Model:        request.Model,
			Alias:        request.Alias,
			AccountLabel: request.AccountLabel,
			APIKeyLabel:  request.APIKeyLabel,
			Endpoint:     request.Endpoint,
		}
		groups[key] = group
	}
	group.Requests += aggregate.Requests
	group.Success += aggregate.Success
	group.Failed += aggregate.Failed
	group.InputTokens += aggregate.Tokens.InputTokens
	group.OutputTokens += aggregate.Tokens.OutputTokens
	group.ReasoningTokens += aggregate.Tokens.ReasoningTokens
	group.CachedTokens += aggregate.Tokens.CachedTokens
	group.TotalTokens += aggregate.Tokens.TotalTokens
	group.CostUSD += aggregate.CostUSD
	if group.LastUsed == nil || request.Time.After(*group.LastUsed) {
		updated := request.Time
		group.LastUsed = &updated
	}
}

func mergeGroupMap(dst map[string]*AnalyticsGroup, src map[string]*AnalyticsGroup) {
	for key, group := range src {
		if group == nil {
			continue
		}
		target := dst[key]
		if target == nil {
			copied := *group
			dst[key] = &copied
			continue
		}
		target.Requests += group.Requests
		target.Success += group.Success
		target.Failed += group.Failed
		target.InputTokens += group.InputTokens
		target.OutputTokens += group.OutputTokens
		target.ReasoningTokens += group.ReasoningTokens
		target.CachedTokens += group.CachedTokens
		target.TotalTokens += group.TotalTokens
		target.CostUSD += group.CostUSD
		if group.LastUsed != nil && (target.LastUsed == nil || group.LastUsed.After(*target.LastUsed)) {
			updated := *group.LastUsed
			target.LastUsed = &updated
		}
	}
}

func sortedGroups(groups map[string]*AnalyticsGroup) []AnalyticsGroup {
	out := make([]AnalyticsGroup, 0, len(groups))
	for _, group := range groups {
		if group == nil {
			continue
		}
		out = append(out, *group)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TotalTokens != out[j].TotalTokens {
			return out[i].TotalTokens > out[j].TotalTokens
		}
		if out[i].Requests != out[j].Requests {
			return out[i].Requests > out[j].Requests
		}
		return out[i].Key < out[j].Key
	})
	return out
}

func providerGroupKey(request RecentRequest) string {
	return cleanText(request.Provider, "unknown")
}

func modelGroupKey(request RecentRequest) string {
	return cleanText(request.Model, "unknown") + " (" + cleanText(request.Provider, "unknown") + ")"
}

func accountGroupKey(request RecentRequest) string {
	return cleanText(request.Provider, "unknown") + " / " + cleanText(request.AccountLabel, "local / no account")
}

func apiKeyGroupKey(request RecentRequest) string {
	if strings.TrimSpace(request.APIKeyLabel) == "" {
		return "local / no API key"
	}
	return request.APIKeyLabel
}

func endpointGroupKey(request RecentRequest) string {
	return cleanText(request.Endpoint, "unknown")
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
	a.CostUSD += other.CostUSD
}

func normalizeWindowDays(days int) int {
	switch days {
	case 1, 7, 30, 60:
		return days
	default:
		return 7
	}
}

type analyticsPeriod struct {
	period      string
	start       time.Time
	end         time.Time
	hourly      bool
	bucketCount int
	days        []string
}

func normalizeAnalyticsPeriod(period string, now time.Time) analyticsPeriod {
	period = strings.ToLower(strings.TrimSpace(period))
	if period == "" {
		period = "today"
	}
	end := now.UTC()
	switch period {
	case "today":
		start := startOfLocalDay(now).UTC()
		return analyticsPeriod{period: period, start: start, end: end, hourly: true, bucketCount: 24}
	case "24h":
		return analyticsPeriod{period: period, start: end.Add(-24 * time.Hour), end: end, hourly: true, bucketCount: 24}
	case "7d", "30d", "60d":
		days := 7
		if period == "30d" {
			days = 30
		} else if period == "60d" {
			days = 60
		}
		today := startOfLocalDay(now)
		start := today.AddDate(0, 0, -(days - 1))
		dayKeys := make([]string, 0, days)
		for i := 0; i < days; i++ {
			dayKeys = append(dayKeys, start.AddDate(0, 0, i).Format("2006-01-02"))
		}
		return analyticsPeriod{period: period, start: start.UTC(), end: end, bucketCount: days, days: dayKeys}
	case "all":
		today := startOfLocalDay(now)
		start := today.AddDate(0, 0, -(retentionDays - 1))
		dayKeys := make([]string, 0, retentionDays)
		for i := 0; i < retentionDays; i++ {
			dayKeys = append(dayKeys, start.AddDate(0, 0, i).Format("2006-01-02"))
		}
		return analyticsPeriod{period: period, start: start.UTC(), end: end, bucketCount: retentionDays, days: dayKeys}
	default:
		return normalizeAnalyticsPeriod("today", now)
	}
}

func (w analyticsPeriod) includesTime(t time.Time) bool {
	if t.IsZero() {
		return false
	}
	t = t.UTC()
	return !t.Before(w.start) && !t.After(w.end)
}

func (w analyticsPeriod) includesDay(day string) bool {
	if len(w.days) == 0 {
		return true
	}
	for _, candidate := range w.days {
		if candidate == day {
			return true
		}
	}
	return false
}

func (w analyticsPeriod) chartBuckets() []ChartPoint {
	if !w.hourly {
		return nil
	}
	buckets := make([]ChartPoint, 0, w.bucketCount)
	step := time.Hour
	start := w.start
	if w.period == "24h" {
		start = w.end.Truncate(time.Hour).Add(-23 * time.Hour)
	}
	for i := 0; i < w.bucketCount; i++ {
		ts := start.Add(time.Duration(i) * step).In(time.Local)
		buckets = append(buckets, ChartPoint{Label: ts.Format("15:04")})
	}
	return buckets
}

func (w analyticsPeriod) bucketFor(t time.Time, buckets []ChartPoint) *ChartPoint {
	if len(buckets) == 0 {
		return nil
	}
	local := t.In(time.Local)
	idx := local.Hour()
	if w.period == "24h" {
		start := w.end.Truncate(time.Hour).Add(-23 * time.Hour)
		idx = int(t.UTC().Truncate(time.Hour).Sub(start).Hours())
	}
	if idx < 0 || idx >= len(buckets) {
		return nil
	}
	return &buckets[idx]
}

func sortedDayKeys(days map[string]*dayUsage) []string {
	keys := make([]string, 0, len(days))
	for day := range days {
		keys = append(keys, day)
	}
	sort.Strings(keys)
	return keys
}

func shortDateLabel(day string) string {
	parsed, err := time.ParseInLocation("2006-01-02", day, time.Local)
	if err != nil {
		return day
	}
	return parsed.Format("Jan 02")
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

func (f *RequestDetailsFilter) normalize() {
	if f.Page <= 0 {
		f.Page = 1
	}
	if f.PageSize <= 0 {
		f.PageSize = 20
	}
	if f.PageSize > 100 {
		f.PageSize = 100
	}
	f.Provider = strings.TrimSpace(f.Provider)
	f.Model = strings.TrimSpace(f.Model)
	f.APIKey = strings.TrimSpace(f.APIKey)
	f.Endpoint = strings.TrimSpace(f.Endpoint)
	f.Status = strings.ToLower(strings.TrimSpace(f.Status))
}

func (f RequestDetailsFilter) matches(request RecentRequest) bool {
	if f.Provider != "" && !strings.EqualFold(request.Provider, f.Provider) {
		return false
	}
	if f.Model != "" && !strings.Contains(strings.ToLower(request.Model), strings.ToLower(f.Model)) {
		return false
	}
	if f.APIKey != "" && request.APIKeyLabel != MaskAPIKey(f.APIKey) && !strings.EqualFold(request.APIKeyLabel, f.APIKey) {
		return false
	}
	if f.Endpoint != "" && !strings.Contains(strings.ToLower(request.Endpoint), strings.ToLower(f.Endpoint)) {
		return false
	}
	if f.Status != "" {
		wantFailed := f.Status == "failed" || f.Status == "error"
		wantOK := f.Status == "ok" || f.Status == "success"
		if wantFailed && !request.Failed {
			return false
		}
		if wantOK && request.Failed {
			return false
		}
	}
	if !f.StartTime.IsZero() && request.Time.Before(f.StartTime) {
		return false
	}
	if !f.EndTime.IsZero() && request.Time.After(f.EndTime) {
		return false
	}
	return true
}

func (f RequestDetailsFilter) matchesDetail(detail RequestDetail) bool {
	return f.matches(RecentRequest{
		Time:        detail.Timestamp,
		Provider:    detail.Provider,
		Model:       detail.Model,
		APIKeyLabel: detail.APIKeyLabel,
		Endpoint:    detail.Endpoint,
		Failed:      detail.Failed,
	})
}

type modelPricing struct {
	Input         float64
	Output        float64
	Cached        float64
	Reasoning     float64
	CacheCreation float64
}

func estimateCostUSD(provider, model string, tokens tokenUsage) float64 {
	pricing := pricingForModel(provider, model)
	input := tokens.InputTokens
	cached := tokens.CachedTokens
	if cached == 0 {
		cached = tokens.CacheReadTokens
	}
	nonCachedInput := input - cached
	if nonCachedInput < 0 {
		nonCachedInput = 0
	}
	cost := float64(nonCachedInput)*(pricing.Input/1_000_000) +
		float64(cached)*(pricing.Cached/1_000_000) +
		float64(tokens.OutputTokens)*(pricing.Output/1_000_000) +
		float64(tokens.ReasoningTokens)*(pricing.Reasoning/1_000_000) +
		float64(tokens.CacheCreationTokens)*(pricing.CacheCreation/1_000_000)
	return cost
}

func pricingForModel(provider, model string) modelPricing {
	value := strings.ToLower(strings.TrimSpace(model))
	provider = strings.ToLower(strings.TrimSpace(provider))
	switch {
	case strings.Contains(value, "claude-opus"):
		return modelPricing{Input: 5, Output: 25, Cached: 0.5, Reasoning: 25, CacheCreation: 6.25}
	case strings.Contains(value, "claude-haiku"):
		return modelPricing{Input: 1, Output: 5, Cached: 0.1, Reasoning: 5, CacheCreation: 1.25}
	case strings.Contains(value, "claude") || provider == "claude":
		return modelPricing{Input: 3, Output: 15, Cached: 0.3, Reasoning: 15, CacheCreation: 3.75}
	case strings.Contains(value, "gemini") || provider == "gemini":
		if strings.Contains(value, "flash") {
			return modelPricing{Input: 0.3, Output: 2.5, Cached: 0.03, Reasoning: 3.75, CacheCreation: 0.3}
		}
		return modelPricing{Input: 2, Output: 12, Cached: 0.25, Reasoning: 18, CacheCreation: 2}
	case strings.Contains(value, "deepseek"):
		return modelPricing{Input: 0.14, Output: 0.28, Cached: 0.0028, Reasoning: 0.28, CacheCreation: 0.14}
	case strings.Contains(value, "qwen"):
		return modelPricing{Input: 0.5, Output: 2, Cached: 0.25, Reasoning: 3, CacheCreation: 0.5}
	case strings.Contains(value, "gpt-5") || strings.Contains(value, "codex") || provider == "codex" || provider == "openai":
		return modelPricing{Input: 3, Output: 12, Cached: 1.5, Reasoning: 18, CacheCreation: 3}
	default:
		return modelPricing{Input: 1, Output: 4, Cached: 0.5, Reasoning: 6, CacheCreation: 1}
	}
}

func ParsePositiveInt(value string, fallback int) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
