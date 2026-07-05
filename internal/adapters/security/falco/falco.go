package falco

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
	statusNotConnected = "Not connected to Falco"
	statusConnectedFmt = "Connected to Falco at %s"

	defaultEventsLimit = 50
)

// ErrNotConnected is returned when a method is called before Connect succeeds.
var ErrNotConnected = errors.New("adapter not connected to Falco")

// Event represents a Falco runtime security event.
type Event struct {
	UUID         string         `json:"uuid,omitempty"`
	Output       string         `json:"output"`
	Priority     string         `json:"priority"` // Emergency, Alert, Critical, Error, Warning, Notice, Informational, Debug
	Rule         string         `json:"rule"`
	Time         time.Time      `json:"time"`
	Source       string         `json:"source"` // syscall, k8s_audit
	Tags         []string       `json:"tags,omitempty"`
	OutputFields map[string]any `json:"output_fields,omitempty"`
}

// Rule summarises a Falco rule derived from recent events.
type Rule struct {
	Name     string `json:"name"`
	Priority string `json:"priority"`
	Source   string `json:"source"`
	Count    int    `json:"count"` // number of events seen for this rule
}

// FalcoAdapter extends the base Adapter with Falco-specific operations.
type FalcoAdapter interface {
	adapters.Adapter
	// ListEvents returns recent runtime security events, filtered by priority,
	// source (syscall/k8s_audit), and/or rule name. limit 0 uses a default.
	ListEvents(ctx context.Context, priority, source, rule string, limit int) ([]Event, error)
	// ListRules returns the unique rules seen in recent events with their
	// most-seen priority and event count.
	ListRules(ctx context.Context) ([]Rule, error)
}

// httpDoer abstracts net/http.Client for testing.
type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Adapter is the concrete Falco adapter.
type Adapter struct {
	mu        sync.RWMutex
	config    Config
	client    httpDoer
	connected bool
}

// New creates a new Falco adapter (not yet connected).
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

// Connect establishes and verifies connectivity to the Falco Sidekick backend.
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

	// Health check: a lightweight events query (limit=1) confirms the API is up.
	u := strings.TrimRight(cfg.URL, "/") + "/api/v1/events?limit=1"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("build health-check request: %w", err)
	}
	a.addHeaders(req)

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("connect to Falco at %s: %w", cfg.URL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("falco health check failed (status %d): %s", resp.StatusCode, string(body))
	}

	a.connected = true
	return nil
}

// Disconnect marks the adapter as disconnected.
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

// ListEvents returns recent Falco runtime security events.
// priority, source, and rule are optional filters. limit 0 uses defaultEventsLimit.
func (a *Adapter) ListEvents(ctx context.Context, priority, source, rule string, limit int) ([]Event, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if !a.connected {
		return nil, ErrNotConnected
	}

	return a.fetchEvents(ctx, priority, source, rule, limit)
}

// ListRules returns the unique Falco rules observed in recent events.
func (a *Adapter) ListRules(ctx context.Context) ([]Rule, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if !a.connected {
		return nil, ErrNotConnected
	}

	events, err := a.fetchEvents(ctx, "", "", "", 500)
	if err != nil {
		return nil, fmt.Errorf("fetch events for rule extraction: %w", err)
	}

	type entry struct {
		priority string
		source   string
		count    int
	}
	seen := make(map[string]*entry)
	for _, e := range events {
		if _, ok := seen[e.Rule]; !ok {
			seen[e.Rule] = &entry{priority: e.Priority, source: e.Source}
		}
		seen[e.Rule].count++
	}

	rules := make([]Rule, 0, len(seen))
	for name, ent := range seen {
		rules = append(rules, Rule{
			Name:     name,
			Priority: ent.priority,
			Source:   ent.source,
			Count:    ent.count,
		})
	}

	return rules, nil
}

// fetchEvents performs the HTTP call without acquiring the lock.
// Callers must already hold a.mu.RLock (or Lock).
func (a *Adapter) fetchEvents(ctx context.Context, priority, source, rule string, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = defaultEventsLimit
	}

	params := url.Values{}
	params.Set("limit", fmt.Sprintf("%d", limit))
	if priority != "" {
		params.Set("priority", priority)
	}
	if source != "" {
		params.Set("source", source)
	}
	if rule != "" {
		params.Set("rule", rule)
	}

	u := strings.TrimRight(a.config.URL, "/") + "/api/v1/events?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build events request: %w", err)
	}
	a.addHeaders(req)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("events request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read events response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("events request failed (status %d): %s", resp.StatusCode, string(body))
	}

	// Falcosidekick UI returns {"events": [...], "total": N}.
	var result struct {
		Events []rawEvent `json:"events"`
		Total  int        `json:"total"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse events response: %w", err)
	}

	events := make([]Event, 0, len(result.Events))
	for _, r := range result.Events {
		// rawEvent is field-identical to Event (tags aside), so a plain
		// conversion replaces the field-by-field copy (staticcheck S1016).
		events = append(events, Event(r))
	}

	return events, nil
}

type rawEvent struct {
	UUID         string         `json:"uuid"`
	Output       string         `json:"output"`
	Priority     string         `json:"priority"`
	Rule         string         `json:"rule"`
	Time         time.Time      `json:"time"`
	Source       string         `json:"source"`
	Tags         []string       `json:"tags"`
	OutputFields map[string]any `json:"output_fields"`
}

// addHeaders sets authentication headers on the request.
func (a *Adapter) addHeaders(req *http.Request) {
	if a.config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+a.config.APIKey)
	}
}
