package newrelic_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/adapters/observability/newrelic"
	"github.com/jaimegago/joe/internal/store"
)

// capturingDoer records the Api-Key header of the NerdGraph request. New Relic's
// endpoint URL is fixed by region, so the credential cannot be observed via a
// test server; an injected doer captures it instead.
type capturingDoer struct {
	gotAPIKey string
}

func (d *capturingDoer) Do(req *http.Request) (*http.Response, error) {
	d.gotAPIKey = req.Header.Get("Api-Key")
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"data":{"actor":{"user":{"name":"joe"}}}}`)),
	}, nil
}

func TestConnect_StaticProvider_ResolvesInlineValue(t *testing.T) {
	d := &capturingDoer{}
	a := newrelic.NewWithClient(d)
	cfg := `{"api_key":"placeholder","account_id":12345,"credential_provider":"static","value":"resolved-tok"}`
	if err := a.Connect(context.Background(), store.Component{ID: "nr-1", Config: []byte(cfg)}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if d.gotAPIKey != "resolved-tok" {
		t.Errorf("Api-Key = %q, want resolved-tok (provider value should win)", d.gotAPIKey)
	}
}

func TestConnect_NoDiscriminator_PreservesLegacyToken(t *testing.T) {
	d := &capturingDoer{}
	a := newrelic.NewWithClient(d)
	cfg := `{"api_key":"legacy-tok","account_id":12345}`
	if err := a.Connect(context.Background(), store.Component{ID: "nr-2", Config: []byte(cfg)}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if d.gotAPIKey != "legacy-tok" {
		t.Errorf("Api-Key = %q, want legacy-tok", d.gotAPIKey)
	}
}

func TestConnect_ResolveFailure_SurfacesWithoutCredential(t *testing.T) {
	d := &capturingDoer{}
	a := newrelic.NewWithClient(d)
	cfg := `{"api_key":"SUPERSECRET","account_id":12345,"credential_provider":"static","env_var":"JOE_DEFINITELY_UNSET_VAR_XYZ"}`
	err := a.Connect(context.Background(), store.Component{ID: "nr-3", Config: []byte(cfg)})
	if err == nil {
		t.Fatal("expected Connect to fail when the named env var is unset")
	}
	if strings.Contains(err.Error(), "SUPERSECRET") {
		t.Errorf("credential leaked into error: %v", err)
	}
}
