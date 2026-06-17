package credential

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// staticConfig is the static/env-var provider's view of a component config. The
// value is read either inline from Value or from the named environment variable.
type staticConfig struct {
	CredentialProvider Kind   `json:"credential_provider,omitempty"`
	Value              string `json:"value,omitempty"`
	EnvVar             string `json:"env_var,omitempty"`
	Audience           string `json:"audience,omitempty"`
}

func parseStaticConfig(config json.RawMessage) (staticConfig, error) {
	var cfg staticConfig
	if len(config) == 0 {
		return cfg, nil
	}
	if err := json.Unmarshal(config, &cfg); err != nil {
		return staticConfig{}, fmt.Errorf("credential: parse static config: %w", err)
	}
	return cfg, nil
}

// StaticProvider wraps a long-lived value (a PAT, datastore URI, or static HTTP
// token). It is the degenerate case: Resolve succeeds trivially to
// StageMintSucceeded and Probe is a no-op success.
type StaticProvider struct {
	// lookupEnv is injectable for tests; defaults to os.LookupEnv.
	lookupEnv func(string) (string, bool)
	// environ is injectable for tests; defaults to os.Environ. It is the ONLY
	// place the static provider reads the whole environment, and it is used solely
	// to enumerate names under a type's prefix in AvailableReferences — never to
	// read a value (the "NAME=VALUE" entries are split on '=' and only the name is
	// kept).
	environ func() []string
}

// NewStaticProvider constructs a StaticProvider backed by the process env.
func NewStaticProvider() *StaticProvider {
	return &StaticProvider{lookupEnv: os.LookupEnv, environ: os.Environ}
}

// Resolve wraps the configured value (inline or from the named env var) and
// reaches StageMintSucceeded. It never contacts a backend. A named-but-unset env
// var is the one operational failure, stopping at StageMintAttempted.
func (p *StaticProvider) Resolve(_ context.Context, componentID string, config json.RawMessage) (*Resolution, error) {
	cfg, err := parseStaticConfig(config)
	if err != nil {
		return nil, err
	}
	diag := Diagnostic{
		ComponentID: componentID,
		Provider:    KindStatic,
		Audience:    cfg.Audience,
		Stage:       StageProviderSelected,
	}

	value := cfg.Value
	if cfg.EnvVar != "" {
		v, ok := p.lookupEnv(cfg.EnvVar)
		if !ok {
			diag.Stage = StageMintAttempted
			diag.OK = false
			diag.Reason = "named environment variable is not set"
			return &Resolution{Diagnostic: diag}, nil
		}
		value = v
	}

	diag.Stage = StageMintSucceeded
	diag.OK = true
	return &Resolution{
		Diagnostic: diag,
		cred:       Credential{kind: KindStatic, static: value},
	}, nil
}

// Probe is a no-op success for a static value: there is no backend mint to prove,
// so it advances a successfully-resolved source to StageConnectivityProbed.
func (p *StaticProvider) Probe(_ context.Context, res *Resolution) (*Resolution, error) {
	if _, ok := res.StaticValue(); !ok {
		return nil, fmt.Errorf("credential: probe requires a static resolution")
	}
	out := *res
	out.Diagnostic.Stage = StageConnectivityProbed
	out.Diagnostic.OK = true
	out.Diagnostic.Reason = ""
	return &out, nil
}

// Describe reports the static provider's non-sensitive descriptor.
func (p *StaticProvider) Describe(config json.RawMessage) (Descriptor, error) {
	cfg, err := parseStaticConfig(config)
	if err != nil {
		return Descriptor{}, err
	}
	return Descriptor{Provider: KindStatic, Audience: cfg.Audience}, nil
}

// AvailableReferences enumerates the process environment for the candidate static
// references an admin may choose for a component of componentType: every env var
// whose name starts with the type's JOE_<SEGMENT>_ prefix, returned as a
// {label, env_var_name} candidate where label is the name with the prefix
// stripped. It reads NAMES ONLY — each "NAME=VALUE" entry is split on the first
// '=' and the value half is discarded, so no credential value is ever read or
// returned — and is scoped strictly to this one type's prefix, so a github
// component can surface only JOE_GITHUB_* and never another prefix or an
// unprefixed variable. A type with no declared prefix segment (i.e. not a
// KindStatic wired type) is a wiring error the caller should not reach via the
// static provider; it is reported as such rather than enumerating nothing.
func (p *StaticProvider) AvailableReferences(componentType string) (References, error) {
	prefix, ok := EnvPrefix(componentType)
	if !ok {
		return References{}, fmt.Errorf("credential: no static env prefix declared for component type %q", componentType)
	}
	candidates := []Candidate{}
	for _, kv := range p.environ() {
		// Split on the first '=' and keep ONLY the name half; the value is
		// discarded unread (a malformed entry without '=' yields the whole token
		// as the name, which is fine — the value is never consulted regardless).
		name, _, _ := strings.Cut(kv, "=")
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		label := name[len(prefix):]
		if label == "" {
			// A bare "JOE_GITHUB_" with no label is not a usable reference.
			continue
		}
		candidates = append(candidates, Candidate{Label: label, EnvVarName: name})
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].EnvVarName < candidates[j].EnvVarName
	})
	return References{Applicable: true, Prefix: prefix, Candidates: candidates}, nil
}
