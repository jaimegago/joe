// Package gitlab provides a GitLab adapter for MR read/comment operations.
package gitlab

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/credential"
	"github.com/jaimegago/joe/internal/store"
)

// MRInfo holds metadata about a GitLab merge request.
type MRInfo struct {
	IID          int    `json:"iid"`
	Title        string `json:"title"`
	State        string `json:"state"`
	SHA          string `json:"sha"`
	SourceBranch string `json:"source_branch"`
	TargetBranch string `json:"target_branch"`
	Author       string `json:"author"`
	WebURL       string `json:"web_url"`
	ProjectID    string `json:"project_id"`
}

// GitLabAdapter extends the base Adapter with GitLab MR operations.
type GitLabAdapter interface {
	WebhookSecret() string
	adapters.Adapter
	GetMR(ctx context.Context, projectID string, iid int) (*MRInfo, error)
	GetMRDiff(ctx context.Context, projectID string, iid int) (string, error)
	PostNote(ctx context.Context, projectID string, iid int, body string) error
	RequestChanges(ctx context.Context, projectID string, iid int, body string) error
	ListMRs(ctx context.Context, projectID string, state string) ([]*MRInfo, error)
}

// Adapter is the concrete GitLab adapter using the GitLab REST API v4.
type Adapter struct {
	mu         sync.RWMutex
	config     Config
	httpClient *http.Client
	connected  bool
}

// New creates a new unconnected GitLab adapter.
func New() *Adapter {
	return &Adapter{
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// NewWithConfig creates a pre-connected adapter (for testing).
func NewWithConfig(cfg Config, httpClient *http.Client) *Adapter {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Adapter{
		config:     cfg,
		httpClient: httpClient,
		connected:  true,
	}
}

func (a *Adapter) Connect(ctx context.Context, source store.Component) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	cfg, err := ParseConfig(source.Config)
	if err != nil {
		return fmt.Errorf("parse source config: %w", err)
	}

	// D-0026 unit 2 (A003-W1): route the credential through the provider selected
	// by the component config. The resolved static value becomes the PRIVATE-TOKEN
	// the per-request snapshot reads. A config without a discriminator selects the
	// static provider, which yields no value for the legacy "token" field — so
	// existing components keep their current behavior.
	provider, err := credential.Select(source.Config)
	if err != nil {
		return fmt.Errorf("select credential provider: %w", err)
	}
	res, err := provider.Resolve(ctx, source.ID, source.Config)
	if err != nil {
		return fmt.Errorf("resolve credential: %w", err)
	}
	if !res.Diagnostic.OK {
		// Non-sensitive reason only; the credential value never enters this error.
		return fmt.Errorf("resolve credential: %s", res.Diagnostic.Reason)
	}
	if token, ok := res.StaticValue(); ok && token != "" {
		cfg.Token = token
	}

	a.config = cfg
	a.connected = true
	return nil
}

func (a *Adapter) Disconnect() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.connected = false
	return nil
}

func (a *Adapter) Status() adapters.Status {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if !a.connected {
		return adapters.Status{Connected: false, Message: "not connected"}
	}
	return adapters.Status{Connected: true}
}

// WebhookSecret returns the configured token for X-Gitlab-Token webhook verification.
func (a *Adapter) WebhookSecret() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.config.WebhookSecret
}

func (a *Adapter) GetMR(ctx context.Context, projectID string, iid int) (*MRInfo, error) {
	a.mu.RLock()
	cfg := a.config
	a.mu.RUnlock()

	path := fmt.Sprintf("/api/v4/projects/%s/merge_requests/%d", projectID, iid)
	var raw struct {
		IID          int    `json:"iid"`
		Title        string `json:"title"`
		State        string `json:"state"`
		SHA          string `json:"sha"`
		SourceBranch string `json:"source_branch"`
		TargetBranch string `json:"target_branch"`
		WebURL       string `json:"web_url"`
		Author       struct {
			Username string `json:"username"`
		} `json:"author"`
	}
	if err := a.get(ctx, cfg, path, &raw); err != nil {
		return nil, err
	}
	return &MRInfo{
		IID:          raw.IID,
		Title:        raw.Title,
		State:        raw.State,
		SHA:          raw.SHA,
		SourceBranch: raw.SourceBranch,
		TargetBranch: raw.TargetBranch,
		Author:       raw.Author.Username,
		WebURL:       raw.WebURL,
		ProjectID:    projectID,
	}, nil
}

func (a *Adapter) GetMRDiff(ctx context.Context, projectID string, iid int) (string, error) {
	a.mu.RLock()
	cfg := a.config
	a.mu.RUnlock()

	path := fmt.Sprintf("/api/v4/projects/%s/merge_requests/%d/diffs", projectID, iid)
	var raw []struct {
		Diff    string `json:"diff"`
		OldPath string `json:"old_path"`
		NewPath string `json:"new_path"`
	}
	if err := a.get(ctx, cfg, path, &raw); err != nil {
		return "", err
	}
	var sb bytes.Buffer
	for _, f := range raw {
		if f.Diff != "" {
			fmt.Fprintf(&sb, "--- %s\n+++ %s\n%s\n", f.OldPath, f.NewPath, f.Diff)
		}
	}
	return sb.String(), nil
}

func (a *Adapter) PostNote(ctx context.Context, projectID string, iid int, body string) error {
	a.mu.RLock()
	cfg := a.config
	a.mu.RUnlock()

	path := fmt.Sprintf("/api/v4/projects/%s/merge_requests/%d/notes", projectID, iid)
	payload := map[string]string{"body": body}
	return a.post(ctx, cfg, path, payload)
}

func (a *Adapter) RequestChanges(ctx context.Context, projectID string, iid int, body string) error {
	// GitLab does not have a native "request changes" review state for MRs in all tiers.
	// We post a note with a prefix to indicate this.
	return a.PostNote(ctx, projectID, iid, "**Changes Requested:**\n\n"+body)
}

func (a *Adapter) ListMRs(ctx context.Context, projectID string, state string) ([]*MRInfo, error) {
	a.mu.RLock()
	cfg := a.config
	a.mu.RUnlock()

	if state == "" {
		state = "opened"
	}
	path := fmt.Sprintf("/api/v4/projects/%s/merge_requests?state=%s&per_page=50", projectID, state)
	var raw []struct {
		IID          int    `json:"iid"`
		Title        string `json:"title"`
		State        string `json:"state"`
		SHA          string `json:"sha"`
		SourceBranch string `json:"source_branch"`
		TargetBranch string `json:"target_branch"`
		WebURL       string `json:"web_url"`
		Author       struct {
			Username string `json:"username"`
		} `json:"author"`
	}
	if err := a.get(ctx, cfg, path, &raw); err != nil {
		return nil, err
	}
	mrs := make([]*MRInfo, 0, len(raw))
	for _, r := range raw {
		mrs = append(mrs, &MRInfo{
			IID:          r.IID,
			Title:        r.Title,
			State:        r.State,
			SHA:          r.SHA,
			SourceBranch: r.SourceBranch,
			TargetBranch: r.TargetBranch,
			Author:       r.Author.Username,
			WebURL:       r.WebURL,
			ProjectID:    projectID,
		})
	}
	return mrs, nil
}

// get performs a JSON GET request against the GitLab API.
func (a *Adapter) get(ctx context.Context, cfg Config, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.BaseURL+path, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("PRIVATE-TOKEN", cfg.Token)
	req.Header.Set("Accept", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("gitlab request %s: %w", path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("gitlab %s: status %d: %s", path, resp.StatusCode, string(body))
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// post performs a JSON POST request against the GitLab API.
func (a *Adapter) post(ctx context.Context, cfg Config, path string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.BaseURL+path, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("PRIVATE-TOKEN", cfg.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("gitlab post %s: %w", path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("gitlab post %s: status %d: %s", path, resp.StatusCode, string(body))
	}
	return nil
}
