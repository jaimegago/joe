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
	"config", "credential_provider", "env_var", "kubeconfig",
	"context", "in_cluster", "audience", "value",
}

// armedComponentConfig is a fully-populated armed reference: every credential
// locator the two wired providers can write, so the absence assertions exercise
// the worst case (a real promotion writes only a subset).
const armedComponentConfig = `{"credential_provider":"kubeconfig-exec","kubeconfig":"/home/op/.kube/config","context":"prod","in_cluster":true,"env_var":"GH_TOKEN","audience":"github","endpoint":"https://k8s.prod"}`

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
	if got.Provider != "kubeconfig-exec" {
		t.Errorf("GET provider = %q, want kubeconfig-exec", got.Provider)
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
	if !list.Components[0].Armed || list.Components[0].Provider != "kubeconfig-exec" {
		t.Errorf("LIST projection = %+v, want armed=true provider=kubeconfig-exec", list.Components[0])
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
