package k8s

import (
	"context"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/credential"
	"github.com/jaimegago/joe/internal/store"
)

// agent-identity-doc-02: the kubernetes transport resolves a bearer token for the
// component's auth_method and builds a *rest.Config by hand. These tests exercise
// the resolution wiring and rest.Config construction, not connectivity — no
// cluster is contacted.

// TestResolveBearerToken_EnvVarSource proves the static-bearer env-var source
// resolves the bearer token through the per-component auth_method seam.
func TestResolveBearerToken_EnvVarSource(t *testing.T) {
	t.Setenv("JOE_K8S_BEARER_TEST", "tok-abc123")
	raw := []byte(`{"auth_method":"static-bearer","api_server":"https://k8s.example.com:6443","env_var":"JOE_K8S_BEARER_TEST"}`)

	token, err := resolveBearerToken(context.Background(), "k8s-1", raw, AuthMethodStaticBearer)
	if err != nil {
		t.Fatalf("resolveBearerToken: %v", err)
	}
	if token != "tok-abc123" {
		t.Errorf("token = %q, want tok-abc123", token)
	}
}

// TestBuildRESTConfig_HandBuilt proves the builder sets host, CA data, and bearer
// token from the coordinates + resolved token — and sets NOTHING else (no exec
// provider, no auth provider, no impersonation, no kubeconfig-derived fields).
func TestBuildRESTConfig_HandBuilt(t *testing.T) {
	cfg := Config{
		APIServer:  "https://k8s.example.com:6443",
		CAData:     "-----BEGIN CERTIFICATE-----\nMIIC\n-----END CERTIFICATE-----",
		Namespace:  "platform",
		AuthMethod: AuthMethodStaticBearer,
	}
	rc, err := buildRESTConfig(cfg, "tok-xyz")
	if err != nil {
		t.Fatalf("buildRESTConfig: %v", err)
	}
	if rc.Host != cfg.APIServer {
		t.Errorf("Host = %q, want %q", rc.Host, cfg.APIServer)
	}
	if string(rc.TLSClientConfig.CAData) != cfg.CAData {
		t.Errorf("CAData = %q, want %q", rc.TLSClientConfig.CAData, cfg.CAData)
	}
	if rc.BearerToken != "tok-xyz" {
		t.Errorf("BearerToken = %q, want tok-xyz", rc.BearerToken)
	}
	// The three fields are the ONLY transport material; everything else stays zero.
	if rc.BearerTokenFile != "" {
		t.Errorf("BearerTokenFile = %q; want empty (token is inline, no file)", rc.BearerTokenFile)
	}
	if rc.TLSClientConfig.CAFile != "" {
		t.Errorf("CAFile = %q; want empty (CA is inline CAData, never an on-disk path)", rc.TLSClientConfig.CAFile)
	}
	if rc.ExecProvider != nil {
		t.Error("ExecProvider is set; the hand-built config must never carry an exec provider")
	}
	if rc.AuthProvider != nil {
		t.Error("AuthProvider is set; the hand-built config must never carry an auth provider")
	}
	if rc.Impersonate.UserName != "" || rc.Impersonate.UID != "" || len(rc.Impersonate.Groups) != 0 {
		t.Errorf("Impersonate is set (%+v); Joe never impersonates", rc.Impersonate)
	}
	if rc.Username != "" || rc.Password != "" || len(rc.TLSClientConfig.CertData) != 0 || len(rc.TLSClientConfig.KeyData) != 0 {
		t.Error("a non-bearer credential (basic-auth or client-cert) is set; only the bearer token is permitted")
	}
}

// TestKindForAuthMethod_SelectsPerMethod proves the per-component auth_method seam
// maps each method to its credential Kind: static-bearer and entra-exchange map to
// their respective Kinds, and an unknown method is a hard error. This is the seam
// the promotion boundary mirrors via the exported KindForAuthMethod.
func TestKindForAuthMethod_SelectsPerMethod(t *testing.T) {
	if k, err := kindForAuthMethod(AuthMethodStaticBearer); err != nil || k != credential.KindStaticBearer {
		t.Errorf("static-bearer -> %q,%v; want KindStaticBearer", k, err)
	}
	if k, err := kindForAuthMethod(AuthMethodEntraExchange); err != nil || k != credential.KindEntraExchange {
		t.Errorf("entra-exchange -> %q,%v; want KindEntraExchange", k, err)
	}
	if _, err := kindForAuthMethod("client-cert"); err == nil {
		t.Error("want error for an unsupported auth_method")
	}
	// The exported face used by the promotion boundary agrees with the internal seam.
	if k, err := KindForAuthMethod(AuthMethodEntraExchange); err != nil || k != credential.KindEntraExchange {
		t.Errorf("KindForAuthMethod(entra-exchange) -> %q,%v; want KindEntraExchange", k, err)
	}
}

// TestResolveBearerToken_EntraExchangeRoutesToProvider proves auth_method
// entra-exchange routes through resolveBearerToken to the Entra provider: with the
// client-secret variable unset the Entra provider's own non-sensitive mint-attempted
// reason surfaces as the Connect error — reaching the provider WITHOUT any live
// Azure exchange. Once the token IS minted it rides the identical BearerToken seam
// and is applied to rc.BearerToken by buildRESTConfig (TestBuildRESTConfig_HandBuilt),
// exactly as a static-bearer token is.
func TestResolveBearerToken_EntraExchangeRoutesToProvider(t *testing.T) {
	raw := []byte(`{"auth_method":"entra-exchange","api_server":"https://aks.example.com:443","tenant_id":"t","client_id":"c","audience":"api://aks","client_secret_env_var":"JOE_AKS_SECRET_UNSET"}`)
	_, err := resolveBearerToken(context.Background(), "k8s-aks", raw, AuthMethodEntraExchange)
	if err == nil {
		t.Fatal("expected resolveBearerToken to fail when the client-secret env var is unset")
	}
	if !strings.Contains(err.Error(), "client-secret") {
		t.Errorf("error = %q; want it to surface the Entra provider's non-sensitive reason", err)
	}
}

// TestBuildRESTConfig_RequiresAPIServer proves a missing api-server URL is a hard
// configuration error — the host is ours to set, never kubeconfig-derived.
func TestBuildRESTConfig_RequiresAPIServer(t *testing.T) {
	if _, err := buildRESTConfig(Config{AuthMethod: AuthMethodStaticBearer}, "tok"); err == nil {
		t.Fatal("buildRESTConfig should fail when api_server is empty")
	}
}

// TestConnect_UnsupportedAuthMethod proves an unknown/empty auth_method surfaces
// through Connect's normal error path before any cluster contact, with no
// credential material in the error.
func TestConnect_UnsupportedAuthMethod(t *testing.T) {
	a := New()
	raw := []byte(`{"api_server":"https://k8s.example.com:6443","auth_method":"client-cert"}`)
	err := a.Connect(context.Background(), store.Component{ID: "k8s-1", Config: raw})
	if err == nil {
		t.Fatal("expected Connect to fail for an unsupported auth_method")
	}
	if a.Status().Connected {
		t.Error("adapter should not be connected after an auth_method failure")
	}
}

// TestConnect_EnvVarUnsetSurfaces proves a static-bearer env-var source whose
// variable is unset fails Connect with a non-sensitive reason before any cluster
// contact.
func TestConnect_EnvVarUnsetSurfaces(t *testing.T) {
	a := New()
	raw := []byte(`{"api_server":"https://k8s.example.com:6443","auth_method":"static-bearer","env_var":"JOE_K8S_DEFINITELY_UNSET"}`)
	err := a.Connect(context.Background(), store.Component{ID: "k8s-1", Config: raw})
	if err == nil {
		t.Fatal("expected Connect to fail when the named env var is unset")
	}
	if a.Status().Connected {
		t.Error("adapter should not be connected after a resolve failure")
	}
}
