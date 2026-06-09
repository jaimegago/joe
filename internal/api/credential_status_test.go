package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/store"
)

// credStatusSentinel is a fake secret planted in a component's credential config.
// The whole point of D-0026's two-half resolved-credential type is that this value
// reaches an adapter but NEVER a serialized response. These tests extend the
// credential package's break-test (TestBreakCredentialHalfNeverLeaks) to the API
// layer: the passive status listing and the live probe response must both carry
// the diagnostic half and never the sentinel.
const credStatusSentinel = "S3NTINEL-api-static-secret-zzz"

// seedCredComponent inserts a component whose static credential config embeds the
// sentinel, so the leak assertions exercise a real configured secret.
func seedCredComponent(t *testing.T, f *llmadminFixture, id string) {
	t.Helper()
	cfg := `{"credential_provider":"static","value":"` + credStatusSentinel + `","audience":"github"}`
	if err := f.store.Components.Create(context.Background(), &store.Component{
		ID:     id,
		Type:   store.ComponentTypeGit,
		Name:   "Seeded " + id,
		Config: json.RawMessage(cfg),
	}); err != nil {
		t.Fatalf("seed component %q: %v", id, err)
	}
}

// (gate) All three credential-status endpoints reject a non-admin with 403 — the
// same D-0012 gate every other admin route carries.
func TestCredentialStatus_NonAdminForbidden(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	seedCredComponent(t, f, "comp-1")

	cases := []struct {
		method, path string
	}{
		{http.MethodGet, "/api/v1/admin/credential-status"},
		{http.MethodPost, "/api/v1/admin/credential-status/comp-1/probe"},
		{http.MethodPost, "/api/v1/admin/credential-status/comp-1/probe/stderr"},
	}
	for _, c := range cases {
		w := f.do(c.method, c.path, "", "user:bob")
		if w.Code != http.StatusForbidden {
			t.Errorf("%s %s as non-admin: status=%d body=%s; want 403", c.method, c.path, w.Code, w.Body.String())
		}
	}
}

// (passive, no leak) The Describe listing returns the non-sensitive descriptor
// (provider kind, audience) for the seeded component and NEVER the sentinel.
func TestCredentialStatus_PassiveDescribeNeverLeaks(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	seedCredComponent(t, f, "comp-1")

	w := f.do(http.MethodGet, "/api/v1/admin/credential-status", "", "user:alice")
	if w.Code != http.StatusOK {
		t.Fatalf("list credential status: status=%d body=%s; want 200", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if strings.Contains(body, credStatusSentinel) {
		t.Fatalf("LEAK: passive credential-status listing contains the sentinel secret: %s", body)
	}
	// Sanity: the diagnostic half DID pass through, so the test exercises real
	// output rather than an empty body.
	if !strings.Contains(body, "comp-1") || !strings.Contains(body, "static") || !strings.Contains(body, "github") {
		t.Fatalf("expected descriptor (component id, provider, audience) in listing: %s", body)
	}
	// And the read was audited (fail-open read-class verb).
	if n := f.countAudit(audit.ActionAdminCredentialStatusRead); n == 0 {
		t.Errorf("expected a credential_status.read audit row for the listing")
	}
}

// (probe, no leak) The live probe response carries the staged Diagnostic — for a
// static credential it reaches connectivity-probed — and NEVER the sentinel; the
// captured-stderr field is absent (static has none).
func TestCredentialProbe_StagedResultNeverLeaks(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	seedCredComponent(t, f, "comp-1")

	w := f.do(http.MethodPost, "/api/v1/admin/credential-status/comp-1/probe", "", "user:alice")
	if w.Code != http.StatusOK {
		t.Fatalf("probe credential: status=%d body=%s; want 200", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if strings.Contains(body, credStatusSentinel) {
		t.Fatalf("LEAK: probe response contains the sentinel secret: %s", body)
	}

	// The Stage type serializes to a string for the FE (Zod) but has no Go
	// UnmarshalJSON, so assert on the rendered body. Static Resolve succeeds and
	// the static Probe is a no-op success, so the staged result reaches
	// connectivity-probed and is ok, with no captured stderr.
	for _, want := range []string{`"component_id":"comp-1"`, `"stage":"connectivity-probed"`, `"ok":true`, `"stderr_available":false`} {
		if !strings.Contains(body, want) {
			t.Errorf("probe response missing %s: %s", want, body)
		}
	}
}

// (stderr endpoint, no leak) The explicit stderr endpoint returns its own
// response shape; for a static credential there is no captured stderr and the
// sentinel never appears.
func TestCredentialStderr_NeverLeaks(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	seedCredComponent(t, f, "comp-1")

	w := f.do(http.MethodPost, "/api/v1/admin/credential-status/comp-1/probe/stderr", "", "user:alice")
	if w.Code != http.StatusOK {
		t.Fatalf("credential stderr: status=%d body=%s; want 200", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), credStatusSentinel) {
		t.Fatalf("LEAK: stderr response contains the sentinel secret: %s", w.Body.String())
	}
	var resp credentialStderrResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode stderr response: %v", err)
	}
	if resp.Stderr != "" {
		t.Errorf("static credential should carry no captured stderr, got %q", resp.Stderr)
	}
}

// (404) Probing an unknown component as an admin is a clean 404, not a 500.
func TestCredentialProbe_UnknownComponent(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")

	w := f.do(http.MethodPost, "/api/v1/admin/credential-status/nope/probe", "", "user:alice")
	if w.Code != http.StatusNotFound {
		t.Fatalf("probe unknown component: status=%d body=%s; want 404", w.Code, w.Body.String())
	}
}
