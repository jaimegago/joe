package alertmanager

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
	statusNotConnected = "Not connected to Alertmanager"
	statusConnectedFmt = "Connected to Alertmanager at %s"
)

var (
	// ErrNotConnected indicates the adapter is not connected.
	ErrNotConnected = errors.New("adapter not connected to Alertmanager")
)

// Alert represents an Alertmanager alert.
type Alert struct {
	Fingerprint string            `json:"fingerprint"`
	Status      AlertStatus       `json:"status"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	StartsAt    time.Time         `json:"starts_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	EndsAt      time.Time         `json:"ends_at,omitempty"`
	Receivers   []string          `json:"receivers,omitempty"`
}

// AlertStatus holds the state of an alert.
type AlertStatus struct {
	State       string   `json:"state"` // "unprocessed", "active", "suppressed"
	InhibitedBy []string `json:"inhibited_by,omitempty"`
	SilencedBy  []string `json:"silenced_by,omitempty"`
}

// AlertmanagerAdapter extends the base Adapter with Alertmanager-specific operations.
type AlertmanagerAdapter interface {
	adapters.Adapter
	// ListAlerts returns active alerts, optionally filtered by label matchers.
	// filter is a comma-separated list of label matchers (e.g., "alertname=HighCPU,severity=critical").
	ListAlerts(ctx context.Context, filter string) ([]Alert, error)
}

// httpDoer abstracts net/http.Client for testing.
type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Adapter is the concrete Alertmanager adapter.
type Adapter struct {
	mu        sync.RWMutex
	config    Config
	client    httpDoer
	connected bool
}

// New creates a new Alertmanager adapter (not yet connected).
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

// Connect establishes and verifies connectivity to Alertmanager.
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
		strings.TrimRight(cfg.URL, "/")+"/api/v2/status", nil)
	if err != nil {
		return fmt.Errorf("build health-check request: %w", err)
	}
	a.addHeaders(req)

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("connect to Alertmanager at %s: %w", cfg.URL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("alertmanager health check failed (status %d): %s", resp.StatusCode, string(body))
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

// ListAlerts returns active alerts from Alertmanager.
// filter is an optional comma-separated list of label matchers.
func (a *Adapter) ListAlerts(ctx context.Context, filter string) ([]Alert, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if !a.connected {
		return nil, ErrNotConnected
	}

	u := strings.TrimRight(a.config.URL, "/") + "/api/v2/alerts"
	if filter != "" {
		params := url.Values{}
		params.Set("filter", filter)
		u += "?" + params.Encode()
	}

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

	// Alertmanager v2 API returns an array of alert objects.
	var raw []rawAlert
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse alerts response: %w", err)
	}

	alerts := make([]Alert, 0, len(raw))
	for _, r := range raw {
		receivers := make([]string, 0, len(r.Receivers))
		for _, rec := range r.Receivers {
			receivers = append(receivers, rec.Name)
		}
		alerts = append(alerts, Alert{
			Fingerprint: r.Fingerprint,
			Status:      r.Status,
			Labels:      r.Labels,
			Annotations: r.Annotations,
			StartsAt:    r.StartsAt,
			UpdatedAt:   r.UpdatedAt,
			EndsAt:      r.EndsAt,
			Receivers:   receivers,
		})
	}

	return alerts, nil
}

type rawAlert struct {
	Fingerprint string            `json:"fingerprint"`
	Status      AlertStatus       `json:"status"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	StartsAt    time.Time         `json:"startsAt"`
	UpdatedAt   time.Time         `json:"updatedAt"`
	EndsAt      time.Time         `json:"endsAt"`
	Receivers   []struct {
		Name string `json:"name"`
	} `json:"receivers"`
}

// addHeaders sets authentication headers.
func (a *Adapter) addHeaders(req *http.Request) {
	if a.config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+a.config.APIKey)
	}
}
