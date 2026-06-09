package credential

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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
}

// NewStaticProvider constructs a StaticProvider backed by the process env.
func NewStaticProvider() *StaticProvider {
	return &StaticProvider{lookupEnv: os.LookupEnv}
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
