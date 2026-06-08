package splunk

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
	statusNotConnected = "Not connected to Splunk"
	statusConnectedFmt = "Connected to Splunk at %s"
)

var ErrNotConnected = errors.New("adapter not connected to Splunk")

// SearchEvent is a single result from a Splunk search.
type SearchEvent struct {
	Time   string         `json:"time,omitempty"`
	Host   string         `json:"host,omitempty"`
	Source string         `json:"source,omitempty"`
	Raw    string         `json:"_raw,omitempty"`
	Fields map[string]any `json:"fields,omitempty"`
}

// SearchResult holds the results of a Splunk one-shot search.
type SearchResult struct {
	Events []SearchEvent `json:"events"`
	Count  int           `json:"count"`
}

// SplunkAdapter extends the base Adapter with Splunk-specific operations.
type SplunkAdapter interface {
	adapters.Adapter
	// Search executes a Splunk SPL search query using one-shot mode.
	// earliest and latest accept Splunk time modifiers (e.g. "-1h", "now") or ISO-8601 timestamps.
	Search(ctx context.Context, query, earliest, latest string, limit int) (*SearchResult, error)
}

// httpDoer abstracts net/http.Client for testing.
type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Adapter is the concrete Splunk adapter.
type Adapter struct {
	mu        sync.RWMutex
	config    Config
	client    httpDoer
	connected bool
}

// New creates a new Splunk adapter (not yet connected).
func New() *Adapter {
	return &Adapter{
		client: &http.Client{
			Timeout: 60 * time.Second,
			// Splunk self-signed certs are common; users should configure their
			// own TLS settings. Use the default transport which honours system CAs.
		},
	}
}

// NewWithClient creates an adapter with a custom HTTP client (for testing).
func NewWithClient(client httpDoer) *Adapter {
	return &Adapter{
		client:    client,
		connected: true,
	}
}

// Connect establishes and verifies connectivity to Splunk.
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

	// Verify connectivity via the server info endpoint.
	u := strings.TrimRight(cfg.URL, "/") + "/services/server/info?output_mode=json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("build health-check request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("connect to Splunk at %s: %w", cfg.URL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("splunk health check failed (status %d): %s", resp.StatusCode, string(body))
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
	return adapters.Status{Connected: false, Message: statusNotConnected}
}

// Search executes a Splunk SPL search in one-shot mode.
// earliest and latest accept Splunk relative time modifiers (e.g. "-1h", "now")
// or absolute ISO-8601 timestamps.
func (a *Adapter) Search(ctx context.Context, query, earliest, latest string, limit int) (*SearchResult, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	if limit <= 0 {
		limit = 100
	}

	// Splunk requires SPL queries to start with "search" for the REST API.
	spl := query
	if !strings.HasPrefix(strings.TrimSpace(strings.ToLower(spl)), "search") {
		spl = "search " + spl
	}

	form := url.Values{}
	form.Set("search", spl)
	form.Set("output_mode", "json")
	form.Set("exec_mode", "oneshot")
	form.Set("count", fmt.Sprintf("%d", limit))
	if earliest != "" {
		form.Set("earliest_time", earliest)
	}
	if latest != "" {
		form.Set("latest_time", latest)
	}

	u := strings.TrimRight(a.config.URL, "/") + "/services/search/jobs"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u,
		strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("build search request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+a.config.Token)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

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
		Results []map[string]any `json:"results"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse search response: %w", err)
	}

	result := &SearchResult{}
	for _, r := range raw.Results {
		event := SearchEvent{
			Fields: make(map[string]any),
		}
		for k, v := range r {
			switch k {
			case "_time":
				event.Time, _ = v.(string)
			case "host":
				event.Host, _ = v.(string)
			case "source":
				event.Source, _ = v.(string)
			case "_raw":
				event.Raw, _ = v.(string)
			default:
				event.Fields[k] = v
			}
		}
		result.Events = append(result.Events, event)
	}
	result.Count = len(result.Events)
	return result, nil
}

func (a *Adapter) checkConnected() error {
	if !a.connected {
		return ErrNotConnected
	}
	return nil
}
