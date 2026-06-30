package credential

import (
	"context"
	"encoding/json"
	"go/parser"
	"go/token"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

// entraConfig is a well-formed entra-exchange reference used across these tests.
const entraConfig = `{"credential_provider":"entra-exchange","tenant_id":"tenant-123","client_id":"client-abc","audience":"api://aks-prod","client_secret_env_var":"JOE_AKS_SECRET"}`

// fixedExpiry is a deterministic token expiry (tests never call time.Now()).
var fixedExpiry = time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)

// newTestEntraProvider builds a provider with an injected env lookup and an
// injected exchange that records the config + secret it was handed and mints a
// token derived from the audience — so a test can prove the audience is sourced
// from config and the secret resolved by reference, with NO live Azure call.
func newTestEntraProvider(secretFor map[string]string, captured *entraExchangeConfig, capturedSecret *string) *EntraExchangeProvider {
	return &EntraExchangeProvider{
		lookupEnv: func(name string) (string, bool) {
			v, ok := secretFor[name]
			return v, ok
		},
		exchange: func(_ context.Context, cfg entraExchangeConfig, clientSecret string) (*oauth2.Token, error) {
			if captured != nil {
				*captured = cfg
			}
			if capturedSecret != nil {
				*capturedSecret = clientSecret
			}
			return &oauth2.Token{AccessToken: "minted-for-" + cfg.Audience, Expiry: fixedExpiry}, nil
		},
	}
}

// TestEntraExchange_MintsTokenThroughBearerAccessor proves a minted Entra token
// flows through the credential half and is reachable ONLY via the generalized
// BearerToken accessor (the same seam static-bearer uses), and that the diagnostic
// half surfaces both the audience and the token expiry.
func TestEntraExchange_MintsTokenThroughBearerAccessor(t *testing.T) {
	p := newTestEntraProvider(map[string]string{"JOE_AKS_SECRET": "the-secret"}, nil, nil)
	res, err := p.Resolve(context.Background(), "k8s-aks", json.RawMessage(entraConfig))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !res.Diagnostic.OK || res.Diagnostic.Stage != StageMintSucceeded {
		t.Fatalf("diagnostic = %+v; want ok mint-succeeded", res.Diagnostic)
	}
	if res.Diagnostic.Provider != KindEntraExchange {
		t.Errorf("provider = %q; want entra-exchange", res.Diagnostic.Provider)
	}
	tok, ok := res.BearerToken()
	if !ok || tok != "minted-for-api://aks-prod" {
		t.Fatalf("BearerToken = %q,%t; want the minted token", tok, ok)
	}
	// An entra-exchange resolution is NOT a static resolution.
	if _, ok := res.StaticValue(); ok {
		t.Errorf("StaticValue returned ok=true for an entra-exchange resolution")
	}
	// Diagnostic surfaces audience AND expiry.
	if res.Diagnostic.Audience != "api://aks-prod" {
		t.Errorf("diagnostic audience = %q; want api://aks-prod", res.Diagnostic.Audience)
	}
	if res.Diagnostic.ExpiresAt == nil || !res.Diagnostic.ExpiresAt.Equal(fixedExpiry) {
		t.Errorf("diagnostic expiry = %v; want %v", res.Diagnostic.ExpiresAt, fixedExpiry)
	}
}

// TestEntraExchange_AudienceFromConfigNotHardcoded proves the audience is a
// per-resolution config value, never a constant: two configs with different
// audiences produce two different diagnostic audiences and two different minted
// tokens, and the exchange is handed the config's audience each time.
func TestEntraExchange_AudienceFromConfigNotHardcoded(t *testing.T) {
	for _, aud := range []string{"api://aks-one", "api://aks-two"} {
		var seen entraExchangeConfig
		p := newTestEntraProvider(map[string]string{"S": "secret"}, &seen, nil)
		cfg := `{"tenant_id":"t","client_id":"c","audience":"` + aud + `","client_secret_env_var":"S"}`
		res, err := p.Resolve(context.Background(), "k8s", json.RawMessage(cfg))
		if err != nil {
			t.Fatalf("Resolve(%s): %v", aud, err)
		}
		if seen.Audience != aud {
			t.Errorf("exchange handed audience %q; want %q (audience must come from config)", seen.Audience, aud)
		}
		if res.Diagnostic.Audience != aud {
			t.Errorf("diagnostic audience = %q; want %q", res.Diagnostic.Audience, aud)
		}
		if tok, _ := res.BearerToken(); tok != "minted-for-"+aud {
			t.Errorf("token = %q; want a token derived from the config audience %q", tok, aud)
		}
	}
}

// TestEntraExchange_ClientSecretByReference proves the client secret is resolved by
// reference through the named env var (name-only stored) and handed to the
// exchange — never read from config inline.
func TestEntraExchange_ClientSecretByReference(t *testing.T) {
	var gotSecret string
	p := newTestEntraProvider(map[string]string{"JOE_AKS_SECRET": "resolved-secret-value"}, nil, &gotSecret)
	if _, err := p.Resolve(context.Background(), "k8s", json.RawMessage(entraConfig)); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if gotSecret != "resolved-secret-value" {
		t.Errorf("exchange got secret %q; want the value resolved from the named env var", gotSecret)
	}
}

// TestEntraExchange_SecretEnvUnsetFails proves an unset client-secret variable
// stops at mint-attempted with a non-sensitive reason and no token (no exchange).
func TestEntraExchange_SecretEnvUnsetFails(t *testing.T) {
	p := newTestEntraProvider(map[string]string{}, nil, nil)
	res, err := p.Resolve(context.Background(), "k8s", json.RawMessage(entraConfig))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Diagnostic.OK || res.Diagnostic.Stage != StageMintAttempted {
		t.Fatalf("diagnostic = %+v; want not-ok mint-attempted", res.Diagnostic)
	}
	if tok, ok := res.BearerToken(); ok {
		t.Errorf("BearerToken = %q; want none on failure", tok)
	}
}

// TestEntraExchange_ExchangeFailureFails proves a failed exchange stops at
// mint-attempted with a non-sensitive reason.
func TestEntraExchange_ExchangeFailureFails(t *testing.T) {
	p := &EntraExchangeProvider{
		lookupEnv: func(string) (string, bool) { return "secret", true },
		exchange: func(context.Context, entraExchangeConfig, string) (*oauth2.Token, error) {
			return nil, context.DeadlineExceeded
		},
	}
	res, err := p.Resolve(context.Background(), "k8s", json.RawMessage(entraConfig))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Diagnostic.OK || res.Diagnostic.Stage != StageMintAttempted {
		t.Fatalf("diagnostic = %+v; want not-ok mint-attempted", res.Diagnostic)
	}
}

// TestEntraExchange_MissingRequiredFieldsError proves an incomplete reference is a
// hard configuration error (distinct from an operational mint failure).
func TestEntraExchange_MissingRequiredFieldsError(t *testing.T) {
	p := NewEntraExchangeProvider()
	cases := []string{
		`{"client_id":"c","audience":"a","client_secret_env_var":"S"}`,  // no tenant_id
		`{"tenant_id":"t","audience":"a","client_secret_env_var":"S"}`,  // no client_id
		`{"tenant_id":"t","client_id":"c","client_secret_env_var":"S"}`, // no audience
		`{"tenant_id":"t","client_id":"c","audience":"a"}`,              // no client_secret_env_var
	}
	for _, c := range cases {
		if _, err := p.Resolve(context.Background(), "k8s", json.RawMessage(c)); err == nil {
			t.Errorf("want hard error for incomplete reference %s", c)
		}
	}
}

// TestEntraExchange_ProbeAdvances proves the provider's OWN Probe advances a minted
// token to connectivity-probed without contacting a backend.
func TestEntraExchange_ProbeAdvances(t *testing.T) {
	p := newTestEntraProvider(map[string]string{"JOE_AKS_SECRET": "s"}, nil, nil)
	res, err := p.Resolve(context.Background(), "k8s", json.RawMessage(entraConfig))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	probed, err := p.Probe(context.Background(), res)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if probed.Diagnostic.Stage != StageConnectivityProbed || !probed.Diagnostic.OK {
		t.Errorf("probe diagnostic = %+v; want ok connectivity-probed", probed.Diagnostic)
	}
}

// TestEntraExchange_AvailableReferencesNotApplicable proves the provider answers
// honestly not-applicable: its client_secret_env_var name is free-form, not an
// enumerable candidate set.
func TestEntraExchange_AvailableReferencesNotApplicable(t *testing.T) {
	refs, err := NewEntraExchangeProvider().AvailableReferences("kubernetes")
	if err != nil {
		t.Fatalf("AvailableReferences: %v", err)
	}
	if refs.Applicable || len(refs.Candidates) != 0 {
		t.Errorf("entra-exchange references should be not-applicable with no candidates; got %+v", refs)
	}
}

// TestEntraProvider_TransportAgnostic is the STRUCTURAL break-test pinning the
// load-bearing reusability invariant: the Entra provider source imports NO
// kubernetes symbol and NO Azure SDK — only the generic vendored OAuth2 client. It
// is AST-scoped to the provider's own file (the transport_break_test.go precedent),
// never a tree-wide grep. If this fails, the provider has become transport-bound
// and the deferred Azure credential track can no longer inherit it.
func TestEntraProvider_TransportAgnostic(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "entra_exchange.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse entra_exchange.go: %v", err)
	}
	forbidden := []string{
		"k8s.io",                // any client-go / apimachinery symbol
		"internal/adapters/k8s", // the kubernetes adapter
		"azure",                 // any Azure SDK (azcore/azidentity/...)
		"microsoft",             // any Microsoft SDK
	}
	for _, imp := range f.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		lower := strings.ToLower(path)
		for _, bad := range forbidden {
			if strings.Contains(lower, bad) {
				t.Errorf("entra_exchange.go imports %q (matches forbidden %q) — the provider must be transport-agnostic with no kubernetes or Azure-SDK binding", path, bad)
			}
		}
	}
}
