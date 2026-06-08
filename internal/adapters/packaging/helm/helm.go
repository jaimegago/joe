package helm

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/store"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

const (
	statusNotConnected = "Not connected to Helm"
	statusConnectedFmt = "Connected to Helm (K8s cluster via %s)"

	helmOwnerLabel   = "owner"
	helmOwnerValue   = "helm"
	helmNameLabel    = "name"
	helmStatusLabel  = "status"
	helmVersionLabel = "version"
)

// ErrNotConnected indicates the adapter is not connected.
var ErrNotConnected = errors.New("adapter not connected to Helm")

// Release is a summary of one Helm release.
type Release struct {
	Name         string `json:"name"`
	Namespace    string `json:"namespace"`
	Chart        string `json:"chart"`
	ChartVersion string `json:"chart_version"`
	AppVersion   string `json:"app_version,omitempty"`
	Status       string `json:"status"`
	Revision     int    `json:"revision"`
	Updated      string `json:"updated"`
}

// ReleaseDetail includes the full release values and notes.
type ReleaseDetail struct {
	Release Release        `json:"release"`
	Values  map[string]any `json:"values"`
	Notes   string         `json:"notes,omitempty"`
}

// RevisionEntry is one entry in a release's revision history.
type RevisionEntry struct {
	Revision    int    `json:"revision"`
	Status      string `json:"status"`
	Chart       string `json:"chart"`
	AppVersion  string `json:"app_version,omitempty"`
	Updated     string `json:"updated"`
	Description string `json:"description,omitempty"`
}

// secretsLister abstracts the K8s secrets API for testing.
type secretsLister interface {
	listSecrets(ctx context.Context, namespace string, opts metav1.ListOptions) (*corev1.SecretList, error)
}

// k8sSecretsLister wraps a real kubernetes.Interface.
type k8sSecretsLister struct {
	client kubernetes.Interface
}

func (k *k8sSecretsLister) listSecrets(ctx context.Context, namespace string, opts metav1.ListOptions) (*corev1.SecretList, error) {
	return k.client.CoreV1().Secrets(namespace).List(ctx, opts)
}

// HelmAdapter extends the base Adapter with Helm operations.
type HelmAdapter interface {
	adapters.Adapter
	Releases(ctx context.Context, namespace string) ([]Release, error)
	GetRelease(ctx context.Context, namespace, name string) (*ReleaseDetail, error)
	History(ctx context.Context, namespace, name string, limit int) ([]RevisionEntry, error)
}

// Adapter is the concrete Helm adapter.
type Adapter struct {
	mu        sync.RWMutex
	config    Config
	configSrc string // kubeconfig source description for status
	lister    secretsLister
	connected bool
}

// New creates a new Helm adapter (not yet connected).
func New() *Adapter {
	return &Adapter{}
}

// NewWithLister creates an adapter with an injected secrets lister (for testing).
func NewWithLister(lister secretsLister, cfg Config) *Adapter {
	return &Adapter{
		config:    cfg,
		configSrc: "injected",
		lister:    lister,
		connected: true,
	}
}

// Connect parses and stores the source config. The K8s client is built lazily
// on first use so that Connect succeeds even when no kubeconfig is available yet.
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

// ensureLister lazily builds the K8s client on first operation.
func (a *Adapter) ensureLister() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.initLister()
}

// initLister builds the K8s client if it has not been initialised yet.
// Must be called with a.mu held for writing.
func (a *Adapter) initLister() error {
	if a.lister != nil {
		return nil
	}
	restConfig, src, err := buildRESTConfig(a.config)
	if err != nil {
		return fmt.Errorf("build kubeconfig: %w", err)
	}
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("create kubernetes client: %w", err)
	}
	a.lister = &k8sSecretsLister{client: clientset}
	a.configSrc = src
	return nil
}

// Disconnect clears the adapter state.
func (a *Adapter) Disconnect() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.connected = false
	a.lister = nil
	return nil
}

// Status returns the current connection status.
func (a *Adapter) Status() adapters.Status {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.connected {
		return adapters.Status{
			Connected: true,
			Message:   fmt.Sprintf(statusConnectedFmt, a.configSrc),
		}
	}
	return adapters.Status{Connected: false, Message: statusNotConnected}
}

// Releases lists all Helm releases, optionally filtered by namespace.
// Passing an empty namespace string lists across all namespaces.
func (a *Adapter) Releases(ctx context.Context, namespace string) ([]Release, error) {
	a.mu.RLock()
	if err := a.checkConnected(); err != nil {
		a.mu.RUnlock()
		return nil, err
	}
	a.mu.RUnlock()

	if err := a.ensureLister(); err != nil {
		return nil, err
	}

	a.mu.RLock()
	defer a.mu.RUnlock()

	secrets, err := a.lister.listSecrets(ctx, namespace, metav1.ListOptions{
		LabelSelector: helmOwnerLabel + "=" + helmOwnerValue,
	})
	if err != nil {
		return nil, fmt.Errorf("list helm secrets: %w", err)
	}

	// Collect latest revision per release.
	latest := make(map[string]*corev1.Secret)
	for i := range secrets.Items {
		s := &secrets.Items[i]
		relName := s.Labels[helmNameLabel]
		if relName == "" {
			continue
		}
		key := s.Namespace + "/" + relName
		existing, ok := latest[key]
		if !ok || revision(s) > revision(existing) {
			latest[key] = s
		}
	}

	var releases []Release
	for _, s := range latest {
		rel, err := releaseFromSecret(s)
		if err != nil {
			continue // skip undecodable secrets
		}
		releases = append(releases, *rel)
	}

	sort.Slice(releases, func(i, j int) bool {
		return releases[i].Name < releases[j].Name
	})
	return releases, nil
}

// GetRelease returns full details for one Helm release.
func (a *Adapter) GetRelease(ctx context.Context, namespace, name string) (*ReleaseDetail, error) {
	a.mu.RLock()
	if err := a.checkConnected(); err != nil {
		a.mu.RUnlock()
		return nil, err
	}
	a.mu.RUnlock()

	if err := a.ensureLister(); err != nil {
		return nil, err
	}

	a.mu.RLock()
	defer a.mu.RUnlock()

	secrets, err := a.lister.listSecrets(ctx, namespace, metav1.ListOptions{
		LabelSelector: helmOwnerLabel + "=" + helmOwnerValue + "," + helmNameLabel + "=" + name,
	})
	if err != nil {
		return nil, fmt.Errorf("list helm secrets for %s/%s: %w", namespace, name, err)
	}

	if len(secrets.Items) == 0 {
		return nil, fmt.Errorf("helm release %s/%s not found", namespace, name)
	}

	// Find latest revision.
	var latest *corev1.Secret
	for i := range secrets.Items {
		s := &secrets.Items[i]
		if latest == nil || revision(s) > revision(latest) {
			latest = s
		}
	}

	raw, err := decodeHelmSecret(latest)
	if err != nil {
		return nil, fmt.Errorf("decode helm release: %w", err)
	}

	rel, err := releaseFromSecret(latest)
	if err != nil {
		return nil, err
	}

	detail := &ReleaseDetail{
		Release: *rel,
		Values:  raw.Config,
		Notes:   raw.Info.Notes,
	}
	return detail, nil
}

// History returns the revision history for a Helm release.
func (a *Adapter) History(ctx context.Context, namespace, name string, limit int) ([]RevisionEntry, error) {
	a.mu.RLock()
	if err := a.checkConnected(); err != nil {
		a.mu.RUnlock()
		return nil, err
	}
	a.mu.RUnlock()

	if err := a.ensureLister(); err != nil {
		return nil, err
	}

	a.mu.RLock()
	defer a.mu.RUnlock()

	secrets, err := a.lister.listSecrets(ctx, namespace, metav1.ListOptions{
		LabelSelector: helmOwnerLabel + "=" + helmOwnerValue + "," + helmNameLabel + "=" + name,
	})
	if err != nil {
		return nil, fmt.Errorf("list helm secrets for history %s/%s: %w", namespace, name, err)
	}

	if len(secrets.Items) == 0 {
		return nil, fmt.Errorf("helm release %s/%s not found", namespace, name)
	}

	var entries []RevisionEntry
	for i := range secrets.Items {
		s := &secrets.Items[i]
		raw, err := decodeHelmSecret(s)
		if err != nil {
			continue
		}
		entries = append(entries, RevisionEntry{
			Revision:    raw.Version,
			Status:      raw.Info.Status,
			Chart:       chartName(raw),
			AppVersion:  raw.Chart.Metadata.AppVersion,
			Updated:     raw.Info.LastDeployed,
			Description: raw.Info.Description,
		})
	}

	// Sort newest first.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Revision > entries[j].Revision
	})

	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}
	return entries, nil
}

// --- internal helpers ---

func (a *Adapter) checkConnected() error {
	if !a.connected {
		return ErrNotConnected
	}
	return nil
}

// helmReleaseJSON is the decoded Helm release object stored in K8s secrets.
type helmReleaseJSON struct {
	Name    string `json:"name"`
	Version int    `json:"version"`
	Info    struct {
		Status        string `json:"status"`
		FirstDeployed string `json:"first_deployed"`
		LastDeployed  string `json:"last_deployed"`
		Notes         string `json:"notes"`
		Description   string `json:"description"`
	} `json:"info"`
	Chart struct {
		Metadata struct {
			Name       string `json:"name"`
			Version    string `json:"version"`
			AppVersion string `json:"appVersion"`
		} `json:"metadata"`
	} `json:"chart"`
	Config    map[string]any `json:"config"`
	Namespace string         `json:"namespace"`
}

func decodeHelmSecret(s *corev1.Secret) (*helmReleaseJSON, error) {
	data, ok := s.Data["release"]
	if !ok {
		return nil, errors.New("no release key in secret")
	}

	// Helm 3: base64(gzip(json))
	decoded, err := base64.StdEncoding.DecodeString(string(data))
	if err != nil {
		// Some versions double-encode.
		decoded, err = base64.StdEncoding.DecodeString(
			strings.TrimRight(string(data), "\n"),
		)
		if err != nil {
			return nil, fmt.Errorf("base64 decode: %w", err)
		}
	}

	gr, err := gzip.NewReader(bytes.NewReader(decoded))
	if err != nil {
		return nil, fmt.Errorf("gzip reader: %w", err)
	}
	defer gr.Close()

	jsonData, err := io.ReadAll(gr)
	if err != nil {
		return nil, fmt.Errorf("gunzip: %w", err)
	}

	var rel helmReleaseJSON
	if err := json.Unmarshal(jsonData, &rel); err != nil {
		return nil, fmt.Errorf("json unmarshal: %w", err)
	}
	return &rel, nil
}

func releaseFromSecret(s *corev1.Secret) (*Release, error) {
	name := s.Labels[helmNameLabel]
	status := s.Labels[helmStatusLabel]
	rev := revision(s)

	raw, err := decodeHelmSecret(s)
	if err != nil {
		// Fallback: use label data only.
		return &Release{
			Name:      name,
			Namespace: s.Namespace,
			Status:    status,
			Revision:  rev,
		}, nil
	}

	return &Release{
		Name:         raw.Name,
		Namespace:    raw.Namespace,
		Chart:        raw.Chart.Metadata.Name,
		ChartVersion: raw.Chart.Metadata.Version,
		AppVersion:   raw.Chart.Metadata.AppVersion,
		Status:       raw.Info.Status,
		Revision:     raw.Version,
		Updated:      raw.Info.LastDeployed,
	}, nil
}

func revision(s *corev1.Secret) int {
	v, _ := strconv.Atoi(s.Labels[helmVersionLabel])
	return v
}

func chartName(r *helmReleaseJSON) string {
	if r.Chart.Metadata.Name == "" {
		return ""
	}
	return r.Chart.Metadata.Name + "-" + r.Chart.Metadata.Version
}

func buildRESTConfig(cfg Config) (*rest.Config, string, error) {
	if cfg.KubeconfigPath != "" {
		rc, err := clientcmd.BuildConfigFromFlags("", cfg.KubeconfigPath)
		if err != nil {
			return nil, "", fmt.Errorf("load kubeconfig %s: %w", cfg.KubeconfigPath, err)
		}
		return rc, cfg.KubeconfigPath, nil
	}

	// Try in-cluster config first.
	rc, err := rest.InClusterConfig()
	if err == nil {
		return rc, "in-cluster", nil
	}

	// Fall back to default kubeconfig location.
	home := homedir.HomeDir()
	defaultPath := filepath.Join(home, ".kube", "config")
	rc, err = clientcmd.BuildConfigFromFlags("", defaultPath)
	if err != nil {
		return nil, "", fmt.Errorf("build kubeconfig from %s: %w", defaultPath, err)
	}
	return rc, defaultPath, nil
}
