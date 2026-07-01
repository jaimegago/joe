package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/credential"
	"github.com/jaimegago/joe/internal/store"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
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
	a.config = cfg

	// agent-identity-doc-02: resolve the bearer token for this component's
	// auth_method, then build the *rest.Config BY HAND — host from the api-server
	// coordinate, CA from the stored inline bundle, bearer token from the resolved
	// credential. No kubeconfig is ever ingested. The eager ServerVersion liveness
	// probe below is kept unchanged.
	token, err := resolveBearerToken(ctx, source.ID, source.Config, cfg.AuthMethod)
	if err != nil {
		return err
	}

	restConfig, err := buildRESTConfig(cfg, token)
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

// kindForAuthMethod maps a kubernetes component's per-component auth_method to the
// credential Kind that resolves its bearer token. This is the per-component
// Kind-selection seam (decision #1 / D-0060): the method, not a fixed type→Kind
// wiring, selects the provider, so each transport method adds a case here without
// touching the rest of the transport. Both methods resolve to a bearer token the
// adapter applies identically; only the credential source differs (a long-lived
// token vs. a minted Entra token).
func kindForAuthMethod(method string) (credential.Kind, error) {
	switch method {
	case AuthMethodStaticBearer:
		return credential.KindStaticBearer, nil
	case AuthMethodEntraExchange:
		return credential.KindEntraExchange, nil
	default:
		return "", fmt.Errorf("kubernetes: unsupported auth_method %q (expected %q or %q)", method, AuthMethodStaticBearer, AuthMethodEntraExchange)
	}
}

// KindForAuthMethod exposes the per-component auth_method→credential.Kind seam to
// the promotion boundary (internal/api) so it selects the SAME Kind the adapter
// will at Connect — the discriminator written at promotion and the provider used
// at Connect cannot diverge. It is the exported face of the internal
// kindForAuthMethod mapping.
func KindForAuthMethod(method string) (credential.Kind, error) {
	return kindForAuthMethod(method)
}

// resolveBearerToken maps the component's auth_method to a credential Kind and
// resolves the bearer token through that Kind's provider. The resolved token is
// the ONLY credential material that reaches the adapter; the host and CA come from
// the component's own coordinates. A resolution failure surfaces as a plain
// Connect error carrying only the non-sensitive reason — never the credential.
func resolveBearerToken(ctx context.Context, componentID string, raw json.RawMessage, authMethod string) (string, error) {
	kind, err := kindForAuthMethod(authMethod)
	if err != nil {
		return "", err
	}
	provider, err := credential.ProviderForKind(kind)
	if err != nil {
		return "", fmt.Errorf("select credential provider: %w", err)
	}
	res, err := provider.Resolve(ctx, componentID, raw)
	if err != nil {
		return "", fmt.Errorf("resolve credential: %w", err)
	}
	if !res.Diagnostic.OK {
		return "", fmt.Errorf("resolve credential: %s", res.Diagnostic.Reason)
	}
	token, ok := res.BearerToken()
	if !ok {
		return "", fmt.Errorf("resolve credential: provider did not yield a bearer token")
	}
	return token, nil
}

// buildRESTConfig constructs a *rest.Config BY HAND (agent-identity-doc-02): host
// from the api-server URL coordinate, TLSClientConfig.CAData from the stored
// inline CA bundle, BearerToken from the resolved static-bearer credential. These
// three fields are the ONLY ones this builder sets. The adapter NEVER ingests a
// kubeconfig and NEVER sets an exec provider, auth provider, or impersonation —
// Joe authenticates only as its own non-human identity.
func buildRESTConfig(cfg Config, bearerToken string) (*rest.Config, error) {
	if cfg.APIServer == "" {
		return nil, fmt.Errorf("kubernetes: api_server URL is required")
	}
	rc := &rest.Config{
		Host:        cfg.APIServer,
		BearerToken: bearerToken,
	}
	if cfg.CAData != "" {
		rc.TLSClientConfig.CAData = []byte(cfg.CAData)
	}
	return rc, nil
}
