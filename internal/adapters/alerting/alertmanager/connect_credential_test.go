package alertmanager_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/adapters/alerting/alertmanager"
	"github.com/jaimegago/joe/internal/store"
)

// amAuthServer answers the status health check 200 and records the Authorization
// header so a test can assert which token the seam routed.
func amAuthServer(t *testing.T, gotAuth *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"cluster":{"status":"ready"}}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestConnect_StaticProvider_ResolvesInlineValue(t *testing.T) {
	var gotAuth string
	srv := amAuthServer(t, &gotAuth)

	a := alertmanager.New()
	cfg := fmt.Sprintf(`{"url":%q,"api_key":"placeholder","credential_provider":"static","value":"resolved-tok"}`, srv.URL)
	if err := a.Connect(context.Background(), store.Component{ID: "am-1", Config: []byte(cfg)}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if !strings.Contains(gotAuth, "resolved-tok") {
		t.Errorf("Authorization = %q, want it to carry resolved-tok (provider value should win)", gotAuth)
	}
}

func TestConnect_NoDiscriminator_PreservesLegacyToken(t *testing.T) {
	var gotAuth string
	srv := amAuthServer(t, &gotAuth)

	a := alertmanager.New()
	cfg := fmt.Sprintf(`{"url":%q,"api_key":"legacy-tok"}`, srv.URL)
	if err := a.Connect(context.Background(), store.Component{ID: "am-2", Config: []byte(cfg)}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if !strings.Contains(gotAuth, "legacy-tok") {
		t.Errorf("Authorization = %q, want it to carry legacy-tok", gotAuth)
	}
}

func TestConnect_ResolveFailure_SurfacesWithoutCredential(t *testing.T) {
	a := alertmanager.New()
	cfg := `{"url":"http://127.0.0.1:1","api_key":"SUPERSECRET","credential_provider":"static","env_var":"JOE_DEFINITELY_UNSET_VAR_XYZ"}`
	err := a.Connect(context.Background(), store.Component{ID: "am-3", Config: []byte(cfg)})
	if err == nil {
		t.Fatal("expected Connect to fail when the named env var is unset")
	}
	if strings.Contains(err.Error(), "SUPERSECRET") {
		t.Errorf("credential leaked into error: %v", err)
	}
}
