package jaeger

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
	statusNotConnected = "Not connected to Jaeger"
	statusConnectedFmt = "Connected to Jaeger at %s"

	defaultTraceLimit = 20
)

var (
	// ErrNotConnected indicates the adapter is not connected.
	ErrNotConnected = errors.New("adapter not connected to Jaeger")
	// ErrTraceNotFound indicates a trace lookup failed.
	ErrTraceNotFound = errors.New("trace not found")
)

// TraceSearchResult is a trace summary from Jaeger search.
type TraceSearchResult struct {
	TraceID   string  `json:"trace_id"`
	Service   string  `json:"service,omitempty"`
	Operation string  `json:"operation,omitempty"`
	StartTime string  `json:"start_time,omitempty"`
	Duration  float64 `json:"duration_ms,omitempty"`
	SpanCount int     `json:"span_count,omitempty"`
}

// JaegerAdapter extends the base Adapter with Jaeger-specific operations.
type JaegerAdapter interface {
	adapters.Adapter
	// ListServices returns all service names Jaeger has seen.
	ListServices(ctx context.Context) ([]string, error)
	// SearchTraces searches for traces by service and optional operation.
	SearchTraces(ctx context.Context, service, operation string, limit int) ([]TraceSearchResult, error)
	// GetTrace retrieves a full trace by ID.
	GetTrace(ctx context.Context, traceID string) (map[string]any, error)
}

// httpDoer abstracts net/http.Client for testing.
type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Adapter is the concrete Jaeger adapter.
type Adapter struct {
	mu        sync.RWMutex
	config    Config
	client    httpDoer
	connected bool
}

// New creates a new Jaeger adapter (not yet connected).
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

// Connect establishes and verifies connectivity to Jaeger.
func (a *Adapter) Connect(ctx context.Context, source store.Component) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	var configMap map[string]any
	if len(source.Config) > 0 {
		if err := json.Unmarshal(source.Config, &configMap); err != nil {
			return fmt.Errorf("parse component config JSON: %w", err)
		}
	} else {
		configMap = make(map[string]any)
	}

	cfg, err := ParseConfig(configMap)
	if err != nil {
		return fmt.Errorf("parse component config: %w", err)
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

	// Verify connectivity by listing services (lightweight call).
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimRight(cfg.URL, "/")+"/api/services", nil)
	if err != nil {
		return fmt.Errorf("build health-check request: %w", err)
	}
	a.addHeaders(req, cfg)

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("connect to Jaeger at %s: %w", cfg.URL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("jaeger health check failed (status %d): %s", resp.StatusCode, string(body))
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

// ListServices returns all service names Jaeger knows about.
func (a *Adapter) ListServices(ctx context.Context) ([]string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	u := strings.TrimRight(a.config.URL, "/") + "/api/services"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build services request: %w", err)
	}
	a.addHeaders(req, a.config)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("services request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read services response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list services failed (status %d): %s", resp.StatusCode, string(body))
	}

	var raw struct {
		Data []string `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse services response: %w", err)
	}

	return raw.Data, nil
}

// SearchTraces searches Jaeger for traces by service and optional operation.
func (a *Adapter) SearchTraces(ctx context.Context, service, operation string, limit int) ([]TraceSearchResult, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	if limit <= 0 {
		limit = defaultTraceLimit
	}

	u := strings.TrimRight(a.config.URL, "/") + "/api/traces"
	params := url.Values{}
	params.Set("service", service)
	params.Set("limit", fmt.Sprintf("%d", limit))
	if operation != "" {
		params.Set("operation", operation)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u+"?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("build traces request: %w", err)
	}
	a.addHeaders(req, a.config)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search traces request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read traces response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search traces failed (status %d): %s", resp.StatusCode, string(body))
	}

	var raw struct {
		Data []struct {
			TraceID string `json:"traceID"`
			Spans   []struct {
				OperationName string `json:"operationName"`
				StartTime     int64  `json:"startTime"` // microseconds
				Duration      int64  `json:"duration"`  // microseconds
			} `json:"spans"`
			Processes map[string]struct {
				ServiceName string `json:"serviceName"`
			} `json:"processes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse traces response: %w", err)
	}

	results := make([]TraceSearchResult, 0, len(raw.Data))
	for _, trace := range raw.Data {
		result := TraceSearchResult{
			TraceID:   trace.TraceID,
			Service:   service,
			SpanCount: len(trace.Spans),
		}
		if len(trace.Spans) > 0 {
			root := trace.Spans[0]
			result.Operation = root.OperationName
			result.StartTime = time.UnixMicro(root.StartTime).UTC().Format(time.RFC3339)
			result.Duration = float64(root.Duration) / 1000.0 // convert to ms
		}
		results = append(results, result)
	}

	return results, nil
}

// GetTrace retrieves a full trace by ID.
func (a *Adapter) GetTrace(ctx context.Context, traceID string) (map[string]any, error) {
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

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse trace response: %w", err)
	}

	return result, nil
}

// addHeaders sets authentication headers.
func (a *Adapter) addHeaders(req *http.Request, cfg Config) {
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
}

func (a *Adapter) checkConnected() error {
	if !a.connected {
		return ErrNotConnected
	}
	return nil
}
