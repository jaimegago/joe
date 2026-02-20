package pagerduty

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
	statusNotConnected = "Not connected to PagerDuty"
	statusConnected    = "Connected to PagerDuty"
)

var (
	// ErrNotConnected indicates the adapter is not connected.
	ErrNotConnected = errors.New("adapter not connected to PagerDuty")
)

// Service is a PagerDuty service reference.
type Service struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Incident represents a PagerDuty incident.
type Incident struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Status      string    `json:"status"` // "triggered", "acknowledged", "resolved"
	Urgency     string    `json:"urgency"` // "high", "low"
	Service     Service   `json:"service"`
	CreatedAt   time.Time `json:"created_at"`
	HTMLURL     string    `json:"html_url"`
	Description string    `json:"description,omitempty"`
}

// PagerDutyAdapter extends the base Adapter with PagerDuty-specific operations.
type PagerDutyAdapter interface {
	adapters.Adapter
	// ListIncidents returns incidents filtered by service and/or status.
	// service is an optional service ID. status is comma-separated statuses
	// ("triggered", "acknowledged", "resolved"). limit is max results.
	ListIncidents(ctx context.Context, serviceID, status string, limit int) ([]Incident, error)
	// ListServices returns all PagerDuty services.
	ListServices(ctx context.Context) ([]Service, error)
}

// httpDoer abstracts net/http.Client for testing.
type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Adapter is the concrete PagerDuty adapter.
type Adapter struct {
	mu        sync.RWMutex
	config    Config
	baseURL   string // overridable in tests
	client    httpDoer
	connected bool
}

// New creates a new PagerDuty adapter (not yet connected).
func New() *Adapter {
	return &Adapter{
		baseURL: defaultAPIURL,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// NewWithClient creates an adapter with a custom HTTP client and base URL (for testing).
func NewWithClient(client httpDoer, baseURL string) *Adapter {
	return &Adapter{
		baseURL:   baseURL,
		client:    client,
		connected: true,
	}
}

// Connect establishes and verifies connectivity to PagerDuty.
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
	a.baseURL = cfg.APIURL

	// Verify connectivity by listing abilities (lightweight, no data returned).
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		a.baseURL+"/abilities", nil)
	if err != nil {
		return fmt.Errorf("build health-check request: %w", err)
	}
	a.setHeaders(req)

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("connect to PagerDuty: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("pagerduty health check failed (status %d): %s", resp.StatusCode, string(body))
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
		return adapters.Status{Connected: true, Message: statusConnected}
	}
	return adapters.Status{Connected: false, Message: statusNotConnected}
}

// ListIncidents returns PagerDuty incidents.
func (a *Adapter) ListIncidents(ctx context.Context, serviceID, status string, limit int) ([]Incident, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if !a.connected {
		return nil, ErrNotConnected
	}

	if limit <= 0 {
		limit = 25
	}

	params := url.Values{}
	params.Set("limit", fmt.Sprintf("%d", limit))

	// Default to open (triggered + acknowledged) if no status specified.
	if status == "" {
		params.Add("statuses[]", "triggered")
		params.Add("statuses[]", "acknowledged")
	} else {
		for _, s := range strings.Split(status, ",") {
			if st := strings.TrimSpace(s); st != "" {
				params.Add("statuses[]", st)
			}
		}
	}

	if serviceID != "" {
		params.Add("service_ids[]", serviceID)
	}

	u := a.baseURL + "/incidents?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build incidents request: %w", err)
	}
	a.setHeaders(req)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("incidents request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read incidents response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("incidents request failed (status %d): %s", resp.StatusCode, string(body))
	}

	var raw struct {
		Incidents []rawIncident `json:"incidents"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse incidents response: %w", err)
	}

	incidents := make([]Incident, 0, len(raw.Incidents))
	for _, r := range raw.Incidents {
		incidents = append(incidents, Incident{
			ID:          r.ID,
			Title:       r.Title,
			Status:      r.Status,
			Urgency:     r.Urgency,
			Service:     r.Service,
			CreatedAt:   r.CreatedAt,
			HTMLURL:     r.HTMLURL,
			Description: r.Description,
		})
	}

	return incidents, nil
}

// ListServices returns all PagerDuty services.
func (a *Adapter) ListServices(ctx context.Context) ([]Service, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if !a.connected {
		return nil, ErrNotConnected
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		a.baseURL+"/services?limit=100", nil)
	if err != nil {
		return nil, fmt.Errorf("build services request: %w", err)
	}
	a.setHeaders(req)

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
		return nil, fmt.Errorf("services request failed (status %d): %s", resp.StatusCode, string(body))
	}

	var raw struct {
		Services []Service `json:"services"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse services response: %w", err)
	}

	return raw.Services, nil
}

type rawIncident struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Status      string    `json:"status"`
	Urgency     string    `json:"urgency"`
	Service     Service   `json:"service"`
	CreatedAt   time.Time `json:"created_at"`
	HTMLURL     string    `json:"html_url"`
	Description string    `json:"description"`
}

// setHeaders sets PagerDuty API authentication headers.
func (a *Adapter) setHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Token token="+a.config.APIKey)
	req.Header.Set("Accept", "application/vnd.pagerduty+json;version=2")
	req.Header.Set("Content-Type", "application/json")
}
