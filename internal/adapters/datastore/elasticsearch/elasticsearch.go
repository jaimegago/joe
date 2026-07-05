package elasticsearch

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
	statusNotConnected = "Not connected to Elasticsearch"
	statusConnectedFmt = "Connected to Elasticsearch at %s"
)

// ErrNotConnected means the adapter is not connected.
var ErrNotConnected = errors.New("adapter not connected to Elasticsearch")

// ClusterHealth holds the result of GET /_cluster/health.
type ClusterHealth struct {
	ClusterName      string `json:"cluster_name"`
	Status           string `json:"status"` // green, yellow, red
	Shards           int    `json:"active_shards"`
	UnassignedShards int    `json:"unassigned_shards"`
	Nodes            int    `json:"number_of_nodes"`
}

// IndexInfo holds one row from GET /_cat/indices.
type IndexInfo struct {
	Name      string `json:"name"`
	Status    string `json:"status"` // open, close
	Health    string `json:"health"` // green, yellow, red
	Docs      int64  `json:"docs_count"`
	StoreSize string `json:"store_size"`
	Primaries int    `json:"pri"`
	Replicas  int    `json:"rep"`
}

// httpDoer abstracts net/http.Client for testing.
type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// ElasticsearchAdapter is the interface for Elasticsearch operations.
type ElasticsearchAdapter interface {
	adapters.Adapter
	ClusterHealth(ctx context.Context) (*ClusterHealth, error)
	ListIndices(ctx context.Context, pattern string) ([]IndexInfo, error)
}

// Adapter is the concrete Elasticsearch adapter.
type Adapter struct {
	mu        sync.RWMutex
	config    Config
	client    httpDoer
	connected bool
}

// New creates a new Elasticsearch adapter (not yet connected).
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

// Connect establishes and verifies connectivity to Elasticsearch.
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

	// Verify connectivity via cluster health endpoint.
	healthURL := strings.TrimRight(cfg.URL, "/") + "/_cluster/health"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		return fmt.Errorf("build health-check request: %w", err)
	}
	a.addAuth(req, cfg)

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("connect to Elasticsearch at %s: %w", cfg.URL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("elasticsearch health check failed (status %d): %s", resp.StatusCode, string(body))
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

// ClusterHealth returns the Elasticsearch cluster health.
func (a *Adapter) ClusterHealth(ctx context.Context) (*ClusterHealth, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	u := strings.TrimRight(a.config.URL, "/") + "/_cluster/health"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build cluster health request: %w", err)
	}
	a.addAuth(req, a.config)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cluster health request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read cluster health response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cluster health failed (status %d): %s", resp.StatusCode, string(body))
	}

	var health ClusterHealth
	if err := json.Unmarshal(body, &health); err != nil {
		return nil, fmt.Errorf("parse cluster health response: %w", err)
	}
	return &health, nil
}

// ListIndices returns index information, optionally filtered by pattern.
func (a *Adapter) ListIndices(ctx context.Context, pattern string) ([]IndexInfo, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	base := strings.TrimRight(a.config.URL, "/") + "/_cat/indices"
	if pattern != "" {
		base += "/" + url.PathEscape(pattern)
	}
	base += "?format=json&h=index,status,health,docs.count,store.size,pri,rep"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base, nil)
	if err != nil {
		return nil, fmt.Errorf("build indices request: %w", err)
	}
	a.addAuth(req, a.config)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("indices request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read indices response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("indices request failed (status %d): %s", resp.StatusCode, string(body))
	}

	// _cat/indices with format=json returns an array of objects.
	var raw []map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse indices response: %w", err)
	}

	indices := make([]IndexInfo, 0, len(raw))
	for _, row := range raw {
		idx := IndexInfo{
			Name:      toString(row["index"]),
			Status:    toString(row["status"]),
			Health:    toString(row["health"]),
			StoreSize: toString(row["store.size"]),
			Docs:      toInt64(row["docs.count"]),
			Primaries: toInt(row["pri"]),
			Replicas:  toInt(row["rep"]),
		}
		indices = append(indices, idx)
	}
	return indices, nil
}

func (a *Adapter) addAuth(req *http.Request, cfg Config) {
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "ApiKey "+cfg.APIKey)
	} else if cfg.Username != "" {
		req.SetBasicAuth(cfg.Username, cfg.Password)
	}
}

func (a *Adapter) checkConnected() error {
	if !a.connected {
		return ErrNotConnected
	}
	return nil
}

func toString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

func toInt64(v any) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case float64:
		return int64(x)
	case string:
		var n int64
		fmt.Sscanf(x, "%d", &n)
		return n
	}
	return 0
}

func toInt(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	case string:
		var n int
		fmt.Sscanf(x, "%d", &n)
		return n
	}
	return 0
}
