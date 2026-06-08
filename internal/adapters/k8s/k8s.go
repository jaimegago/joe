package k8s

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/jaimegago/joe/internal/adapters"
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
func (a *Adapter) Connect(_ context.Context, source store.Component) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	cfg, err := ParseConfig(source.Config)
	if err != nil {
		return fmt.Errorf("parse source config: %w", err)
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
