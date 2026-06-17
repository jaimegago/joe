package credential

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// kubeconfigExecConfig is the kubeconfig-exec provider's view of a component
// config: a selection of WHICH kubeconfig/context the adapter will build a
// *rest.Config from. The exec credential plugin lives in that kubeconfig, not in
// Joe — client-go owns the refresh.
type kubeconfigExecConfig struct {
	CredentialProvider Kind   `json:"credential_provider,omitempty"`
	Kubeconfig         string `json:"kubeconfig,omitempty"`
	Context            string `json:"context,omitempty"`
	InCluster          bool   `json:"in_cluster,omitempty"`
	Audience           string `json:"audience,omitempty"`
}

func parseKubeconfigExecConfig(config json.RawMessage) (kubeconfigExecConfig, error) {
	var cfg kubeconfigExecConfig
	if len(config) == 0 {
		return cfg, nil
	}
	if err := json.Unmarshal(config, &cfg); err != nil {
		return kubeconfigExecConfig{}, fmt.Errorf("credential: parse kubeconfig-exec config: %w", err)
	}
	return cfg, nil
}

// kubeProbeResult is the outcome of a connectivity probe. On failure, stderr
// carries the raw exec-plugin output verbatim — untrusted, possibly
// secret-bearing text surfaced only through Resolution.CapturedStderr.
type kubeProbeResult struct {
	expiresAt *time.Time
	stderr    string
	err       error
}

// kubeProber attempts connectivity for a selection. Injectable so tests need no
// real cluster or exec plugin.
type kubeProber func(ctx context.Context, sel KubeSelection) kubeProbeResult

// KubeconfigExecProvider is the first-class, vendor-neutral, refresh-native
// (via client-go) provider. Its source is the selected kubeconfig/context, NOT a
// built *rest.Config and NOT an extracted token: the adapter builds the client,
// client-go owns the refresh, Joe observes the mint.
type KubeconfigExecProvider struct {
	prober kubeProber
}

// NewKubeconfigExecProvider constructs a provider that probes via client-go.
func NewKubeconfigExecProvider() *KubeconfigExecProvider {
	return &KubeconfigExecProvider{prober: defaultKubeProbe}
}

// Resolve selects the kubeconfig/context the adapter will consume and reaches
// StageMintSucceeded. It never builds a *rest.Config and never contacts the
// backend — the real mint is client-go's transport, observed (not driven) later.
func (p *KubeconfigExecProvider) Resolve(_ context.Context, componentID string, config json.RawMessage) (*Resolution, error) {
	cfg, err := parseKubeconfigExecConfig(config)
	if err != nil {
		return nil, err
	}
	sel := KubeSelection{
		Kubeconfig: cfg.Kubeconfig,
		Context:    cfg.Context,
		InCluster:  cfg.InCluster,
	}
	diag := Diagnostic{
		ComponentID: componentID,
		Provider:    KindKubeconfigExec,
		Audience:    cfg.Audience,
		Stage:       StageMintSucceeded,
		OK:          true,
	}
	return &Resolution{
		Diagnostic: diag,
		cred:       Credential{kind: KindKubeconfigExec, kube: sel},
	}, nil
}

// Probe attempts connectivity against the cluster. On exec-plugin failure it
// returns a GENERIC mint-failed result (StageMintAttempted, non-sensitive reason)
// and captures the plugin's raw stderr verbatim into the credential half, where
// it is reachable ONLY through Resolution.CapturedStderr — never the diagnostic
// half, never structured-log rendering.
func (p *KubeconfigExecProvider) Probe(ctx context.Context, res *Resolution) (*Resolution, error) {
	sel, ok := res.KubeSelection()
	if !ok {
		return nil, fmt.Errorf("credential: probe requires a kubeconfig-exec resolution")
	}
	out := *res
	result := p.prober(ctx, sel)
	if result.err != nil {
		out.Diagnostic.Stage = StageMintAttempted
		out.Diagnostic.OK = false
		out.Diagnostic.Reason = "credential mint failed (exec plugin error)"
		out.Diagnostic.ExpiresAt = nil
		out.cred.stderr = result.stderr
		return &out, nil
	}
	out.Diagnostic.Stage = StageConnectivityProbed
	out.Diagnostic.OK = true
	out.Diagnostic.Reason = ""
	out.Diagnostic.ExpiresAt = result.expiresAt
	return &out, nil
}

// AvailableReferences is honestly not-applicable for the kubeconfig-exec
// provider: its reference is a kubeconfig file path plus a context name, not an
// enumerable set the way env-var names are. Rather than forcing env semantics
// onto it, it reports Applicable=false with no candidates — the picker renders
// the kubeconfig/context locator form (from promotion-requirements) instead of a
// candidate list. componentType is unused; the answer is the same for every type
// wired to this provider.
func (p *KubeconfigExecProvider) AvailableReferences(_ string) (References, error) {
	return References{Applicable: false, Candidates: []Candidate{}}, nil
}

// Describe reports the kubeconfig-exec provider's non-sensitive descriptor.
func (p *KubeconfigExecProvider) Describe(config json.RawMessage) (Descriptor, error) {
	cfg, err := parseKubeconfigExecConfig(config)
	if err != nil {
		return Descriptor{}, err
	}
	return Descriptor{
		Provider: KindKubeconfigExec,
		Audience: cfg.Audience,
		Context:  cfg.Context,
	}, nil
}

// defaultKubeProbe builds a throwaway client from the selection and performs a
// server-version connectivity check. It observes the outcome — it does not drive
// the adapter's client. The error text (which embeds exec-plugin stderr) is
// captured for the human-facing accessor.
func defaultKubeProbe(_ context.Context, sel KubeSelection) kubeProbeResult {
	var restCfg *rest.Config
	var err error
	if sel.InCluster {
		restCfg, err = rest.InClusterConfig()
	} else {
		rules := clientcmd.NewDefaultClientConfigLoadingRules()
		if sel.Kubeconfig != "" {
			expanded, expErr := expandKubeconfigPath(sel.Kubeconfig)
			if expErr != nil {
				return kubeProbeResult{stderr: expErr.Error(), err: expErr}
			}
			rules.ExplicitPath = expanded
		}
		overrides := &clientcmd.ConfigOverrides{}
		if sel.Context != "" {
			overrides.CurrentContext = sel.Context
		}
		restCfg, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).ClientConfig()
	}
	if err != nil {
		return kubeProbeResult{stderr: err.Error(), err: err}
	}
	cs, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return kubeProbeResult{stderr: err.Error(), err: err}
	}
	if _, err := cs.Discovery().ServerVersion(); err != nil {
		return kubeProbeResult{stderr: err.Error(), err: err}
	}
	return kubeProbeResult{}
}

// expandKubeconfigPath expands a leading ~ in a kubeconfig path to the user's
// home directory so Probe loads exactly the file the k8s adapter would. clientcmd
// does not expand ~ itself, so without this a tilde-prefixed path the adapter
// handles via its expandPath would be misreported as unreachable.
//
// This mirrors internal/adapters/k8s.expandPath byte-for-byte (os.UserHomeDir,
// no filepath.Abs) deliberately: the logic is duplicated rather than shared
// because internal/adapters/k8s imports internal/credential (D-0026 unit 2), so
// credential cannot import it back without an import cycle. The path is a file
// pointer, not credential material, and is never placed on the diagnostic half.
func expandKubeconfigPath(path string) (string, error) {
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

// ExpandKubeconfigPathForTest exposes the unexported expandKubeconfigPath to the
// cross-package tilde-helper guard (internal/credential/tildeguard, D-0026). It
// exists only so that guard can assert this hand-copied duplicate stays
// byte-identical to the canonical k8s adapter helper; it has no production use.
func ExpandKubeconfigPathForTest(path string) (string, error) { return expandKubeconfigPath(path) }
