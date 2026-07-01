package credential

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// serviceAccountTokenPath is the projected service-account token mounted into
// every pod. The static-bearer in_cluster source reads THIS file directly —
// deliberately NOT via rest.InClusterConfig(), which would also derive the host
// and CA and own all three REST fields, defeating the hand-built-config stance
// (agent-identity-doc-02). Only the token is read here; the kubernetes adapter
// supplies host and CA from the component's own coordinates.
const serviceAccountTokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token" //nolint:gosec // a well-known mount PATH, not a credential

// staticBearerConfig is the static-bearer provider's view of a component config:
// which locator source the bearer token is resolved from. Exactly one of the two
// sources is supplied. The env_var source stores only a NAME and looks the value
// up at call time (never persisting the secret); the in_cluster source reads the
// pod-mounted token directly. No inline value field exists by construction —
// static-bearer is indirection-only.
type staticBearerConfig struct {
	CredentialProvider Kind   `json:"credential_provider,omitempty"`
	EnvVar             string `json:"env_var,omitempty"`
	InCluster          bool   `json:"in_cluster,omitempty"`
	Audience           string `json:"audience,omitempty"`
}

func parseStaticBearerConfig(config json.RawMessage) (staticBearerConfig, error) {
	var cfg staticBearerConfig
	if len(config) == 0 {
		return cfg, nil
	}
	if err := json.Unmarshal(config, &cfg); err != nil {
		return staticBearerConfig{}, fmt.Errorf("credential: parse static-bearer config: %w", err)
	}
	return cfg, nil
}

// StaticBearerProvider resolves a long-lived bearer token from one of two
// sources. It is its own Kind (not the generic static provider) so the
// pod-mounted in_cluster source stays contained to the kubernetes transport and
// can never be selected for a single-token HTTP backend.
type StaticBearerProvider struct {
	// lookupEnv is injectable for tests; defaults to os.LookupEnv. It is the
	// call-time env read the env_var source reuses — only a name is stored.
	lookupEnv func(string) (string, bool)
	// readFile is injectable for tests; defaults to os.ReadFile. It is the direct
	// read of the pod-mounted service-account token the in_cluster source uses.
	readFile func(string) ([]byte, error)
}

// NewStaticBearerProvider constructs a StaticBearerProvider backed by the process
// env and filesystem.
func NewStaticBearerProvider() *StaticBearerProvider {
	return &StaticBearerProvider{lookupEnv: os.LookupEnv, readFile: os.ReadFile}
}

// Resolve produces the bearer token the adapter applies and reaches
// StageMintSucceeded. It never contacts a backend. The env_var source fails
// (StageMintAttempted) when the named variable is unset; the in_cluster source
// fails when the mounted token is unreadable.
func (p *StaticBearerProvider) Resolve(_ context.Context, componentID string, config json.RawMessage) (*Resolution, error) {
	cfg, err := parseStaticBearerConfig(config)
	if err != nil {
		return nil, err
	}
	diag := Diagnostic{
		ComponentID: componentID,
		Provider:    KindStaticBearer,
		Audience:    cfg.Audience,
		Stage:       StageProviderSelected,
	}

	var token string
	switch {
	case cfg.InCluster:
		// Direct read of ONLY the token; host and CA are the adapter's coordinates.
		b, rerr := p.readFile(serviceAccountTokenPath)
		if rerr != nil {
			diag.Stage = StageMintAttempted
			diag.OK = false
			diag.Reason = "pod-mounted service-account token is not readable"
			return &Resolution{Diagnostic: diag}, nil
		}
		token = strings.TrimSpace(string(b))
	case cfg.EnvVar != "":
		v, ok := p.lookupEnv(cfg.EnvVar)
		if !ok {
			diag.Stage = StageMintAttempted
			diag.OK = false
			diag.Reason = "named environment variable is not set"
			return &Resolution{Diagnostic: diag}, nil
		}
		token = v
	default:
		return nil, fmt.Errorf("credential: static-bearer requires either in_cluster=true or an env_var locator")
	}

	diag.Stage = StageMintSucceeded
	diag.OK = true
	return &Resolution{
		Diagnostic: diag,
		cred:       Credential{kind: KindStaticBearer, static: token},
	}, nil
}

// Probe is a no-op success for a resolved bearer token: there is no separate
// backend mint to prove, so it advances a successful resolution to
// StageConnectivityProbed.
func (p *StaticBearerProvider) Probe(_ context.Context, res *Resolution) (*Resolution, error) {
	if _, ok := res.BearerToken(); !ok {
		return nil, fmt.Errorf("credential: probe requires a static-bearer resolution")
	}
	out := *res
	out.Diagnostic.Stage = StageConnectivityProbed
	out.Diagnostic.OK = true
	out.Diagnostic.Reason = ""
	return &out, nil
}

// Describe reports the static-bearer provider's non-sensitive descriptor.
func (p *StaticBearerProvider) Describe(config json.RawMessage) (Descriptor, error) {
	cfg, err := parseStaticBearerConfig(config)
	if err != nil {
		return Descriptor{}, err
	}
	return Descriptor{Provider: KindStaticBearer, Audience: cfg.Audience}, nil
}

// AvailableReferences is not-applicable for the static-bearer provider: its
// env_var source is a free-form operator-chosen name (kubernetes has no
// JOE_<SEGMENT>_ enumeration prefix) and its in_cluster source is a fixed mount,
// so neither is an enumerable candidate set. It reports Applicable=false; the
// picker renders the locator form (an env-var name field or an in-cluster choice)
// instead of a candidate list.
func (p *StaticBearerProvider) AvailableReferences(_ string) (References, error) {
	return References{Applicable: false, Candidates: []Candidate{}}, nil
}
