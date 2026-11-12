//go:build e2e
// +build e2e

package e2e

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// TestE2E_RegistrationContract drives the declared per-type registration
// contract (D-0168) over joe's public API, behind the harness's scripted
// responder. Registration now accepts exactly the non-credential routing fields
// each type's adapter reads: a registration-required locator must be present,
// every value must have its declared JSON type, an undeclared field — a secret
// among them — is refused, and the contract is served to the Register dialog as
// a per-type descriptor on GET /api/v1/component-types.
//
// The scenarios are the ones joe-pm thread registration-routing-locator names.
func TestE2E_RegistrationContract(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	// A stand-in Prometheus the test owns. Test Connection on a component dials
	// the url its STORED config carries and asks for buildinfo, so a request
	// arriving here proves the url the registration sent is the one joe kept —
	// read back through the public API, never through the database.
	var buildinfo atomic.Int32
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/status/buildinfo" {
			http.NotFound(w, r)
			return
		}
		buildinfo.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"version":"2.53.0"}}`))
	}))
	defer prom.Close()

	h := NewTestHarness(t)
	defer h.Stop()
	if err := h.BootstrapAdmin("svc:e2e"); err != nil {
		t.Fatal(err)
	}
	if err := h.Start(); err != nil {
		t.Fatalf("failed to start harness: %v", err)
	}

	register := func(t *testing.T, body map[string]any) (int, string) {
		t.Helper()
		status, raw, err := h.Do(http.MethodPost, "/api/v1/components", e2eServiceAccountKey, body)
		if err != nil {
			t.Fatalf("POST /api/v1/components: %v", err)
		}
		return status, string(raw)
	}
	// exists reports whether GET /api/v1/components/{id} finds the component.
	exists := func(t *testing.T, id string) bool {
		t.Helper()
		status, raw, err := h.Do(http.MethodGet, "/api/v1/components/"+id, e2eServiceAccountKey, nil)
		if err != nil {
			t.Fatalf("GET /api/v1/components/%s: %v", id, err)
		}
		switch status {
		case http.StatusOK:
			return true
		case http.StatusNotFound:
			return false
		}
		t.Fatalf("GET /api/v1/components/%s: status %d: %s", id, status, raw)
		return false
	}
	refused := func(t *testing.T, body map[string]any, want string) {
		t.Helper()
		status, raw := register(t, body)
		if status != http.StatusBadRequest {
			t.Fatalf("register %v: status %d, want 400: %s", body, status, raw)
		}
		if !strings.Contains(raw, want) {
			t.Errorf("register %v: refusal %s does not say %q", body, raw, want)
		}
		if exists(t, body["id"].(string)) {
			t.Errorf("register %v was refused but the component exists", body)
		}
	}

	t.Run("prometheus with a url is created and keeps it", func(t *testing.T) {
		status, raw := register(t, map[string]any{
			"id": "prom-located", "type": "prometheus", "name": "prom-located",
			"config": map[string]any{"url": prom.URL},
		})
		if status != http.StatusCreated {
			t.Fatalf("register prometheus with a url: status %d, want 201: %s", status, raw)
		}
		status, body, err := h.Do(http.MethodPost, "/api/v1/components/prom-located/test", e2eServiceAccountKey, map[string]any{})
		if err != nil {
			t.Fatalf("test connection: %v", err)
		}
		var result struct {
			OK      bool   `json:"ok"`
			Message string `json:"message"`
		}
		if status != http.StatusOK || json.Unmarshal(body, &result) != nil {
			t.Fatalf("test connection: status %d: %s", status, body)
		}
		if !result.OK || buildinfo.Load() == 0 {
			t.Fatalf("test connection ok=%v (%q) with %d buildinfo request(s) at %s; want the stored url dialled",
				result.OK, result.Message, buildinfo.Load(), prom.URL)
		}
	})

	t.Run("prometheus without a url is refused", func(t *testing.T) {
		refused(t, map[string]any{"id": "prom-bare", "type": "prometheus", "name": "prom-bare"}, `\"url\" is required`)
	})

	t.Run("an undeclared field is refused", func(t *testing.T) {
		refused(t, map[string]any{
			"id": "gh-endpoint", "type": "github", "name": "gh-endpoint",
			"config": map[string]any{"endpoint": "https://github.example.com"},
		}, `\"endpoint\" is not a registration field`)
	})

	t.Run("newrelic with a string account_id is refused", func(t *testing.T) {
		refused(t, map[string]any{
			"id": "nr-string", "type": "newrelic", "name": "nr-string",
			"config": map[string]any{"account_id": "1234567"},
		}, "must be a JSON integer")
	})

	t.Run("an api_key is refused", func(t *testing.T) {
		refused(t, map[string]any{
			"id": "prom-keyed", "type": "prometheus", "name": "prom-keyed",
			"config": map[string]any{"url": prom.URL, "api_key": "inline-secret"},
		}, `\"api_key\" is a secret`)
	})

	t.Run("kubernetes with no config is created", func(t *testing.T) {
		status, raw := register(t, map[string]any{"id": "k8s-bare", "type": "kubernetes", "name": "k8s-bare"})
		if status != http.StatusCreated {
			t.Fatalf("register kubernetes with no config: status %d, want 201: %s", status, raw)
		}
		if !exists(t, "k8s-bare") {
			t.Fatal("kubernetes registered 201 but is not found")
		}
	})

	t.Run("the descriptor route serves the contract and no secret", func(t *testing.T) {
		status, raw, err := h.Do(http.MethodGet, "/api/v1/component-types", e2eServiceAccountKey, nil)
		if err != nil || status != http.StatusOK {
			t.Fatalf("GET /api/v1/component-types: status %d, err %v: %s", status, err, raw)
		}
		type field struct {
			Name  string `json:"name"`
			Type  string `json:"type"`
			Phase string `json:"phase"`
		}
		var body struct {
			Descriptors []struct {
				Name   string  `json:"name"`
				Fields []field `json:"fields"`
			} `json:"descriptors"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("decode %s: %v", raw, err)
		}
		fields := map[string][]field{}
		for _, d := range body.Descriptors {
			fields[d.Name] = d.Fields
		}
		var url *field
		for i, f := range fields["prometheus"] {
			if f.Name == "url" {
				url = &fields["prometheus"][i]
			}
		}
		if url == nil || url.Type != "url" || url.Phase != "registration" {
			t.Errorf("prometheus descriptor = %+v; want url, typed url, required at registration", fields["prometheus"])
		}
		gh, ok := fields["github"]
		if !ok {
			t.Fatal("no github descriptor")
		}
		for _, f := range gh {
			switch f.Name {
			case "token", "webhook_secret", "api_key", "env_var", "value", "credential_provider":
				t.Errorf("github descriptor carries secret or credential field %q", f.Name)
			}
		}
	})
}
