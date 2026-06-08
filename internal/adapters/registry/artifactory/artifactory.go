// Package artifactory provides an adapter for JFrog Artifactory.
// It supports listing Docker and Helm repositories, Docker image tags,
// and artifact metadata via the Artifactory REST API.
package artifactory

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/store"
	"sync"
)

const (
	statusConnected    = "connected"
	statusNotConnected = "not connected"
)

// httpDoer abstracts the HTTP client for testability.
type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// ArtifactoryAdapter extends the base Adapter with Artifactory operations.
type ArtifactoryAdapter interface {
	adapters.Adapter
	ListRepositories(ctx context.Context) ([]Repository, error)
	ListDockerTags(ctx context.Context, repo, image string) ([]string, error)
	GetArtifactInfo(ctx context.Context, repo, path string) (*ArtifactInfo, error)
}

// Repository describes an Artifactory repository.
type Repository struct {
	Key         string `json:"key"`
	Type        string `json:"type"`        // "LOCAL", "REMOTE", "VIRTUAL"
	PackageType string `json:"packageType"` // "Docker", "Helm", "Generic"
	Description string `json:"description"`
}

// ArtifactInfo holds metadata for a specific artifact.
type ArtifactInfo struct {
	Repo      string            `json:"repo"`
	Path      string            `json:"path"`
	Created   string            `json:"created"`
	Modified  string            `json:"modified"`
	Checksums map[string]string `json:"checksums,omitempty"`
}

// Adapter implements ArtifactoryAdapter.
type Adapter struct {
	mu         sync.RWMutex
	config     Config
	httpClient httpDoer
	connected  bool
}

// New creates a new Artifactory adapter (not yet connected).
func New() *Adapter {
	return &Adapter{}
}

// NewWithClient creates an adapter with an injected HTTP client (for testing).
func NewWithClient(client httpDoer, cfg Config) *Adapter {
	return &Adapter{
		config:     cfg,
		httpClient: client,
		connected:  true,
	}
}

// Connect establishes connectivity by probing the Artifactory ping endpoint.
func (a *Adapter) Connect(_ context.Context, source store.Component) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	cfg, err := ParseConfig(source.Config)
	if err != nil {
		return fmt.Errorf("parse artifactory source config: %w", err)
	}
	a.config = cfg
	a.httpClient = &http.Client{Timeout: 30 * time.Second}

	req, err := http.NewRequest(http.MethodGet, cfg.BaseURL+"/api/system/ping", nil)
	if err != nil {
		return fmt.Errorf("build ping request: %w", err)
	}
	a.setAuthHeader(req)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("ping artifactory %s: %w", cfg.BaseURL, err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("artifactory ping returned status %d: %s", resp.StatusCode, body)
	}
	a.connected = true
	return nil
}

// Disconnect clears the adapter state.
func (a *Adapter) Disconnect() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.connected = false
	a.httpClient = nil
	return nil
}

// Status returns the current connection status.
func (a *Adapter) Status() adapters.Status {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.connected {
		return adapters.Status{Connected: true, Message: fmt.Sprintf("%s (%s)", statusConnected, a.config.BaseURL)}
	}
	return adapters.Status{Connected: false, Message: statusNotConnected}
}

// ListRepositories returns local Docker and Helm repositories.
// If Config.Repositories is set, only those keys are returned.
func (a *Adapter) ListRepositories(ctx context.Context) ([]Repository, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	url := a.config.BaseURL + "/api/repositories?type=local"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build repositories request: %w", err)
	}
	a.setAuthHeader(req)
	req.Header.Set("Accept", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list artifactory repositories: %w", err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("read repositories response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("repositories returned status %d: %s", resp.StatusCode, body)
	}

	var all []Repository
	if err := json.Unmarshal(body, &all); err != nil {
		return nil, fmt.Errorf("decode repositories response: %w", err)
	}

	// Filter by package type and optional key allowlist.
	allowed := make(map[string]bool, len(a.config.Repositories))
	for _, k := range a.config.Repositories {
		allowed[k] = true
	}

	var result []Repository
	for _, r := range all {
		if r.PackageType != "Docker" && r.PackageType != "Helm" {
			continue
		}
		if len(allowed) > 0 && !allowed[r.Key] {
			continue
		}
		result = append(result, r)
	}
	return result, nil
}

// ListDockerTags returns the available tags for a Docker image in a repository.
func (a *Adapter) ListDockerTags(ctx context.Context, repo, image string) ([]string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/api/docker/%s/v2/%s/tags/list", a.config.BaseURL, repo, image)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build docker tags request: %w", err)
	}
	a.setAuthHeader(req)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list docker tags for %s/%s: %w", repo, image, err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("read docker tags response: %w", err)
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("image %s/%s not found", repo, image)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("docker tags returned status %d: %s", resp.StatusCode, body)
	}

	var result struct {
		Tags []string `json:"tags"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode docker tags response: %w", err)
	}
	return result.Tags, nil
}

// GetArtifactInfo retrieves metadata for a specific artifact path in a repository.
func (a *Adapter) GetArtifactInfo(ctx context.Context, repo, path string) (*ArtifactInfo, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/api/storage/%s/%s", a.config.BaseURL, repo, path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build artifact info request: %w", err)
	}
	a.setAuthHeader(req)
	req.Header.Set("Accept", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get artifact info for %s/%s: %w", repo, path, err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("read artifact info response: %w", err)
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("artifact %s/%s not found", repo, path)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("artifact info returned status %d: %s", resp.StatusCode, body)
	}

	var info ArtifactInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("decode artifact info response: %w", err)
	}
	return &info, nil
}

// checkConnected returns an error if the adapter is not connected.
func (a *Adapter) checkConnected() error {
	if !a.connected {
		return fmt.Errorf("artifactory adapter not connected")
	}
	return nil
}

// setAuthHeader sets the authentication header on r.
// Prefers X-JFrog-Art-Api (API key) over Basic auth.
func (a *Adapter) setAuthHeader(r *http.Request) {
	if a.config.APIKey != "" {
		r.Header.Set("X-JFrog-Art-Api", a.config.APIKey)
		return
	}
	if a.config.Username != "" {
		r.SetBasicAuth(a.config.Username, "")
	}
}
