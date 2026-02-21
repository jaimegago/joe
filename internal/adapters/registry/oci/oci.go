// Package oci provides an adapter for OCI Distribution Spec v2 compatible registries.
// This covers DockerHub, GitHub Container Registry (GHCR), Harbor, Quay, and
// any self-hosted registry that implements the OCI Distribution Spec.
package oci

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/store"
)

const (
	statusConnected    = "connected"
	statusNotConnected = "not connected"

	// OCI Distribution Spec media types.
	mediaTypeOCIManifest    = "application/vnd.oci.image.manifest.v1+json"
	mediaTypeDockerManifest = "application/vnd.docker.distribution.manifest.v2+json"
)

// httpDoer abstracts the HTTP client for testability.
type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// OCIAdapter extends the base Adapter with OCI registry operations.
type OCIAdapter interface {
	adapters.Adapter
	ListRepositories(ctx context.Context) ([]string, error)
	ListTags(ctx context.Context, repo string) ([]string, error)
	GetManifest(ctx context.Context, repo, reference string) (*Manifest, error)
}

// Manifest holds key metadata extracted from an OCI image manifest.
type Manifest struct {
	// Digest is the content-addressable digest, e.g. "sha256:abc123...".
	Digest string `json:"digest"`

	// MediaType identifies the manifest format.
	MediaType string `json:"media_type"`

	// Labels from the image config, including OCI annotation labels such as
	// "org.opencontainers.image.revision" (git commit SHA).
	Labels map[string]string `json:"labels,omitempty"`

	// CreatedAt is the image creation timestamp from the config, if present.
	CreatedAt string `json:"created_at,omitempty"`
}

// Adapter implements OCIAdapter.
type Adapter struct {
	mu         sync.RWMutex
	config     Config
	httpClient httpDoer
	connected  bool
}

// New creates a new OCI adapter (not yet connected).
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

// Connect establishes connectivity to the OCI registry by probing the /v2/ endpoint.
func (a *Adapter) Connect(_ context.Context, source store.Source) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	cfg, err := ParseConfig(source.Config)
	if err != nil {
		return fmt.Errorf("parse oci source config: %w", err)
	}
	a.config = cfg

	transport := http.DefaultTransport
	if cfg.SkipTLSVerify {
		transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		}
	}
	a.httpClient = &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}

	// Probe the v2 endpoint to confirm registry is reachable.
	req, err := http.NewRequest(http.MethodGet, cfg.RegistryURL+"/v2/", nil)
	if err != nil {
		return fmt.Errorf("build /v2/ probe request: %w", err)
	}
	a.addAuthHeader(req)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("probe oci registry %s: %w", cfg.RegistryURL, err)
	}
	resp.Body.Close()

	// 200 = unauthenticated allowed; 401 = auth required but registry is live.
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusUnauthorized {
		return fmt.Errorf("unexpected status from /v2/: %d", resp.StatusCode)
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
		return adapters.Status{Connected: true, Message: fmt.Sprintf("%s (%s)", statusConnected, a.config.RegistryURL)}
	}
	return adapters.Status{Connected: false, Message: statusNotConnected}
}

// ListRepositories returns all repository names via the /v2/_catalog endpoint.
// Handles Link-header pagination per the OCI Distribution Spec.
func (a *Adapter) ListRepositories(ctx context.Context) ([]string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	var repos []string
	next := a.config.RegistryURL + "/v2/_catalog"

	for next != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, next, nil)
		if err != nil {
			return nil, fmt.Errorf("build catalog request: %w", err)
		}
		a.addAuthHeader(req)

		resp, err := a.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("get catalog: %w", err)
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("read catalog response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("catalog returned status %d: %s", resp.StatusCode, body)
		}

		var page struct {
			Repositories []string `json:"repositories"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("decode catalog response: %w", err)
		}
		repos = append(repos, page.Repositories...)

		// Follow Link header for pagination.
		next = parseLinkHeader(resp.Header.Get("Link"), a.config.RegistryURL)
	}

	return repos, nil
}

// ListTags returns the list of tags for a given repository.
func (a *Adapter) ListTags(ctx context.Context, repo string) ([]string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/v2/%s/tags/list", a.config.RegistryURL, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build tags request: %w", err)
	}
	a.addAuthHeader(req)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get tags for %s: %w", repo, err)
	}

	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("read tags response: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("repository %q not found", repo)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tags returned status %d: %s", resp.StatusCode, body)
	}

	var result struct {
		Tags []string `json:"tags"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode tags response: %w", err)
	}
	return result.Tags, nil
}

// GetManifest retrieves the manifest for a given repo and reference (tag or digest).
// It extracts the content digest, labels, and creation timestamp from the image config.
func (a *Adapter) GetManifest(ctx context.Context, repo, reference string) (*Manifest, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/v2/%s/manifests/%s", a.config.RegistryURL, repo, reference)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build manifest request: %w", err)
	}
	a.addAuthHeader(req)
	req.Header.Set("Accept", strings.Join([]string{
		mediaTypeOCIManifest,
		mediaTypeDockerManifest,
	}, ", "))

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get manifest for %s:%s: %w", repo, reference, err)
	}

	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("read manifest response: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("manifest %s:%s not found", repo, reference)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("manifest returned status %d: %s", resp.StatusCode, body)
	}

	manifest := &Manifest{
		Digest:    resp.Header.Get("Docker-Content-Digest"),
		MediaType: resp.Header.Get("Content-Type"),
	}

	// Parse the manifest to find config labels / annotations.
	// OCI manifests point to a config blob that contains the Labels map.
	// For simplicity, extract OCI annotations from the manifest JSON itself.
	var raw struct {
		Annotations map[string]string `json:"annotations"`
		Config      struct {
			Digest string `json:"digest"`
		} `json:"config"`
	}
	if err := json.Unmarshal(body, &raw); err == nil {
		if len(raw.Annotations) > 0 {
			manifest.Labels = raw.Annotations
		}
	}

	return manifest, nil
}

// checkConnected returns an error if the adapter is not connected.
func (a *Adapter) checkConnected() error {
	if !a.connected {
		return fmt.Errorf("oci adapter not connected")
	}
	return nil
}

// addAuthHeader sets the Authorization header using Basic auth credentials if configured.
func (a *Adapter) addAuthHeader(req *http.Request) {
	if a.config.Username == "" && a.config.Password == "" {
		return
	}
	creds := base64.StdEncoding.EncodeToString([]byte(a.config.Username + ":" + a.config.Password))
	req.Header.Set("Authorization", "Basic "+creds)
}

// parseLinkHeader extracts the next URL from an OCI Distribution Spec Link header.
// The header format is: </v2/_catalog?last=foo&n=100>; rel="next"
// baseURL is prepended if the link is a relative path.
func parseLinkHeader(header, baseURL string) string {
	if header == "" {
		return ""
	}
	// Split on comma for multiple links, find rel="next".
	parts := strings.Split(header, ",")
	for _, part := range parts {
		segments := strings.Split(strings.TrimSpace(part), ";")
		if len(segments) < 2 {
			continue
		}
		rel := strings.TrimSpace(segments[1])
		if !strings.Contains(rel, `rel="next"`) {
			continue
		}
		// Extract URL from angle brackets.
		urlPart := strings.TrimSpace(segments[0])
		urlPart = strings.Trim(urlPart, "<>")
		if strings.HasPrefix(urlPart, "/") {
			return baseURL + urlPart
		}
		return urlPart
	}
	return ""
}
