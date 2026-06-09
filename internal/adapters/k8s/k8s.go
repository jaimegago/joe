package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/credential"
	"github.com/jaimegago/joe/internal/store"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// KubernetesAdapter extends the base Adapter with K8s-specific operations.
type KubernetesAdapter interface {
	adapters.Adapter
	ListResources(ctx context.Context, resource, namespace string) ([]unstructured.Unstructured, error)
	GetResource(ctx context.Context, resource, namespace, name string) (*unstructured.Unstructured, error)
	GetPodLogs(ctx context.Context, namespace, pod, container string, tailLines int) (string, error)
}

// Adapter is the concrete K8s adapter using client-go.
type Adapter struct {
	mu         sync.RWMutex
	config     Config
	restConfig *rest.Config
	dynClient  dynamic.Interface
	clientset  kubernetes.Interface
	connected  bool
}

// New creates a new K8s adapter (not yet connected).
func New() *Adapter {
	return &Adapter{}
}

// NewWithClients creates an adapter with pre-built clients (for testing).
func NewWithClients(dynClient dynamic.Interface, clientset kubernetes.Interface) *Adapter {
	return &Adapter{
		dynClient: dynClient,
		clientset: clientset,
		connected: true,
	}
}

// Connect establishes a connection to the K8s cluster.
func (a *Adapter) Connect(ctx context.Context, source store.Component) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	cfg, err := ParseConfig(source.Config)
	if err != nil {
		return fmt.Errorf("parse source config: %w", err)
	}

	// D-0026 unit 2: route the kubeconfig/context selection through the provider
	// selected by the component config before building the *rest.Config. A config
	// without a discriminator selects the static provider, which yields no
	// KubeSelection, so cfg is left as parsed and existing behavior is preserved.
	// The eager ServerVersion liveness probe below is kept unchanged.
	cfg, err = applyResolvedCredential(ctx, source.ID, source.Config, cfg)
	if err != nil {
		return err
	}
	a.config = cfg

	restConfig, err := buildRESTConfig(cfg)
	if err != nil {
		return fmt.Errorf("build rest config: %w", err)
	}
	a.restConfig = restConfig

	dynClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("create dynamic client: %w", err)
	}
	a.dynClient = dynClient

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("create clientset: %w", err)
	}
	a.clientset = clientset

	// Verify connectivity
	_, err = clientset.Discovery().ServerVersion()
	if err != nil {
		return fmt.Errorf("verify cluster connectivity: %w", err)
	}

	a.connected = true
	return nil
}

// Disconnect closes the connection.
func (a *Adapter) Disconnect() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.connected = false
	a.dynClient = nil
	a.clientset = nil
	a.restConfig = nil
	return nil
}

// Status returns the current connection status.
func (a *Adapter) Status() adapters.Status {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.connected {
		return adapters.Status{Connected: true, Message: "connected"}
	}
	return adapters.Status{Connected: false, Message: "disconnected"}
}

// applyResolvedCredential selects the credential provider from the component
// config and, for a kubeconfig-exec resolution, routes the selected
// kubeconfig/context selection into cfg. A config without a discriminator
// resolves via the static provider, which yields no KubeSelection, so cfg is
// returned unchanged. A resolution failure is surfaced as a plain Connect error
// carrying only the non-sensitive reason — never the credential.
func applyResolvedCredential(ctx context.Context, componentID string, raw json.RawMessage, cfg Config) (Config, error) {
	provider, err := credential.Select(raw)
	if err != nil {
		return Config{}, fmt.Errorf("select credential provider: %w", err)
	}
	res, err := provider.Resolve(ctx, componentID, raw)
	if err != nil {
		return Config{}, fmt.Errorf("resolve credential: %w", err)
	}
	if !res.Diagnostic.OK {
		return Config{}, fmt.Errorf("resolve credential: %s", res.Diagnostic.Reason)
	}
	if sel, ok := res.KubeSelection(); ok {
		cfg.Kubeconfig = sel.Kubeconfig
		cfg.Context = sel.Context
		cfg.InCluster = sel.InCluster
	}
	return cfg, nil
}

func buildRESTConfig(cfg Config) (*rest.Config, error) {
	if cfg.InCluster {
		return rest.InClusterConfig()
	}

	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if cfg.Kubeconfig != "" {
		expanded, err := expandPath(cfg.Kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("expand kubeconfig path: %w", err)
		}
		rules.ExplicitPath = expanded
	}

	overrides := &clientcmd.ConfigOverrides{}
	if cfg.Context != "" {
		overrides.CurrentContext = cfg.Context
	}

	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).ClientConfig()
}

func expandPath(path string) (string, error) {
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if len(path) == 1 {
			return home, nil
		}
		return filepath.Join(home, path[1:]), nil
	}
	return path, nil
}
