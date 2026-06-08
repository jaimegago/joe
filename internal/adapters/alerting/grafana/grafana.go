package grafana

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
	statusNotConnected = "Not connected to Grafana"
	statusConnectedFmt = "Connected to Grafana at %s"
)

var (
	// ErrNotConnected indicates the adapter is not connected.
	ErrNotConnected = errors.New("adapter not connected to Grafana")
)

// Dashboard is a Grafana dashboard search result.
type Dashboard struct {
	ID    int      `json:"id"`
	UID   string   `json:"uid"`
	Title string   `json:"title"`
	URI   string   `json:"uri"`
	URL   string   `json:"url"`
	Slug  string   `json:"slug"`
	Tags  []string `json:"tags"`
	Type  string   `json:"type"`
}

// Panel is a single panel within a dashboard.
type Panel struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
	Type  string `json:"type"`
}

// DashboardDetail contains full dashboard information.
type DashboardDetail struct {
	ID          int       `json:"id"`
	UID         string    `json:"uid"`
	Title       string    `json:"title"`
	Description string    `json:"description,omitempty"`
	Tags        []string  `json:"tags"`
	Panels      []Panel   `json:"panels"`
	URL         string    `json:"url"`
	Version     int       `json:"version"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// GrafanaAlert is a Grafana-managed alert rule or Alertmanager alert.
type GrafanaAlert struct {
	Fingerprint string            `json:"fingerprint"`
	State       string            `json:"state"` // "unprocessed", "active", "suppressed"
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	StartsAt    time.Time         `json:"starts_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// GrafanaAdapter extends the base Adapter with Grafana-specific operations.
type GrafanaAdapter interface {
	adapters.Adapter
	// ListDashboards searches for dashboards by query string.
	ListDashboards(ctx context.Context, query string, limit int) ([]Dashboard, error)
	// GetDashboard retrieves a dashboard by UID.
	GetDashboard(ctx context.Context, uid string) (*DashboardDetail, error)
	// ListAlerts returns active Grafana-managed alerts.
	ListAlerts(ctx context.Context) ([]GrafanaAlert, error)
}

// httpDoer abstracts net/http.Client for testing.
type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Adapter is the concrete Grafana adapter.
type Adapter struct {
	mu        sync.RWMutex
	config    Config
	client    httpDoer
	connected bool
}

// New creates a new Grafana adapter (not yet connected).
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

// Connect establishes and verifies connectivity to Grafana.
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

	// Verify connectivity via the health endpoint.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimRight(cfg.URL, "/")+"/api/health", nil)
	if err != nil {
		return fmt.Errorf("build health-check request: %w", err)
	}
	a.addHeaders(req)

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("connect to Grafana at %s: %w", cfg.URL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("grafana health check failed (status %d): %s", resp.StatusCode, string(body))
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

// ListDashboards searches for dashboards by optional query string.
func (a *Adapter) ListDashboards(ctx context.Context, query string, limit int) ([]Dashboard, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if !a.connected {
		return nil, ErrNotConnected
	}

	if limit <= 0 {
		limit = 50
	}

	params := url.Values{}
	params.Set("type", "dash-db")
	params.Set("limit", fmt.Sprintf("%d", limit))
	if query != "" {
		params.Set("query", query)
	}

	u := strings.TrimRight(a.config.URL, "/") + "/api/search?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build dashboards request: %w", err)
	}
	a.addHeaders(req)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("dashboards request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read dashboards response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("dashboards request failed (status %d): %s", resp.StatusCode, string(body))
	}

	var dashboards []Dashboard
	if err := json.Unmarshal(body, &dashboards); err != nil {
		return nil, fmt.Errorf("parse dashboards response: %w", err)
	}

	return dashboards, nil
}

// GetDashboard retrieves a full dashboard by UID.
func (a *Adapter) GetDashboard(ctx context.Context, uid string) (*DashboardDetail, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if !a.connected {
		return nil, ErrNotConnected
	}

	u := strings.TrimRight(a.config.URL, "/") + "/api/dashboards/uid/" + url.PathEscape(uid)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build dashboard request: %w", err)
	}
	a.addHeaders(req)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("dashboard request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read dashboard response: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("dashboard not found: %s", uid)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("dashboard request failed (status %d): %s", resp.StatusCode, string(body))
	}

	// Grafana wraps the dashboard in a meta envelope.
	var raw struct {
		Dashboard rawDashboard `json:"dashboard"`
		Meta      struct {
			URL     string    `json:"url"`
			Updated time.Time `json:"updated"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse dashboard response: %w", err)
	}

	panels := make([]Panel, 0, len(raw.Dashboard.Panels))
	for _, p := range raw.Dashboard.Panels {
		panels = append(panels, Panel{
			ID:    p.ID,
			Title: p.Title,
			Type:  p.Type,
		})
	}

	return &DashboardDetail{
		ID:          raw.Dashboard.ID,
		UID:         raw.Dashboard.UID,
		Title:       raw.Dashboard.Title,
		Description: raw.Dashboard.Description,
		Tags:        raw.Dashboard.Tags,
		Panels:      panels,
		URL:         raw.Meta.URL,
		Version:     raw.Dashboard.Version,
		UpdatedAt:   raw.Meta.Updated,
	}, nil
}

// ListAlerts returns active Grafana-managed alerts via Alertmanager API.
func (a *Adapter) ListAlerts(ctx context.Context) ([]GrafanaAlert, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if !a.connected {
		return nil, ErrNotConnected
	}

	u := strings.TrimRight(a.config.URL, "/") + "/api/alertmanager/grafana/api/v2/alerts"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build alerts request: %w", err)
	}
	a.addHeaders(req)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("alerts request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read alerts response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("alerts request failed (status %d): %s", resp.StatusCode, string(body))
	}

	var raw []struct {
		Fingerprint string `json:"fingerprint"`
		Status      struct {
			State string `json:"state"`
		} `json:"status"`
		Labels      map[string]string `json:"labels"`
		Annotations map[string]string `json:"annotations"`
		StartsAt    time.Time         `json:"startsAt"`
		UpdatedAt   time.Time         `json:"updatedAt"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse alerts response: %w", err)
	}

	alerts := make([]GrafanaAlert, 0, len(raw))
	for _, r := range raw {
		alerts = append(alerts, GrafanaAlert{
			Fingerprint: r.Fingerprint,
			State:       r.Status.State,
			Labels:      r.Labels,
			Annotations: r.Annotations,
			StartsAt:    r.StartsAt,
			UpdatedAt:   r.UpdatedAt,
		})
	}

	return alerts, nil
}

type rawDashboard struct {
	ID          int      `json:"id"`
	UID         string   `json:"uid"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	Version     int      `json:"version"`
	Panels      []struct {
		ID    int    `json:"id"`
		Title string `json:"title"`
		Type  string `json:"type"`
	} `json:"panels"`
}

// addHeaders sets Grafana API authentication headers.
func (a *Adapter) addHeaders(req *http.Request) {
	if a.config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+a.config.APIKey)
	}
	req.Header.Set("Content-Type", "application/json")
}
