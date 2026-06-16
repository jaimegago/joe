package prometheus_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/adapters/observability/prometheus"
	"github.com/jaimegago/joe/internal/store"
)

// promAuthServer answers the buildinfo health check 200 and records the
// Authorization header of the most recent request, so a test can assert which
// token the credential seam routed onto the wire.
func promAuthServer(t *testing.T, gotAuth *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"success","data":{}}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// A003-W2 (a): Connect resolves its api_key through the static provider's inline
// value, which overrides the legacy field — proving the seam is exercised, not
// bypassed.
func TestConnect_StaticProvider_ResolvesInlineValue(t *testing.T) {
	var gotAuth string
	srv := promAuthServer(t, &gotAuth)

	a := prometheus.New()
	cfg := fmt.Sprintf(`{"url":%q,"api_key":"placeholder","credential_provider":"static","value":"resolved-tok"}`, srv.URL)
	if err := a.Connect(context.Background(), store.Component{ID: "prom-1", Config: []byte(cfg)}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if !strings.Contains(gotAuth, "resolved-tok") {
		t.Errorf("Authorization = %q, want it to carry resolved-tok (provider value should win)", gotAuth)
	}
}

// A003-W2 (b): a config with no discriminator selects the static provider, which
// yields no override, so the legacy inline api_key still reaches the wire.
func TestConnect_NoDiscriminator_PreservesLegacyToken(t *testing.T) {
	var gotAuth string
	srv := promAuthServer(t, &gotAuth)

	a := prometheus.New()
	cfg := fmt.Sprintf(`{"url":%q,"api_key":"legacy-tok"}`, srv.URL)
	if err := a.Connect(context.Background(), store.Component{ID: "prom-2", Config: []byte(cfg)}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if !strings.Contains(gotAuth, "legacy-tok") {
		t.Errorf("Authorization = %q, want it to carry legacy-tok", gotAuth)
	}
}

// A003-W2 (c): a Resolve failure (named env var unset) surfaces through Connect's
// error path, and the credential-bearing config value never enters the error.
func TestConnect_ResolveFailure_SurfacesWithoutCredential(t *testing.T) {
	a := prometheus.New()
	cfg := `{"url":"http://127.0.0.1:1","api_key":"SUPERSECRET","credential_provider":"static","env_var":"JOE_DEFINITELY_UNSET_VAR_XYZ"}`
	err := a.Connect(context.Background(), store.Component{ID: "prom-3", Config: []byte(cfg)})
	if err == nil {
		t.Fatal("expected Connect to fail when the named env var is unset")
	}
	if strings.Contains(err.Error(), "SUPERSECRET") {
		t.Errorf("credential leaked into error: %v", err)
	}
}
