package credential

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

// entraExchangeConfig is the Entra-exchange provider's view of a component config.
// All four fields are per-resolution values read from config — NONE is hardcoded
// in the provider, which is what keeps the provider transport-agnostic and
// reusable by a future non-kubernetes Azure component type. The client secret is
// NOT stored here: ClientSecretEnvVar names the environment variable the secret is
// read from at call time (indirection-only, value never persisted), under a
// DISTINCT field name from static-bearer's env_var so it is intentionally exempt
// from the env-var uniqueness guard (one Azure app registration may legitimately
// front many components — agent-identity-doc-03, D-0063).
//
// FederatedTokenFile is the designed-for, NOT-yet-built second credential source:
// a federated workload-identity assertion read from a projected token file. The
// field is reserved here and in the requirements at-least-one-of constraint so the
// additive source slots in later WITHOUT disturbing the client-secret source; the
// provider does not consume it this slice.
type entraExchangeConfig struct {
	CredentialProvider Kind   `json:"credential_provider,omitempty"`
	TenantID           string `json:"tenant_id,omitempty"`
	ClientID           string `json:"client_id,omitempty"`
	Audience           string `json:"audience,omitempty"`
	ClientSecretEnvVar string `json:"client_secret_env_var,omitempty"`
	// FederatedTokenFile is reserved for the deferred federated-assertion source;
	// it is not consumed yet (agent-identity-doc-03).
	FederatedTokenFile string `json:"federated_token_file,omitempty"`
}

func parseEntraExchangeConfig(config json.RawMessage) (entraExchangeConfig, error) {
	var cfg entraExchangeConfig
	if len(config) == 0 {
		return cfg, nil
	}
	if err := json.Unmarshal(config, &cfg); err != nil {
		return entraExchangeConfig{}, fmt.Errorf("credential: parse entra-exchange config: %w", err)
	}
	return cfg, nil
}

// entraTokenURL builds the Microsoft Entra (Azure AD) v2.0 token endpoint for a
// tenant. The tenant is a per-resolution config value; the host is the only
// constant and it is the Microsoft identity platform, not an AKS or kubernetes
// coordinate.
func entraTokenURL(tenantID string) string {
	return "https://login.microsoftonline.com/" + tenantID + "/oauth2/v2.0/token"
}

// tokenExchange performs the OAuth2 client-credentials grant and returns the
// minted token. It is injectable so tests exercise the provider without a live
// Azure call; the default uses golang.org/x/oauth2/clientcredentials.
type tokenExchange func(ctx context.Context, cfg entraExchangeConfig, clientSecret string) (*oauth2.Token, error)

// defaultTokenExchange mints a token via the vendored clientcredentials client.
// The scope is the audience with the "/.default" suffix the v2.0 endpoint
// requires; the audience is a per-resolution config value and is NEVER hardcoded.
func defaultTokenExchange(ctx context.Context, cfg entraExchangeConfig, clientSecret string) (*oauth2.Token, error) {
	cc := clientcredentials.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: clientSecret,
		TokenURL:     entraTokenURL(cfg.TenantID),
		Scopes:       []string{cfg.Audience + "/.default"},
	}
	return cc.Token(ctx)
}

// EntraExchangeProvider mints a short-lived bearer token via an Azure Entra OAuth2
// client-credentials exchange. It is its own Kind so its minted-token lifecycle
// stays distinct from the long-lived static providers, but its resolved credential
// is a bearer token consumed through the identical BearerToken accessor. The
// provider imports no kubernetes symbol and no Azure SDK — only the generic
// vendored OAuth2 client — so it is transport-agnostic and reusable.
type EntraExchangeProvider struct {
	// lookupEnv is injectable for tests; defaults to os.LookupEnv. It is the
	// call-time read of the client secret — only the env-var NAME is stored,
	// exactly as static-bearer's env_var source resolves its token.
	lookupEnv func(string) (string, bool)
	// exchange is injectable for tests; defaults to defaultTokenExchange so no
	// live Azure call is made under test.
	exchange tokenExchange
}

// NewEntraExchangeProvider constructs an EntraExchangeProvider backed by the
// process env and the vendored client-credentials exchange.
func NewEntraExchangeProvider() *EntraExchangeProvider {
	return &EntraExchangeProvider{lookupEnv: os.LookupEnv, exchange: defaultTokenExchange}
}

// Resolve mints the bearer token the adapter applies and reaches
// StageMintSucceeded. The client secret is read from the named environment
// variable at call time; an unset variable fails at StageMintAttempted, and a
// failed exchange fails at StageMintAttempted with a non-sensitive reason. The
// minted token returns through the credential half; the audience and the token's
// expiry are surfaced on the diagnostic half.
func (p *EntraExchangeProvider) Resolve(ctx context.Context, componentID string, config json.RawMessage) (*Resolution, error) {
	cfg, err := parseEntraExchangeConfig(config)
	if err != nil {
		return nil, err
	}
	diag := Diagnostic{
		ComponentID: componentID,
		Provider:    KindEntraExchange,
		Audience:    cfg.Audience,
		Stage:       StageProviderSelected,
	}
	if cfg.TenantID == "" || cfg.ClientID == "" || cfg.Audience == "" {
		return nil, fmt.Errorf("credential: entra-exchange requires tenant_id, client_id and audience")
	}
	if cfg.ClientSecretEnvVar == "" {
		return nil, fmt.Errorf("credential: entra-exchange requires client_secret_env_var (the client secret is resolved by reference, never inline)")
	}

	secret, ok := p.lookupEnv(cfg.ClientSecretEnvVar)
	if !ok {
		diag.Stage = StageMintAttempted
		diag.OK = false
		diag.Reason = "named client-secret environment variable is not set"
		return &Resolution{Diagnostic: diag}, nil
	}

	tok, err := p.exchange(ctx, cfg, secret)
	if err != nil {
		diag.Stage = StageMintAttempted
		diag.OK = false
		diag.Reason = "entra client-credentials exchange failed"
		return &Resolution{Diagnostic: diag}, nil
	}

	diag.Stage = StageMintSucceeded
	diag.OK = true
	if !tok.Expiry.IsZero() {
		exp := tok.Expiry
		diag.ExpiresAt = &exp
	}
	return &Resolution{
		Diagnostic: diag,
		cred:       Credential{kind: KindEntraExchange, static: tok.AccessToken},
	}, nil
}

// Probe is a no-op success for a minted bearer token: there is no separate backend
// mint to prove beyond the exchange Resolve already performed, so it advances a
// successful resolution to StageConnectivityProbed. It is the provider's OWN Probe,
// not routed through any other Kind's accessor.
func (p *EntraExchangeProvider) Probe(_ context.Context, res *Resolution) (*Resolution, error) {
	if _, ok := res.BearerToken(); !ok {
		return nil, fmt.Errorf("credential: probe requires an entra-exchange resolution")
	}
	out := *res
	out.Diagnostic.Stage = StageConnectivityProbed
	out.Diagnostic.OK = true
	out.Diagnostic.Reason = ""
	return &out, nil
}

// Describe reports the entra-exchange provider's non-sensitive descriptor: its
// kind and the configured audience. Expiry is not known without a mint.
func (p *EntraExchangeProvider) Describe(config json.RawMessage) (Descriptor, error) {
	cfg, err := parseEntraExchangeConfig(config)
	if err != nil {
		return Descriptor{}, err
	}
	return Descriptor{Provider: KindEntraExchange, Audience: cfg.Audience}, nil
}

// AvailableReferences is not-applicable for the entra-exchange provider: its
// client_secret_env_var is a free-form operator-chosen name (no JOE_<SEGMENT>_
// enumeration prefix), so it is not an enumerable candidate set. Like the
// static-bearer provider it reports Applicable=false; the picker renders the
// locator form instead of a candidate list.
func (p *EntraExchangeProvider) AvailableReferences(_ string) (References, error) {
	return References{Applicable: false, Candidates: []Candidate{}}, nil
}

// compile-time assertion that the exchange default matches the injectable shape.
var _ tokenExchange = defaultTokenExchange
