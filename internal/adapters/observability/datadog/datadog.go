package datadog

import (
	"bytes"
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
	"github.com/jaimegago/joe/internal/store"
)

const (
	statusNotConnected = "Not connected to Datadog"
	statusConnectedFmt = "Connected to Datadog (%s)"
)

var ErrNotConnected = errors.New("adapter not connected to Datadog")

// MetricSeries is one time-series in a Datadog metrics query response.
type MetricSeries struct {
	Metric     string      `json:"metric"`
	Expression string      `json:"expression,omitempty"`
	Scope      string      `json:"scope,omitempty"`
	Tags       []string    `json:"tags,omitempty"`
	Points     [][]float64 `json:"points"` // [[unix_ms, value], ...]
}

// MetricsResult holds the result of a Datadog metrics query.
type MetricsResult struct {
	Query  string         `json:"query"`
	From   int64          `json:"from"`
	To     int64          `json:"to"`
	Series []MetricSeries `json:"series"`
}

// LogEntry is a single log event returned from Datadog.
type LogEntry struct {
	ID         string            `json:"id"`
	Timestamp  string            `json:"timestamp"`
	Host       string            `json:"host"`
	Service    string            `json:"service"`
	Status     string            `json:"status"`
	Message    string            `json:"message"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// LogsResult holds the result of a Datadog logs search.
type LogsResult struct {
	Logs  []LogEntry `json:"logs"`
	Count int        `json:"count"`
}

// DatadogAdapter extends the base Adapter with Datadog-specific operations.
type DatadogAdapter interface {
	adapters.Adapter
	// MetricsQuery executes a Datadog metrics query over a time range.
	// from and to are Unix timestamps in seconds.
	MetricsQuery(ctx context.Context, query string, from, to int64) (*MetricsResult, error)
	// LogsSearch searches Datadog log events.
	// from and to are Unix timestamps in seconds.
	LogsSearch(ctx context.Context, query string, from, to int64, limit int) (*LogsResult, error)
	// ListActiveServices returns distinct service names from active Datadog hosts.
	// Used for metrics_in edge discovery during graph refresh.
	ListActiveServices(ctx context.Context) ([]string, error)
	// ListLogServices returns distinct service names from recent Datadog log events.
	// Used for logs_in edge discovery during graph refresh.
	ListLogServices(ctx context.Context) ([]string, error)
}

// httpDoer abstracts net/http.Client for testing.
type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Adapter is the concrete Datadog adapter.
type Adapter struct {
	mu        sync.RWMutex
	config    Config
	client    httpDoer
	connected bool
}

// New creates a new Datadog adapter (not yet connected).
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

// Connect establishes and verifies connectivity to Datadog.
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
	a.config = cfg

	// Verify connectivity via API key validation endpoint.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		cfg.BaseURL()+"/api/v1/validate", nil)
	if err != nil {
		return fmt.Errorf("build health-check request: %w", err)
	}
	a.addHeaders(req, cfg)

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("connect to Datadog at %s: %w", cfg.BaseURL(), err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("datadog validation failed (status %d): %s", resp.StatusCode, string(body))
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
			Message:   fmt.Sprintf(statusConnectedFmt, a.config.Site),
		}
	}
	return adapters.Status{Connected: false, Message: statusNotConnected}
}

// MetricsQuery executes a Datadog metrics query.
// from and to are Unix timestamps in seconds.
func (a *Adapter) MetricsQuery(ctx context.Context, query string, from, to int64) (*MetricsResult, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	params := url.Values{}
	params.Set("query", query)
	params.Set("from", fmt.Sprintf("%d", from))
	params.Set("to", fmt.Sprintf("%d", to))

	u := a.config.BaseURL() + "/api/v1/query?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build metrics query request: %w", err)
	}
	a.addHeaders(req, a.config)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("metrics query request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read metrics response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("metrics query failed (status %d): %s", resp.StatusCode, string(body))
	}

	var raw struct {
		Status string `json:"status"`
		Series []struct {
			Metric     string      `json:"metric"`
			Expression string      `json:"expression"`
			Scope      string      `json:"scope"`
			TagSet     []string    `json:"tag_set"`
			Pointlist  [][]float64 `json:"pointlist"`
		} `json:"series"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse metrics response: %w", err)
	}
	if raw.Status != "ok" {
		return nil, fmt.Errorf("datadog metrics query error: status=%s", raw.Status)
	}

	result := &MetricsResult{
		Query: query,
		From:  from,
		To:    to,
	}
	for _, s := range raw.Series {
		result.Series = append(result.Series, MetricSeries{
			Metric:     s.Metric,
			Expression: s.Expression,
			Scope:      s.Scope,
			Tags:       s.TagSet,
			Points:     s.Pointlist,
		})
	}
	return result, nil
}

// LogsSearch searches Datadog log events.
// from and to are Unix timestamps in seconds.
func (a *Adapter) LogsSearch(ctx context.Context, query string, from, to int64, limit int) (*LogsResult, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	if limit <= 0 {
		limit = 25
	}

	fromStr := fmt.Sprintf("%d", from)
	toStr := fmt.Sprintf("%d", to)

	payload := map[string]any{
		"filter": map[string]any{
			"query": query,
			"from":  fromStr,
			"to":    toStr,
		},
		"page": map[string]any{
			"limit": limit,
		},
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal logs search payload: %w", err)
	}

	u := a.config.BaseURL() + "/api/v2/logs/events/search"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("build logs search request: %w", err)
	}
	a.addHeaders(req, a.config)
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("logs search request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read logs response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("logs search failed (status %d): %s", resp.StatusCode, string(body))
	}

	var raw struct {
		Data []struct {
			ID         string `json:"id"`
			Attributes struct {
				Timestamp  string         `json:"timestamp"`
				Host       string         `json:"host"`
				Service    string         `json:"service"`
				Status     string         `json:"status"`
				Message    string         `json:"message"`
				Attributes map[string]any `json:"attributes"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse logs response: %w", err)
	}

	result := &LogsResult{}
	for _, d := range raw.Data {
		entry := LogEntry{
			ID:        d.ID,
			Timestamp: d.Attributes.Timestamp,
			Host:      d.Attributes.Host,
			Service:   d.Attributes.Service,
			Status:    d.Attributes.Status,
			Message:   d.Attributes.Message,
		}
		if len(d.Attributes.Attributes) > 0 {
			entry.Attributes = make(map[string]string)
			for k, v := range d.Attributes.Attributes {
				entry.Attributes[k] = fmt.Sprintf("%v", v)
			}
		}
		result.Logs = append(result.Logs, entry)
	}
	result.Count = len(result.Logs)
	return result, nil
}

// addHeaders sets Datadog authentication headers.
func (a *Adapter) addHeaders(req *http.Request, cfg Config) {
	req.Header.Set("DD-API-KEY", cfg.APIKey)
	if cfg.AppKey != "" {
		req.Header.Set("DD-APPLICATION-KEY", cfg.AppKey)
	}
	req.Header.Set("Accept", "application/json")
}

func (a *Adapter) checkConnected() error {
	if !a.connected {
		return ErrNotConnected
	}
	return nil
}

// ListActiveServices returns distinct service names discovered from Datadog's active
// host list. Hosts report their service tags (e.g. "service:payment-api") which are
// used to build metrics_in edges during graph refresh.
func (a *Adapter) ListActiveServices(ctx context.Context) ([]string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		a.config.BaseURL()+"/api/v1/hosts", nil)
	if err != nil {
		return nil, fmt.Errorf("build hosts request: %w", err)
	}
	a.addHeaders(req, a.config)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list hosts request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read hosts response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list hosts failed (status %d): %s", resp.StatusCode, string(body))
	}

	var raw struct {
		HostList []struct {
			TagsBySource map[string][]string `json:"tags_by_source"`
		} `json:"host_list"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse hosts response: %w", err)
	}

	seen := make(map[string]struct{})
	var services []string
	for _, host := range raw.HostList {
		for _, tags := range host.TagsBySource {
			for _, tag := range tags {
				if strings.HasPrefix(tag, "service:") {
					svc := strings.TrimPrefix(tag, "service:")
					if svc != "" {
						if _, ok := seen[svc]; !ok {
							seen[svc] = struct{}{}
							services = append(services, svc)
						}
					}
				}
			}
		}
	}
	return services, nil
}

// ListLogServices returns distinct service names from recent Datadog log events
// (last 15 minutes, up to 500 events). Used to build logs_in edges during graph refresh.
func (a *Adapter) ListLogServices(ctx context.Context) ([]string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	now := time.Now()
	payload := map[string]any{
		"filter": map[string]any{
			"query": "*",
			"from":  fmt.Sprintf("%d", now.Add(-15*time.Minute).Unix()),
			"to":    fmt.Sprintf("%d", now.Unix()),
		},
		"page": map[string]any{
			"limit": 500,
		},
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal log services payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		a.config.BaseURL()+"/api/v2/logs/events/search", bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("build log services request: %w", err)
	}
	a.addHeaders(req, a.config)
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("log services request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read log services response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("log services search failed (status %d): %s", resp.StatusCode, string(body))
	}

	var raw struct {
		Data []struct {
			Attributes struct {
				Service string `json:"service"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse log services response: %w", err)
	}

	seen := make(map[string]struct{})
	var services []string
	for _, d := range raw.Data {
		svc := d.Attributes.Service
		if svc != "" {
			if _, ok := seen[svc]; !ok {
				seen[svc] = struct{}{}
				services = append(services, svc)
			}
		}
	}
	return services, nil
}
