package prometheus

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
	statusNotConnected = "Not connected to Prometheus"
	statusConnectedFmt = "Connected to Prometheus at %s"
)

var (
	// ErrNotConnected indicates the adapter is not connected.
	ErrNotConnected = errors.New("adapter not connected to Prometheus")
)

// QueryResult holds the result of a PromQL query.
type QueryResult struct {
	ResultType string   `json:"result_type"` // "vector", "matrix", "scalar", "string"
	Vector     []Sample `json:"vector,omitempty"`
	Matrix     []Series `json:"matrix,omitempty"`
}

// Sample is a single instant vector sample.
type Sample struct {
	Metric    map[string]string `json:"metric"`
	Timestamp float64           `json:"timestamp"`
	Value     string            `json:"value"`
}

// Series is a range vector series (for matrix results).
type Series struct {
	Metric map[string]string `json:"metric"`
	Values [][]any           `json:"values"` // [[timestamp, "value"], ...]
}

// Target is a Prometheus scrape target.
type Target struct {
	Labels     map[string]string `json:"labels"`
	State      string            `json:"state"` // "active", "dropped"
	ScrapeURL  string            `json:"scrape_url,omitempty"`
	LastError  string            `json:"last_error,omitempty"`
	LastScrape string            `json:"last_scrape,omitempty"`
}

// PrometheusAdapter extends the base Adapter with Prometheus-specific operations.
type PrometheusAdapter interface {
	adapters.Adapter
	// Query executes an instant PromQL query.
	Query(ctx context.Context, query string, queryTime time.Time) (*QueryResult, error)
	// QueryRange executes a range PromQL query.
	QueryRange(ctx context.Context, query string, start, end time.Time, step time.Duration) (*QueryResult, error)
	// Targets returns the list of scrape targets (active + dropped).
	Targets(ctx context.Context) ([]Target, error)
}

// httpDoer abstracts net/http.Client for testing.
type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Adapter is the concrete Prometheus/Mimir adapter.
type Adapter struct {
	mu        sync.RWMutex
	config    Config
	client    httpDoer
	connected bool
}

// New creates a new Prometheus adapter (not yet connected).
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

// Connect establishes and verifies connectivity to Prometheus/Mimir.
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

	// Verify connectivity via buildinfo endpoint.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimRight(cfg.URL, "/")+"/api/v1/status/buildinfo", nil)
	if err != nil {
		return fmt.Errorf("build health-check request: %w", err)
	}
	a.addHeaders(req, cfg)

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("connect to Prometheus at %s: %w", cfg.URL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("prometheus health check failed (status %d): %s", resp.StatusCode, string(body))
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

// Query executes an instant PromQL query.
func (a *Adapter) Query(ctx context.Context, query string, queryTime time.Time) (*QueryResult, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	u := strings.TrimRight(a.config.URL, "/") + "/api/v1/query"
	params := url.Values{}
	params.Set("query", query)
	if !queryTime.IsZero() {
		params.Set("time", fmt.Sprintf("%d", queryTime.Unix()))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u+"?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("build query request: %w", err)
	}
	a.addHeaders(req, a.config)

	return a.execQuery(req)
}

// QueryRange executes a range PromQL query.
func (a *Adapter) QueryRange(ctx context.Context, query string, start, end time.Time, step time.Duration) (*QueryResult, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	u := strings.TrimRight(a.config.URL, "/") + "/api/v1/query_range"
	params := url.Values{}
	params.Set("query", query)
	params.Set("start", fmt.Sprintf("%d", start.Unix()))
	params.Set("end", fmt.Sprintf("%d", end.Unix()))
	stepSec := int64(step.Seconds())
	if stepSec < 1 {
		stepSec = 15
	}
	params.Set("step", fmt.Sprintf("%d", stepSec))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u+"?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("build query_range request: %w", err)
	}
	a.addHeaders(req, a.config)

	return a.execQuery(req)
}

// Targets returns the current scrape targets.
func (a *Adapter) Targets(ctx context.Context) ([]Target, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	u := strings.TrimRight(a.config.URL, "/") + "/api/v1/targets"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build targets request: %w", err)
	}
	a.addHeaders(req, a.config)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("targets request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read targets response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("targets request failed (status %d): %s", resp.StatusCode, string(body))
	}

	var raw struct {
		Status string `json:"status"`
		Data   struct {
			ActiveTargets  []rawTarget `json:"activeTargets"`
			DroppedTargets []rawTarget `json:"droppedTargets"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse targets response: %w", err)
	}

	var targets []Target
	for _, t := range raw.Data.ActiveTargets {
		targets = append(targets, Target{
			Labels:     t.Labels,
			State:      "active",
			ScrapeURL:  t.ScrapeURL,
			LastError:  t.LastError,
			LastScrape: t.LastScrape,
		})
	}
	for _, t := range raw.Data.DroppedTargets {
		targets = append(targets, Target{
			Labels: t.DiscoveredLabels,
			State:  "dropped",
		})
	}

	return targets, nil
}

type rawTarget struct {
	Labels           map[string]string `json:"labels"`
	DiscoveredLabels map[string]string `json:"discoveredLabels"`
	ScrapeURL        string            `json:"scrapeUrl"`
	LastError        string            `json:"lastError"`
	LastScrape       string            `json:"lastScrape"`
}

// execQuery handles the shared HTTP call + response parsing for query/query_range.
func (a *Adapter) execQuery(req *http.Request) (*QueryResult, error) {
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("query request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read query response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("query failed (status %d): %s", resp.StatusCode, string(body))
	}

	var raw struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string          `json:"resultType"`
			Result     json.RawMessage `json:"result"`
		} `json:"data"`
		Error     string `json:"error,omitempty"`
		ErrorType string `json:"errorType,omitempty"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse query response: %w", err)
	}

	if raw.Status != "success" {
		return nil, fmt.Errorf("prometheus query error (%s): %s", raw.ErrorType, raw.Error)
	}

	result := &QueryResult{ResultType: raw.Data.ResultType}

	switch raw.Data.ResultType {
	case "vector":
		var samples []struct {
			Metric map[string]string `json:"metric"`
			Value  []any             `json:"value"` // [timestamp, "value"]
		}
		if err := json.Unmarshal(raw.Data.Result, &samples); err != nil {
			return nil, fmt.Errorf("parse vector result: %w", err)
		}
		for _, s := range samples {
			ts, _ := s.Value[0].(float64)
			val, _ := s.Value[1].(string)
			result.Vector = append(result.Vector, Sample{
				Metric:    s.Metric,
				Timestamp: ts,
				Value:     val,
			})
		}

	case "matrix":
		var series []struct {
			Metric map[string]string `json:"metric"`
			Values [][]any           `json:"values"`
		}
		if err := json.Unmarshal(raw.Data.Result, &series); err != nil {
			return nil, fmt.Errorf("parse matrix result: %w", err)
		}
		for _, s := range series {
			result.Matrix = append(result.Matrix, Series{
				Metric: s.Metric,
				Values: s.Values,
			})
		}
	}

	return result, nil
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
