package loki

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
	"github.com/jaimegago/joe/internal/store"
)

const (
	statusNotConnected = "Not connected to Loki"
	statusConnectedFmt = "Connected to Loki at %s"

	defaultLimit = 100
)

var (
	// ErrNotConnected indicates the adapter is not connected.
	ErrNotConnected = errors.New("adapter not connected to Loki")
)

// LogEntry is a single log line with timestamp and labels.
type LogEntry struct {
	Timestamp string            `json:"timestamp"` // RFC3339Nano
	Line      string            `json:"line"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// QueryResult holds the result of a LogQL query.
type QueryResult struct {
	ResultType string     `json:"result_type"` // "streams", "vector", "matrix"
	Entries    []LogEntry `json:"entries,omitempty"`
}

// LokiAdapter extends the base Adapter with Loki-specific operations.
type LokiAdapter interface {
	adapters.Adapter
	// Query executes an instant LogQL query.
	Query(ctx context.Context, query string, limit int, since time.Duration) (*QueryResult, error)
	// QueryRange executes a range LogQL query.
	QueryRange(ctx context.Context, query string, start, end time.Time, limit int) (*QueryResult, error)
	// ListServices returns values of the "app" label from Loki for logs_in edge discovery.
	ListServices(ctx context.Context) ([]string, error)
}

// httpDoer abstracts net/http.Client for testing.
type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Adapter is the concrete Loki adapter.
type Adapter struct {
	mu        sync.RWMutex
	config    Config
	client    httpDoer
	connected bool
}

// New creates a new Loki adapter (not yet connected).
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

// Connect establishes and verifies connectivity to Loki.
func (a *Adapter) Connect(ctx context.Context, source store.Source) error {
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

	// Verify connectivity by listing labels.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimRight(cfg.URL, "/")+"/loki/api/v1/labels", nil)
	if err != nil {
		return fmt.Errorf("build health-check request: %w", err)
	}
	a.addHeaders(req, cfg)

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("connect to Loki at %s: %w", cfg.URL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("loki health check failed (status %d): %s", resp.StatusCode, string(body))
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

// Query executes an instant LogQL query (tail from now, looking back `since`).
func (a *Adapter) Query(ctx context.Context, query string, limit int, since time.Duration) (*QueryResult, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	if limit <= 0 {
		limit = defaultLimit
	}

	u := strings.TrimRight(a.config.URL, "/") + "/loki/api/v1/query"
	params := url.Values{}
	params.Set("query", query)
	params.Set("limit", fmt.Sprintf("%d", limit))
	if since > 0 {
		start := time.Now().Add(-since).UnixNano()
		params.Set("start", fmt.Sprintf("%d", start))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u+"?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("build query request: %w", err)
	}
	a.addHeaders(req, a.config)

	return a.execQuery(req)
}

// QueryRange executes a range LogQL query.
func (a *Adapter) QueryRange(ctx context.Context, query string, start, end time.Time, limit int) (*QueryResult, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	if limit <= 0 {
		limit = defaultLimit
	}

	u := strings.TrimRight(a.config.URL, "/") + "/loki/api/v1/query_range"
	params := url.Values{}
	params.Set("query", query)
	params.Set("start", fmt.Sprintf("%d", start.UnixNano()))
	params.Set("end", fmt.Sprintf("%d", end.UnixNano()))
	params.Set("limit", fmt.Sprintf("%d", limit))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u+"?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("build query_range request: %w", err)
	}
	a.addHeaders(req, a.config)

	return a.execQuery(req)
}

// ListServices returns the unique values of the "app" label from Loki.
// These are used to discover which services ship logs to this Loki instance.
func (a *Adapter) ListServices(ctx context.Context) ([]string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	u := strings.TrimRight(a.config.URL, "/") + "/loki/api/v1/label/app/values"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build label-values request: %w", err)
	}
	a.addHeaders(req, a.config)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("label-values request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read label-values response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("label-values failed (status %d): %s", resp.StatusCode, string(body))
	}

	var raw struct {
		Status string   `json:"status"`
		Data   []string `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse label-values response: %w", err)
	}

	return raw.Data, nil
}

// execQuery parses Loki's query response into a QueryResult.
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

	// Loki response structure.
	var raw struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string `json:"resultType"`
			Result     []struct {
				Stream map[string]string `json:"stream"`
				Values [][]string        `json:"values"` // [[unixnano_str, line], ...]
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse query response: %w", err)
	}

	if raw.Status != "success" {
		return nil, fmt.Errorf("loki query error: %s", raw.Status)
	}

	result := &QueryResult{ResultType: raw.Data.ResultType}
	for _, stream := range raw.Data.Result {
		for _, val := range stream.Values {
			if len(val) < 2 {
				continue
			}
			result.Entries = append(result.Entries, LogEntry{
				Timestamp: val[0],
				Line:      val[1],
				Labels:    stream.Stream,
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
