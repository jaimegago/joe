package argocd_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/adapters/gitops/argocd"
	"github.com/jaimegago/joe/internal/store"
)

// argoAuthServer answers the version ping 200 and records the Authorization
// header (Bearer <token>) so a test can assert which token the seam routed.
func argoAuthServer(t *testing.T, gotAuth *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"Version":"v2.9.0"}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestConnect_StaticProvider_ResolvesInlineValue(t *testing.T) {
	var gotAuth string
	srv := argoAuthServer(t, &gotAuth)

	a := argocd.New()
	cfg := fmt.Sprintf(`{"url":%q,"token":"placeholder","credential_provider":"static","value":"resolved-tok"}`, srv.URL)
	if err := a.Connect(context.Background(), store.Component{ID: "argo-1", Config: []byte(cfg)}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if !strings.Contains(gotAuth, "resolved-tok") {
		t.Errorf("Authorization = %q, want it to carry resolved-tok (provider value should win)", gotAuth)
	}
}

func TestConnect_NoDiscriminator_PreservesLegacyToken(t *testing.T) {
	var gotAuth string
	srv := argoAuthServer(t, &gotAuth)

	a := argocd.New()
	cfg := fmt.Sprintf(`{"url":%q,"token":"legacy-tok"}`, srv.URL)
	if err := a.Connect(context.Background(), store.Component{ID: "argo-2", Config: []byte(cfg)}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if !strings.Contains(gotAuth, "legacy-tok") {
		t.Errorf("Authorization = %q, want it to carry legacy-tok", gotAuth)
	}
}

func TestConnect_ResolveFailure_SurfacesWithoutCredential(t *testing.T) {
	a := argocd.New()
	cfg := `{"url":"http://127.0.0.1:1","token":"SUPERSECRET","credential_provider":"static","env_var":"JOE_DEFINITELY_UNSET_VAR_XYZ"}`
	err := a.Connect(context.Background(), store.Component{ID: "argo-3", Config: []byte(cfg)})
	if err == nil {
		t.Fatal("expected Connect to fail when the named env var is unset")
	}
	if strings.Contains(err.Error(), "SUPERSECRET") {
		t.Errorf("credential leaked into error: %v", err)
	}
}
