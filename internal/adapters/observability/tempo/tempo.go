package tempo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/credential"
	"github.com/jaimegago/joe/internal/store"
)

const (
	statusNotConnected = "Not connected to Tempo"
	statusConnectedFmt = "Connected to Tempo at %s"

	defaultSearchLimit = 20
)

var (
	// ErrNotConnected indicates the adapter is not connected.
	ErrNotConnected = errors.New("adapter not connected to Tempo")
	// ErrTraceNotFound indicates a trace lookup failed.
	ErrTraceNotFound = errors.New("trace not found")
)

// TraceSearchResult is a summary of a trace returned by search.
type TraceSearchResult struct {
	TraceID           string            `json:"trace_id"`
	RootServiceName   string            `json:"root_service_name,omitempty"`
	RootTraceName     string            `json:"root_trace_name,omitempty"`
	StartTimeUnixNano string            `json:"start_time_unix_nano,omitempty"`
	DurationMs        float64           `json:"duration_ms,omitempty"`
	SpanCount         int               `json:"span_count,omitempty"`
	Attributes        map[string]string `json:"attributes,omitempty"`
}

// Trace is the full trace data returned by GetTrace.
type Trace struct {
	TraceID   string `json:"trace_id"`
	Batches   []any  `json:"batches,omitempty"` // raw OTLP batches
	SpanCount int    `json:"span_count,omitempty"`
}

// TempoAdapter extends the base Adapter with Tempo-specific operations.
type TempoAdapter interface {
	adapters.Adapter
	// Search searches for traces matching the given criteria.
	Search(ctx context.Context, service, tags string, minDurationMs, maxDurationMs int, limit int) ([]TraceSearchResult, error)
	// GetTrace retrieves a full trace by ID.
	GetTrace(ctx context.Context, traceID string) (*Trace, error)
	// ListServices returns service names discovered from Tempo for traces_in edge discovery.
	ListServices(ctx context.Context) ([]string, error)
}

// httpDoer abstracts net/http.Client for testing.
type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Adapter is the concrete Tempo adapter.
type Adapter struct {
	mu        sync.RWMutex
	config    Config
	client    httpDoer
	connected bool
}

// New creates a new Tempo adapter (not yet connected).
func New() *Adapter {
	return &Adapter{
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// NewWithClient creates an adapter with a custom HTTP client (for testing).
func NewWithClient(client httpDoer) *Adapter {
	return &Adapter{
		client:    client,
		connected: true,
	}
}

// Connect establishes and verifies connectivity to Tempo.
func (a *Adapter) Connect(ctx context.Context, source store.Component) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	var configMap map[string]any
	if len(source.Config) > 0 {
		if err := json.Unmarshal(source.Config, &configMap); err != nil {
			return fmt.Errorf("parse source config JSON: %w", err)
		}
	} else {
		configMap = make(map[string]any)
	}

	cfg, err := ParseConfig(configMap)
	if err != nil {
		return fmt.Errorf("parse source config: %w", err)
	}

	// A003-W2: resolve the credential through the provider selected by the
	// component config (default static). The resolved static value overrides the
	// parsed api_key; an empty value leaves the legacy inline token intact, so a
	// component carrying an inline api_key keeps working.
	provider, err := credential.Select(source.Config)
	if err != nil {
		return fmt.Errorf("select credential provider: %w", err)
	}
	res, err := provider.Resolve(ctx, source.ID, source.Config)
	if err != nil {
		return fmt.Errorf("resolve credential: %w", err)
	}
	if !res.Diagnostic.OK {
		return fmt.Errorf("resolve credential: %s", res.Diagnostic.Reason)
	}
	if v, ok := res.StaticValue(); ok && v != "" {
		cfg.APIKey = v
	}

	a.config = cfg

	// Verify connectivity via the status endpoint.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimRight(cfg.URL, "/")+"/api/status", nil)
	if err != nil {
		return fmt.Errorf("build health-check request: %w", err)
	}
	a.addHeaders(req, cfg)

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("connect to Tempo at %s: %w", cfg.URL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("tempo health check failed (status %d): %s", resp.StatusCode, string(body))
	}

	a.connected = true
	return nil
}

// Disconnect closes the connection.
func (a *Adapter) Disconnect() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.connected = false
	return nil
}

// Status returns the current connection status.
func (a *Adapter) Status() adapters.Status {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.connected {
		return adapters.Status{
			Connected: true,
			Message:   fmt.Sprintf(statusConnectedFmt, a.config.URL),
		}
	}

	return adapters.Status{
		Connected: false,
		Message:   statusNotConnected,
	}
}

// Search searches Tempo for traces matching the given criteria.
func (a *Adapter) Search(ctx context.Context, service, tags string, minDurationMs, maxDurationMs int, limit int) ([]TraceSearchResult, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	if limit <= 0 {
		limit = defaultSearchLimit
	}

	u := strings.TrimRight(a.config.URL, "/") + "/api/search"
	params := url.Values{}
	params.Set("limit", fmt.Sprintf("%d", limit))
	if service != "" {
		params.Set("tags", "service.name="+service)
		if tags != "" {
			params.Set("tags", "service.name="+service+" "+tags)
		}
	} else if tags != "" {
		params.Set("tags", tags)
	}
	if minDurationMs > 0 {
		params.Set("minDuration", fmt.Sprintf("%dms", minDurationMs))
	}
	if maxDurationMs > 0 {
		params.Set("maxDuration", fmt.Sprintf("%dms", maxDurationMs))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u+"?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("build search request: %w", err)
	}
	a.addHeaders(req, a.config)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read search response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search failed (status %d): %s", resp.StatusCode, string(body))
	}

	var raw struct {
		Traces []struct {
			TraceID           string            `json:"traceID"`
			RootServiceName   string            `json:"rootServiceName"`
			RootTraceName     string            `json:"rootTraceName"`
			StartTimeUnixNano string            `json:"startTimeUnixNano"`
			DurationMs        float64           `json:"durationMs"`
			SpanCount         int               `json:"spanCount"`
			Attributes        map[string]string `json:"spanSet"`
		} `json:"traces"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse search response: %w", err)
	}

	results := make([]TraceSearchResult, 0, len(raw.Traces))
	for _, t := range raw.Traces {
		results = append(results, TraceSearchResult{
			TraceID:           t.TraceID,
			RootServiceName:   t.RootServiceName,
			RootTraceName:     t.RootTraceName,
			StartTimeUnixNano: t.StartTimeUnixNano,
			DurationMs:        t.DurationMs,
			SpanCount:         t.SpanCount,
		})
	}

	return results, nil
}

// ListServices returns service names seen in Tempo by querying the service.name tag values.
// These are used to discover which services send traces to this Tempo instance.
func (a *Adapter) ListServices(ctx context.Context) ([]string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	u := strings.TrimRight(a.config.URL, "/") + "/api/search/tag/service.name/values"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build tag-values request: %w", err)
	}
	a.addHeaders(req, a.config)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tag-values request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read tag-values response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tag-values failed (status %d): %s", resp.StatusCode, string(body))
	}

	var raw struct {
		TagValues []struct {
			Value string `json:"value"`
		} `json:"tagValues"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse tag-values response: %w", err)
	}

	services := make([]string, 0, len(raw.TagValues))
	for _, tv := range raw.TagValues {
		if tv.Value != "" {
			services = append(services, tv.Value)
		}
	}
	return services, nil
}

// GetTrace retrieves a full trace by ID.
func (a *Adapter) GetTrace(ctx context.Context, traceID string) (*Trace, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	u := strings.TrimRight(a.config.URL, "/") + "/api/traces/" + url.PathEscape(traceID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build get-trace request: %w", err)
	}
	a.addHeaders(req, a.config)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get trace request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%w: %s", ErrTraceNotFound, traceID)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read trace response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get trace failed (status %d): %s", resp.StatusCode, string(body))
	}

	// Raw OTLP JSON - return as-is with trace ID.
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse trace response: %w", err)
	}

	batches, _ := raw["batches"].([]any)
	return &Trace{
		TraceID: traceID,
		Batches: batches,
	}, nil
}

// addHeaders sets authentication and multi-tenancy headers.
func (a *Adapter) addHeaders(req *http.Request, cfg Config) {
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	if cfg.OrgID != "" {
		req.Header.Set("X-Scope-OrgID", cfg.OrgID)
	}
}

func (a *Adapter) checkConnected() error {
	if !a.connected {
		return ErrNotConnected
	}
	return nil
}
