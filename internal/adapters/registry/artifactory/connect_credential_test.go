package artifactory_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/adapters/registry/artifactory"
	"github.com/jaimegago/joe/internal/store"
)

// artifactoryAuthServer answers the ping 200 and records the X-JFrog-Art-Api
// header (the single api-key token) so a test can assert which token the seam
// routed onto the wire.
func artifactoryAuthServer(t *testing.T, gotKey *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*gotKey = r.Header.Get("X-JFrog-Art-Api")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestConnect_StaticProvider_ResolvesInlineValue(t *testing.T) {
	var gotKey string
	srv := artifactoryAuthServer(t, &gotKey)

	a := artifactory.New()
	cfg := fmt.Sprintf(`{"base_url":%q,"api_key":"placeholder","credential_provider":"static","value":"resolved-tok"}`, srv.URL)
	if err := a.Connect(context.Background(), store.Component{ID: "artf-1", Config: []byte(cfg)}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if gotKey != "resolved-tok" {
		t.Errorf("X-JFrog-Art-Api = %q, want resolved-tok (provider value should win)", gotKey)
	}
}

func TestConnect_NoDiscriminator_PreservesLegacyToken(t *testing.T) {
	var gotKey string
	srv := artifactoryAuthServer(t, &gotKey)

	a := artifactory.New()
	cfg := fmt.Sprintf(`{"base_url":%q,"api_key":"legacy-tok"}`, srv.URL)
	if err := a.Connect(context.Background(), store.Component{ID: "artf-2", Config: []byte(cfg)}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if gotKey != "legacy-tok" {
		t.Errorf("X-JFrog-Art-Api = %q, want legacy-tok", gotKey)
	}
}

func TestConnect_ResolveFailure_SurfacesWithoutCredential(t *testing.T) {
	a := artifactory.New()
	cfg := `{"base_url":"http://127.0.0.1:1","api_key":"SUPERSECRET","credential_provider":"static","env_var":"JOE_DEFINITELY_UNSET_VAR_XYZ"}`
	err := a.Connect(context.Background(), store.Component{ID: "artf-3", Config: []byte(cfg)})
	if err == nil {
		t.Fatal("expected Connect to fail when the named env var is unset")
	}
	if strings.Contains(err.Error(), "SUPERSECRET") {
		t.Errorf("credential leaked into error: %v", err)
	}
}
