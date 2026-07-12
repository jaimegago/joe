package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/store"
)

// locatorKeys are the credential reference locator fields that an armed
// component's Config blob carries (plus the raw blob key itself). The A002
// read-model fix forbids ANY of these from appearing on GET /api/v1/components
// or GET /api/v1/components/{id} — the read model replaces the blob with a
// derived armed/provider projection.
var forbiddenReadKeys = []string{
	"config", "credential_provider", "env_var", "in_cluster",
	"tenant_id", "client_id", "client_secret_env_var", "audience", "value",
}

// armedComponentConfig is a fully-populated armed reference: a superset of the
// credential locators the wired kubernetes providers can write (static-bearer's
// env_var/in_cluster and entra-exchange's tenant/client/secret), so the absence
// assertions exercise the worst case (a real promotion writes only a subset).
const armedComponentConfig = `{"credential_provider":"static-bearer","auth_method":"static-bearer","api_server":"https://k8s.prod","ca_data":"PEMDATA","in_cluster":true,"env_var":"GH_TOKEN","tenant_id":"t","client_id":"c","client_secret_env_var":"SECRET","audience":"github","endpoint":"https://k8s.prod"}`

func assertNoLocatorKeys(t *testing.T, raw string) {
	t.Helper()
	for _, k := range forbiddenReadKeys {
		if strings.Contains(raw, "\""+k+"\"") {
			t.Errorf("read response leaks forbidden key %q; body: %s", k, raw)
		}
	}
}

// TestComponentReadModel_ArmedHidesLocators_GetAndList pins the A002 acceptance
// criteria: for an armed component neither read endpoint returns any credential
// locator or the raw config blob, and both surface the derived armed=true +
// provider Kind from the SAME projection shape.
func TestComponentReadModel_ArmedHidesLocators_GetAndList(t *testing.T) {
	_, sqlStore, mux := setupTestServerWithStore(t)

	sqlStore.Components.Create(context.Background(), &store.Component{
		ID:     "k8s-prod",
		Type:   "kubernetes",
		Name:   "Production K8s",
		Config: json.RawMessage(armedComponentConfig),
	})

	// Single-get
	req := httptest.NewRequest("GET", "/api/v1/components/k8s-prod", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	getBody := w.Body.String()
	assertNoLocatorKeys(t, getBody)

	var got struct {
		ID       string `json:"id"`
		Armed    bool   `json:"armed"`
		Provider string `json:"provider"`
	}
	if err := json.Unmarshal([]byte(getBody), &got); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	if !got.Armed {
		t.Errorf("GET armed = false, want true for promoted component")
	}
	if got.Provider != "static-bearer" {
		t.Errorf("GET provider = %q, want static-bearer", got.Provider)
	}

	// List
	req = httptest.NewRequest("GET", "/api/v1/components", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("LIST status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	listBody := w.Body.String()
	assertNoLocatorKeys(t, listBody)

	var list struct {
		Components []struct {
			ID       string `json:"id"`
			Armed    bool   `json:"armed"`
			Provider string `json:"provider"`
		} `json:"components"`
	}
	if err := json.Unmarshal([]byte(listBody), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Components) != 1 {
		t.Fatalf("list len = %d, want 1", len(list.Components))
	}
	if !list.Components[0].Armed || list.Components[0].Provider != "static-bearer" {
		t.Errorf("LIST projection = %+v, want armed=true provider=static-bearer", list.Components[0])
	}
}

// TestComponentReadModel_CreateEchoHidesLocators pins the create-echo half of the
// read-model closure: the POST /api/v1/components 201 body serializes the SAME
// componentView projection the GET endpoints return, so a registration echo can no
// more leak a credential locator or the raw Config blob than a read can. The
// submitted config deliberately carries locator-shaped keys that PASS the
// credential-less-at-registration guard (audience is excluded from the
// credential-bearing set; datastore secret names uri/password/api_key are unknown
// to it), so the record registers 201 and those keys land in the stored Config —
// making the absence assertion on the 201 body a genuine regression guard: were the
// handler to echo the raw store.Component, the blob (and its audience key) would
// appear. RBAC is disabled in the test harness, so requireAdmin permits the create.
func TestComponentReadModel_CreateEchoHidesLocators(t *testing.T) {
	_, _, mux := setupTestServerWithStore(t)

	body := `{"id":"echo-src","type":"mongodb","name":"Echo Store","config":` +
		`{"audience":"github","uri":"mongodb://user:password@host:27017/db","password":"p","api_key":"k","endpoint":"mongodb://host:27017"}}`
	req := httptest.NewRequest("POST", "/api/v1/components", strings.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("CREATE status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
	createBody := w.Body.String()
	assertNoLocatorKeys(t, createBody)

	// The echo is the read-model shape: identity fields present, and an inert
	// (credential-less) registration projects armed=false with no provider.
	var got struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Name     string `json:"name"`
		Armed    bool   `json:"armed"`
		Provider string `json:"provider"`
	}
	if err := json.Unmarshal([]byte(createBody), &got); err != nil {
		t.Fatalf("decode create echo: %v", err)
	}
	if got.ID != "echo-src" || got.Type != "mongodb" || got.Name != "Echo Store" {
		t.Errorf("create echo identity = %+v, want id=echo-src type=mongodb name=Echo Store", got)
	}
	if got.Armed {
		t.Errorf("create echo armed = true, want false for a credential-less registration")
	}
	if got.Provider != "" {
		t.Errorf("create echo provider = %q, want empty for an inert component", got.Provider)
	}
}

// TestComponentReadModel_InertArmedFalse pins armed=false (and no provider) for
// an inert, never-promoted component on both read endpoints.
func TestComponentReadModel_InertArmedFalse(t *testing.T) {
	_, sqlStore, mux := setupTestServerWithStore(t)

	sqlStore.Components.Create(context.Background(), &store.Component{
		ID:     "prod-prometheus",
		Type:   "prometheus",
		Name:   "Production Prometheus",
		Config: json.RawMessage(`{"endpoint":"https://prom.prod"}`),
	})

	for _, tc := range []struct {
		name string
		path string
		list bool
	}{
		{"get", "/api/v1/components/prod-prometheus", false},
		{"list", "/api/v1/components", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tc.path, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
			}
			body := w.Body.String()
			assertNoLocatorKeys(t, body)

			var armed, hasProvider bool
			if tc.list {
				var list struct {
					Components []struct {
						Armed    bool   `json:"armed"`
						Provider string `json:"provider"`
					} `json:"components"`
				}
				if err := json.Unmarshal([]byte(body), &list); err != nil {
					t.Fatalf("decode: %v", err)
				}
				if len(list.Components) != 1 {
					t.Fatalf("list len = %d, want 1", len(list.Components))
				}
				armed = list.Components[0].Armed
				hasProvider = list.Components[0].Provider != ""
			} else {
				var got struct {
					Armed    bool   `json:"armed"`
					Provider string `json:"provider"`
				}
				if err := json.Unmarshal([]byte(body), &got); err != nil {
					t.Fatalf("decode: %v", err)
				}
				armed = got.Armed
				hasProvider = got.Provider != ""
			}
			if armed {
				t.Errorf("armed = true, want false for inert component")
			}
			if hasProvider {
				t.Errorf("provider present, want absent for inert component")
			}
		})
	}
}
