// Package github provides a GitHub adapter for PR read/comment operations.
package github

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
	"github.com/jaimegago/joe/internal/store"
)

// PRInfo holds metadata about a GitHub pull request.
type PRInfo struct {
	Number    int    `json:"number"`
	Title     string `json:"title"`
	State     string `json:"state"`
	HeadSHA   string `json:"head_sha"`
	BaseSHA   string `json:"base_sha"`
	HeadRef   string `json:"head_ref"`
	BaseRef   string `json:"base_ref"`
	Author    string `json:"author"`
	URL       string `json:"url"`
	RepoOwner string `json:"repo_owner"`
	RepoName  string `json:"repo_name"`
}

// GitHubAdapter extends the base Adapter with GitHub PR operations.
type GitHubAdapter interface {
	WebhookSecret() string
	adapters.Adapter
	GetPR(ctx context.Context, owner, repo string, number int) (*PRInfo, error)
	GetPRDiff(ctx context.Context, owner, repo string, number int) (string, error)
	PostComment(ctx context.Context, owner, repo string, number int, body string) error
	RequestChanges(ctx context.Context, owner, repo string, number int, body string) error
	ListPRs(ctx context.Context, owner, repo string, state string) ([]*PRInfo, error)
}

// Adapter is the concrete GitHub adapter using the GitHub REST API v3.
type Adapter struct {
	mu         sync.RWMutex
	config     Config
	httpClient *http.Client
	connected  bool
}

// New creates a new unconnected GitHub adapter.
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

func (a *Adapter) Connect(_ context.Context, source store.Component) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	cfg, err := ParseConfig(source.Config)
	if err != nil {
		return fmt.Errorf("parse source config: %w", err)
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

// WebhookSecret returns the configured HMAC secret for webhook verification.
func (a *Adapter) WebhookSecret() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.config.WebhookSecret
}

func (a *Adapter) GetPR(ctx context.Context, owner, repo string, number int) (*PRInfo, error) {
	a.mu.RLock()
	cfg := a.config
	a.mu.RUnlock()

	path := fmt.Sprintf("/repos/%s/%s/pulls/%d", owner, repo, number)
	var raw struct {
		Number  int    `json:"number"`
		Title   string `json:"title"`
		State   string `json:"state"`
		HTMLURL string `json:"html_url"`
		Head    struct {
			SHA string `json:"sha"`
			Ref string `json:"ref"`
		} `json:"head"`
		Base struct {
			SHA string `json:"sha"`
			Ref string `json:"ref"`
		} `json:"base"`
		User struct {
			Login string `json:"login"`
		} `json:"user"`
	}
	if err := a.get(ctx, cfg, path, "application/vnd.github+json", &raw); err != nil {
		return nil, err
	}
	return &PRInfo{
		Number:    raw.Number,
		Title:     raw.Title,
		State:     raw.State,
		HeadSHA:   raw.Head.SHA,
		HeadRef:   raw.Head.Ref,
		BaseSHA:   raw.Base.SHA,
		BaseRef:   raw.Base.Ref,
		Author:    raw.User.Login,
		URL:       raw.HTMLURL,
		RepoOwner: owner,
		RepoName:  repo,
	}, nil
}

func (a *Adapter) GetPRDiff(ctx context.Context, owner, repo string, number int) (string, error) {
	a.mu.RLock()
	cfg := a.config
	a.mu.RUnlock()

	path := fmt.Sprintf("/repos/%s/%s/pulls/%d", owner, repo, number)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.BaseURL+path, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	req.Header.Set("Accept", "application/vnd.github.v3.diff")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("github diff request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read diff body: %w", err)
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("github diff %s: status %d: %s", path, resp.StatusCode, string(body))
	}
	return string(body), nil
}

func (a *Adapter) PostComment(ctx context.Context, owner, repo string, number int, body string) error {
	a.mu.RLock()
	cfg := a.config
	a.mu.RUnlock()

	path := fmt.Sprintf("/repos/%s/%s/issues/%d/comments", owner, repo, number)
	payload := map[string]string{"body": body}
	return a.post(ctx, cfg, path, payload)
}

func (a *Adapter) RequestChanges(ctx context.Context, owner, repo string, number int, body string) error {
	a.mu.RLock()
	cfg := a.config
	a.mu.RUnlock()

	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/reviews", owner, repo, number)
	payload := map[string]string{
		"body":  body,
		"event": "REQUEST_CHANGES",
	}
	return a.post(ctx, cfg, path, payload)
}

func (a *Adapter) ListPRs(ctx context.Context, owner, repo string, state string) ([]*PRInfo, error) {
	a.mu.RLock()
	cfg := a.config
	a.mu.RUnlock()

	if state == "" {
		state = "open"
	}
	path := fmt.Sprintf("/repos/%s/%s/pulls?state=%s&per_page=50", owner, repo, state)
	var raw []struct {
		Number  int    `json:"number"`
		Title   string `json:"title"`
		State   string `json:"state"`
		HTMLURL string `json:"html_url"`
		Head    struct {
			SHA string `json:"sha"`
			Ref string `json:"ref"`
		} `json:"head"`
		Base struct {
			SHA string `json:"sha"`
			Ref string `json:"ref"`
		} `json:"base"`
		User struct {
			Login string `json:"login"`
		} `json:"user"`
	}
	if err := a.get(ctx, cfg, path, "application/vnd.github+json", &raw); err != nil {
		return nil, err
	}
	prs := make([]*PRInfo, 0, len(raw))
	for _, r := range raw {
		prs = append(prs, &PRInfo{
			Number:    r.Number,
			Title:     r.Title,
			State:     r.State,
			HeadSHA:   r.Head.SHA,
			HeadRef:   r.Head.Ref,
			BaseSHA:   r.Base.SHA,
			BaseRef:   r.Base.Ref,
			Author:    r.User.Login,
			URL:       r.HTMLURL,
			RepoOwner: owner,
			RepoName:  repo,
		})
	}
	return prs, nil
}

// get performs a JSON GET request against the GitHub API.
func (a *Adapter) get(ctx context.Context, cfg Config, path, accept string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.BaseURL+path, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	req.Header.Set("Accept", accept)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("github request %s: %w", path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("github %s: status %d: %s", path, resp.StatusCode, string(body))
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// post performs a JSON POST request against the GitHub API.
func (a *Adapter) post(ctx context.Context, cfg Config, path string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.BaseURL+path, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("github post %s: %w", path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("github post %s: status %d: %s", path, resp.StatusCode, string(body))
	}
	return nil
}
