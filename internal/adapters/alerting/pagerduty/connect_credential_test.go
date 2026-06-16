package pagerduty_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/adapters/alerting/pagerduty"
	"github.com/jaimegago/joe/internal/store"
)

// pdAuthServer answers the abilities health check 200 and records the
// Authorization header (Token token=<key>) so a test can assert which key the
// seam routed.
func pdAuthServer(t *testing.T, gotAuth *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"abilities":["teams"]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestConnect_StaticProvider_ResolvesInlineValue(t *testing.T) {
	var gotAuth string
	srv := pdAuthServer(t, &gotAuth)

	a := pagerduty.New()
	cfg := fmt.Sprintf(`{"api_url":%q,"api_key":"placeholder","credential_provider":"static","value":"resolved-key"}`, srv.URL)
	if err := a.Connect(context.Background(), store.Component{ID: "pd-1", Config: []byte(cfg)}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if !strings.Contains(gotAuth, "resolved-key") {
		t.Errorf("Authorization = %q, want it to carry resolved-key (provider value should win)", gotAuth)
	}
}

func TestConnect_NoDiscriminator_PreservesLegacyToken(t *testing.T) {
	var gotAuth string
	srv := pdAuthServer(t, &gotAuth)

	a := pagerduty.New()
	cfg := fmt.Sprintf(`{"api_url":%q,"api_key":"legacy-key"}`, srv.URL)
	if err := a.Connect(context.Background(), store.Component{ID: "pd-2", Config: []byte(cfg)}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if !strings.Contains(gotAuth, "legacy-key") {
		t.Errorf("Authorization = %q, want it to carry legacy-key", gotAuth)
	}
}

func TestConnect_ResolveFailure_SurfacesWithoutCredential(t *testing.T) {
	a := pagerduty.New()
	cfg := `{"api_url":"http://127.0.0.1:1","api_key":"SUPERSECRET","credential_provider":"static","env_var":"JOE_DEFINITELY_UNSET_VAR_XYZ"}`
	err := a.Connect(context.Background(), store.Component{ID: "pd-3", Config: []byte(cfg)})
	if err == nil {
		t.Fatal("expected Connect to fail when the named env var is unset")
	}
	if strings.Contains(err.Error(), "SUPERSECRET") {
		t.Errorf("credential leaked into error: %v", err)
	}
}
