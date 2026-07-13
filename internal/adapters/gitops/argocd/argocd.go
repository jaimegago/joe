package argocd

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/credential"
	"github.com/jaimegago/joe/internal/store"
)

const (
	statusNotConnected = "Not connected to Argo CD"
	statusConnectedFmt = "Connected to Argo CD at %s"
)

// ErrNotConnected indicates the adapter is not connected.
var ErrNotConnected = errors.New("adapter not connected to Argo CD")

// App is a summary of an Argo CD application.
type App struct {
	Name        string `json:"name"`
	Project     string `json:"project"`
	Namespace   string `json:"namespace"`
	Cluster     string `json:"cluster"`
	SyncStatus  string `json:"sync_status"`
	Health      string `json:"health"`
	Revision    string `json:"revision"`
	RepoURL     string `json:"repo_url"`
	TargetPath  string `json:"target_path,omitempty"`
	TargetChart string `json:"target_chart,omitempty"`
}

// AppResource is one resource managed by an Argo CD app.
type AppResource struct {
	Group     string `json:"group"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Status    string `json:"sync_status"`
	Health    string `json:"health"`
}

// AppCondition is a status condition on an Argo CD app.
type AppCondition struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// AppDetail holds full details for one Argo CD application.
type AppDetail struct {
	App        App            `json:"app"`
	Resources  []AppResource  `json:"resources"`
	Conditions []AppCondition `json:"conditions"`
}

// Diff represents the sync diff for an Argo CD app.
type Diff struct {
	Name       string `json:"name"`
	SyncStatus string `json:"sync_status"`
	Revision   string `json:"revision"`
	Message    string `json:"message,omitempty"`
}

// SyncOperation is one entry in the sync history of an Argo CD app.
type SyncOperation struct {
	Revision   string `json:"revision"`
	Phase      string `json:"phase"`
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at,omitempty"`
	Message    string `json:"message,omitempty"`
	Initiator  string `json:"initiator,omitempty"`
}

// ArgoCDAdapter extends the base Adapter with Argo CD operations.
type ArgoCDAdapter interface {
	adapters.Adapter
	Apps(ctx context.Context, project string) ([]App, error)
	GetApp(ctx context.Context, name string) (*AppDetail, error)
	GetDiff(ctx context.Context, name string) (*Diff, error)
	GetHistory(ctx context.Context, name string, limit int) ([]SyncOperation, error)
}

// httpDoer abstracts net/http.Client for testing.
type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Adapter is the concrete Argo CD adapter.
type Adapter struct {
	mu        sync.RWMutex
	config    Config
	client    httpDoer
	connected bool
}

// New creates a new Argo CD adapter (not yet connected).
func New() *Adapter {
	return &Adapter{}
}

// NewWithClient creates an Argo CD adapter with an injected HTTP client (for testing).
func NewWithClient(client httpDoer, cfg Config) *Adapter {
	return &Adapter{
		config:    cfg,
		client:    client,
		connected: true,
	}
}

// Connect establishes and verifies connectivity to Argo CD.
func (a *Adapter) Connect(ctx context.Context, source store.Component) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	cfg, err := ParseConfig(source.Config)
	if err != nil {
		return fmt.Errorf("parse component config: %w", err)
	}

	// A003-W2: resolve the credential through the provider selected by the
	// component config (default static). The resolved static value overrides the
	// parsed token; an empty value leaves the legacy inline token intact, so a
	// component carrying an inline token keeps working.
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
		cfg.Token = v
	}

	a.config = cfg

	transport := &http.Transport{}
	if cfg.InsecureTLS {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	}
	a.client = &http.Client{Transport: transport}

	// Verify connectivity by listing apps (or at least reaching the API).
	if err := a.ping(ctx); err != nil {
		return fmt.Errorf("ping Argo CD at %s: %w", cfg.URL, err)
	}

	a.connected = true
	return nil
}

// Disconnect clears the adapter state.
func (a *Adapter) Disconnect() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.connected = false
	a.client = nil
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

// Apps lists Argo CD applications, optionally filtered by project.
func (a *Adapter) Apps(ctx context.Context, project string) ([]App, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	path := "/api/v1/applications"
	if project != "" {
		path += "?project=" + project
	}

	var resp struct {
		Items []appJSON `json:"items"`
	}
	if err := a.doJSON(ctx, path, &resp); err != nil {
		return nil, fmt.Errorf("list apps: %w", err)
	}

	apps := make([]App, 0, len(resp.Items))
	for _, item := range resp.Items {
		apps = append(apps, parseApp(item))
	}
	return apps, nil
}

// GetApp returns full details for one Argo CD application.
func (a *Adapter) GetApp(ctx context.Context, name string) (*AppDetail, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	var item appJSON
	if err := a.doJSON(ctx, "/api/v1/applications/"+name, &item); err != nil {
		return nil, fmt.Errorf("get app %s: %w", name, err)
	}

	app := parseApp(item)
	detail := &AppDetail{App: app}

	for _, r := range item.Status.Resources {
		health := ""
		if r.Health != nil {
			health = r.Health.Status
		}
		detail.Resources = append(detail.Resources, AppResource{
			Group:     r.Group,
			Kind:      r.Kind,
			Name:      r.Name,
			Namespace: r.Namespace,
			Status:    r.Status,
			Health:    health,
		})
	}

	for _, c := range item.Status.Conditions {
		detail.Conditions = append(detail.Conditions, AppCondition{
			Type:    c.Type,
			Message: c.Message,
		})
	}

	return detail, nil
}

// GetDiff returns the sync diff summary for an Argo CD app.
func (a *Adapter) GetDiff(ctx context.Context, name string) (*Diff, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	var item appJSON
	if err := a.doJSON(ctx, "/api/v1/applications/"+name, &item); err != nil {
		return nil, fmt.Errorf("get app diff for %s: %w", name, err)
	}

	diff := &Diff{
		Name:       name,
		SyncStatus: item.Status.Sync.Status,
		Revision:   item.Status.Sync.Revision,
	}
	if item.Status.OperationState != nil {
		diff.Message = item.Status.OperationState.Message
	}
	return diff, nil
}

// GetHistory returns the sync operation history for an Argo CD app.
func (a *Adapter) GetHistory(ctx context.Context, name string, limit int) ([]SyncOperation, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	var item appJSON
	if err := a.doJSON(ctx, "/api/v1/applications/"+name, &item); err != nil {
		return nil, fmt.Errorf("get app history for %s: %w", name, err)
	}

	history := item.Status.History
	if limit > 0 && len(history) > limit {
		// Return most recent entries (history is oldest-first).
		history = history[len(history)-limit:]
	}

	ops := make([]SyncOperation, 0, len(history))
	for i := len(history) - 1; i >= 0; i-- {
		h := history[i]
		op := SyncOperation{
			Revision:  h.Revision,
			StartedAt: h.DeployStartedAt,
		}
		if h.DeployedAt != "" {
			op.FinishedAt = h.DeployedAt
			op.Phase = "Succeeded"
		}
		if h.Source.RepoURL != "" {
			op.Initiator = h.Source.RepoURL
		}
		ops = append(ops, op)
	}
	return ops, nil
}

// --- internal helpers ---

func (a *Adapter) checkConnected() error {
	if !a.connected {
		return ErrNotConnected
	}
	return nil
}

func (a *Adapter) ping(ctx context.Context) error {
	var result struct {
		Version string `json:"Version"`
	}
	return a.doJSON(ctx, "/api/version", &result)
}

func (a *Adapter) doJSON(ctx context.Context, path string, out any) error {
	url := strings.TrimRight(a.config.URL, "/") + path

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+a.config.Token)
	req.Header.Set("Accept", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("http get %s: %w", path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("argocd API %s returned %d: %s", path, resp.StatusCode, truncate(string(body), 200))
	}

	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("decode response from %s: %w", path, err)
		}
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// --- Argo CD API JSON types (internal) ---

type appJSON struct {
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Spec struct {
		Project     string     `json:"project"`
		Source      sourceJSON `json:"source"`
		Destination struct {
			Server    string `json:"server"`
			Namespace string `json:"namespace"`
		} `json:"destination"`
	} `json:"spec"`
	Status struct {
		Health struct {
			Status string `json:"status"`
		} `json:"health"`
		Sync struct {
			Status   string `json:"status"`
			Revision string `json:"revision"`
		} `json:"sync"`
		OperationState *struct {
			Phase   string `json:"phase"`
			Message string `json:"message"`
		} `json:"operationState"`
		Resources []struct {
			Group     string `json:"group"`
			Kind      string `json:"kind"`
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
			Status    string `json:"status"`
			Health    *struct {
				Status string `json:"status"`
			} `json:"health"`
		} `json:"resources"`
		Conditions []struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"conditions"`
		History []historyJSON `json:"history"`
	} `json:"status"`
}

type sourceJSON struct {
	RepoURL        string `json:"repoURL"`
	TargetRevision string `json:"targetRevision"`
	Path           string `json:"path"`
	Chart          string `json:"chart"`
}

type historyJSON struct {
	Revision        string     `json:"revision"`
	DeployedAt      string     `json:"deployedAt"`
	DeployStartedAt string     `json:"deployStartedAt"`
	Source          sourceJSON `json:"source"`
}

func parseApp(item appJSON) App {
	return App{
		Name:        item.Metadata.Name,
		Project:     item.Spec.Project,
		Namespace:   item.Spec.Destination.Namespace,
		Cluster:     item.Spec.Destination.Server,
		SyncStatus:  item.Status.Sync.Status,
		Health:      item.Status.Health.Status,
		Revision:    item.Status.Sync.Revision,
		RepoURL:     item.Spec.Source.RepoURL,
		TargetPath:  item.Spec.Source.Path,
		TargetChart: item.Spec.Source.Chart,
	}
}
