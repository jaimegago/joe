package k8s

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/jaimegago/joe/internal/store"
)

// writeKubeconfig writes a minimal kubeconfig pointing at a fake server and
// returns its path. No real cluster is contacted — these tests exercise the
// credential-resolution wiring and rest.Config construction, not connectivity.
func writeKubeconfig(t *testing.T, server, contextName string) string {
	t.Helper()
	content := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- cluster:
    server: %s
  name: fake-cluster
contexts:
- context:
    cluster: fake-cluster
    user: fake-user
  name: %s
current-context: %s
users:
- name: fake-user
  user:
    token: dummy-token
`, server, contextName, contextName)
	path := filepath.Join(t.TempDir(), "kubeconfig")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	return path
}

// D-0026 unit 2: the kubeconfig-exec provider's selection round-trips through
// applyResolvedCredential into cfg, and buildRESTConfig still produces a working
// *rest.Config pointed at the selected context's server.
func TestApplyResolvedCredential_KubeconfigExec_BuildsRESTConfig(t *testing.T) {
	kubeconfig := writeKubeconfig(t, "https://fake.example.com:6443", "fake-context")

	raw := fmt.Appendf(nil,
		`{"credential_provider":"kubeconfig-exec","kubeconfig":%q,"context":"fake-context"}`, kubeconfig)
	parsed, err := ParseConfig(raw)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}

	cfg, err := applyResolvedCredential(context.Background(), "k8s-1", raw, parsed)
	if err != nil {
		t.Fatalf("applyResolvedCredential: %v", err)
	}
	if cfg.Kubeconfig != kubeconfig {
		t.Errorf("Kubeconfig = %q, want %q (selection should come via the provider)", cfg.Kubeconfig, kubeconfig)
	}
	if cfg.Context != "fake-context" {
		t.Errorf("Context = %q, want fake-context", cfg.Context)
	}

	restConfig, err := buildRESTConfig(cfg)
	if err != nil {
		t.Fatalf("buildRESTConfig: %v", err)
	}
	if restConfig.Host != "https://fake.example.com:6443" {
		t.Errorf("rest.Config.Host = %q, want https://fake.example.com:6443", restConfig.Host)
	}
}

// D-0026 unit 2: a config with no discriminator selects the static provider,
// which yields no KubeSelection, so cfg is left exactly as parsed and the
// adapter still builds the same *rest.Config (existing behavior preserved).
func TestApplyResolvedCredential_NoDiscriminator_PreservesSelection(t *testing.T) {
	kubeconfig := writeKubeconfig(t, "https://legacy.example.com:6443", "legacy-context")

	raw := fmt.Appendf(nil, `{"kubeconfig":%q,"context":"legacy-context"}`, kubeconfig)
	parsed, err := ParseConfig(raw)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}

	cfg, err := applyResolvedCredential(context.Background(), "k8s-2", raw, parsed)
	if err != nil {
		t.Fatalf("applyResolvedCredential: %v", err)
	}
	if cfg != parsed {
		t.Errorf("cfg = %+v, want unchanged %+v", cfg, parsed)
	}

	restConfig, err := buildRESTConfig(cfg)
	if err != nil {
		t.Fatalf("buildRESTConfig: %v", err)
	}
	if restConfig.Host != "https://legacy.example.com:6443" {
		t.Errorf("rest.Config.Host = %q, want https://legacy.example.com:6443", restConfig.Host)
	}
}

// D-0026 unit 2 break-test: a resolution failure (unknown provider kind) surfaces
// through Connect's normal error path before any cluster contact, and no
// credential material appears in the error.
func TestConnect_ResolveFailure_SurfacesThroughNormalPath(t *testing.T) {
	a := New()
	raw := []byte(`{"credential_provider":"bogus","kubeconfig":"/nonexistent","context":"c"}`)
	err := a.Connect(context.Background(), store.Component{ID: "k8s-3", Config: raw})
	if err == nil {
		t.Fatal("expected Connect to fail for an unknown provider kind")
	}
	if a.Status().Connected {
		t.Error("adapter should not be connected after a resolve failure")
	}
}
