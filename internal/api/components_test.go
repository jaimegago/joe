package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/api"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/store"
	_ "modernc.org/sqlite"
)

// setupFullTestServer creates a test server with full store (graph + components tables).
func setupFullTestServer(t *testing.T) *api.Server {
	t.Helper()

	sqlStore, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { sqlStore.Close() })

	if err := sqlStore.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	services := &core.Services{
		Config:   &config.Config{},
		Graph:    graph.NewSQLiteStore(sqlStore.DB(), nil),
		Store:    sqlStore,
		Adapters: adapters.NewRegistry(),
	}

	return api.New(services, api.TestingPolicyEngine(services))
}

func TestHandleListComponents_Empty(t *testing.T) {
	server := setupFullTestServer(t)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/v1/components", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if int(body["count"].(float64)) != 0 {
		t.Errorf("count = %v, want 0", body["count"])
	}
}

func TestHandleListComponentTypes(t *testing.T) {
	server := setupFullTestServer(t)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/v1/component-types", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var body struct {
		ComponentTypes []string `json:"component_types"`
		Count          int      `json:"count"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	want := store.AllowedComponentTypes()
	if body.Count != len(want) {
		t.Errorf("count = %d, want %d (full enum)", body.Count, len(want))
	}
	if len(body.ComponentTypes) != len(want) {
		t.Fatalf("component_types len = %d, want %d", len(body.ComponentTypes), len(want))
	}
	for i, ty := range want {
		if body.ComponentTypes[i] != ty {
			t.Errorf("component_types[%d] = %q, want %q", i, body.ComponentTypes[i], ty)
		}
	}
}

func TestHandleCreateComponent_MissingFields(t *testing.T) {
	server := setupFullTestServer(t)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	req := httptest.NewRequest("POST", "/api/v1/components", strings.NewReader(`{"id":"test"}`))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleCreateComponent_InvalidJSON(t *testing.T) {
	server := setupFullTestServer(t)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	req := httptest.NewRequest("POST", "/api/v1/components", strings.NewReader(`{bad`))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// TestHandleCreateComponent_MalformedID pins the registration-time ID format
// gate: a malformed ID gets the standard bad-request error shape with a
// message naming the rule, and no component row is written.
func TestHandleCreateComponent_MalformedID(t *testing.T) {
	server := setupFullTestServer(t)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	for name, id := range map[string]string{
		"slash":     "prod/cluster",
		"space":     "prod cluster",
		"uppercase": "Prod-Cluster",
		"trailing":  "prod-",
	} {
		t.Run(name, func(t *testing.T) {
			body, _ := json.Marshal(map[string]any{
				"id": id, "type": "prometheus", "name": "Test",
			})
			req := httptest.NewRequest("POST", "/api/v1/components", strings.NewReader(string(body)))
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusBadRequest, w.Body.String())
			}
			var resp struct {
				Error   string `json:"error"`
				Message string `json:"message"`
			}
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if resp.Error != "invalid_request" {
				t.Errorf("error code = %q, want invalid_request", resp.Error)
			}
			if !strings.Contains(resp.Message, "lowercase") {
				t.Errorf("message %q does not state the format rule", resp.Message)
			}
		})
	}
}

func TestHandleCreateComponent_InvalidType(t *testing.T) {
	server := setupFullTestServer(t)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	req := httptest.NewRequest("POST", "/api/v1/components", strings.NewReader(`{"id":"src-1","type":"nope","name":"bad","config":{}}`))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var response struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Error != "invalid_component" {
		t.Errorf("error code: got %q, want %q", response.Error, "invalid_component")
	}
}

func TestHandleGetComponent_NotFound(t *testing.T) {
	server := setupFullTestServer(t)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/v1/components/nonexistent", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandleDeleteComponent_NotFound(t *testing.T) {
	server := setupFullTestServer(t)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	req := httptest.NewRequest("DELETE", "/api/v1/components/nonexistent", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// setupTestServerWithStore creates a test server and returns the underlying store.
func setupTestServerWithStore(t *testing.T) (*api.Server, *store.Store, *http.ServeMux) {
	t.Helper()

	sqlStore, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { sqlStore.Close() })

	if err := sqlStore.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	services := &core.Services{
		Config:   &config.Config{},
		Graph:    graph.NewSQLiteStore(sqlStore.DB(), nil),
		Store:    sqlStore,
		Adapters: adapters.NewRegistry(),
	}

	server := api.New(services, api.TestingPolicyEngine(services))
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	return server, sqlStore, mux
}

func TestHandleCreateComponent_DuplicateSource(t *testing.T) {
	_, sqlStore, mux := setupTestServerWithStore(t)

	// Pre-insert a source directly so the duplicate check triggers
	sqlStore.Components.Create(context.Background(), &store.Component{
		ID:     "src-dup",
		Type:   "kubernetes",
		Name:   "existing cluster",
		Config: json.RawMessage(`{}`),
	})

	body := `{"id":"src-dup","type":"kubernetes","name":"cluster"}`
	req := httptest.NewRequest("POST", "/api/v1/components", strings.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", w.Code, http.StatusConflict)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["error"] != "invalid_request" {
		t.Errorf("error code = %v, want invalid_request", resp["error"])
	}
}

// TestHandleCreateComponent_NoConnectProbe_KubernetesEmptyConfig pins the A003
// Stream G probe removal: an empty config used to FAIL at registration because the
// handler eagerly called the adapter's Connect (kubernetes requires api_server).
// Registration no longer connects — a credential-less record cannot authenticate —
// so the same request now succeeds (201). This is the behavioural half of the
// "probe is gone" guarantee (the structural half is the AST guard in
// components_governance_test.go). The fixture was git until
// trim-unsupported-component-types made it unregistrable; kubernetes carries the
// same premise (a Connect that requires config the empty blob lacks).
func TestHandleCreateComponent_NoConnectProbe_KubernetesEmptyConfig(t *testing.T) {
	_, _, mux := setupTestServerWithStore(t)

	body := `{"id":"k8s-1","type":"kubernetes","name":"test cluster","config":{}}`
	req := httptest.NewRequest("POST", "/api/v1/components", strings.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d — registration must not connect, so an empty kubernetes config is accepted", w.Code, http.StatusCreated)
	}
}

// TestHandleCreateComponent_AdapterTypesRegisterInert confirms every registrable
// adapter type registers as an inert, credential-less record with an empty config —
// no Connect probe runs at registration (A003 Stream G), so types that previously
// failed Connect with empty config now persist successfully (201). The
// adapter-bearing types that trim-unsupported-component-types made unregistrable
// (aws, azure, and the datastore group) moved to
// TestHandleCreateComponent_UnregistrableTypesRejected.
func TestHandleCreateComponent_AdapterTypesRegisterInert(t *testing.T) {
	adapterTypes := []string{
		"kubernetes",
		"prometheus", "mimir", "loki", "tempo", "jaeger",
		"alertmanager", "pagerduty", "grafana",
		"argocd", "terraform", "envoy", "falco",
	}

	for _, srcType := range adapterTypes {
		t.Run(srcType, func(t *testing.T) {
			_, _, mux := setupTestServerWithStore(t)
			body := `{"id":"src-1","type":"` + srcType + `","name":"test source","config":{}}`
			req := httptest.NewRequest("POST", "/api/v1/components", strings.NewReader(body))
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != http.StatusCreated {
				t.Errorf("type=%s: status %d, want 201 — registration is probe-free and credential-less", srcType, w.Code)
			}
		})
	}
}

// TestHandleCreateComponent_FallthroughTypes exercises source types that have no
// adapter connect step (fall through the switch) and are saved directly.
func TestHandleCreateComponent_FallthroughTypes(t *testing.T) {
	// Types that have no connect step (fall through the switch). The boot-only
	// group (github/gitlab/splunk/dynatrace/newrelic) is constructed only at boot
	// (connectSourcesDefault), so it registers OK here with no runtime connect
	// step. Types removed from this list because they are no longer registrable
	// (see TestHandleCreateComponent_UnregistrableTypesRejected):
	// oci_registry/artifactory/ecr by trim-deadonarrival-component-types, and
	// datadog (boot-only) plus helm/nginx-ingress (whose Connect succeeded with
	// empty config) by trim-unsupported-component-types.
	fallthroughTypes := []string{
		"github", "gitlab",
		"splunk", "dynatrace", "newrelic",
	}

	for _, srcType := range fallthroughTypes {
		t.Run(srcType, func(t *testing.T) {
			_, _, mux := setupTestServerWithStore(t)
			body := `{"id":"src-ft","type":"` + srcType + `","name":"fallthrough source","config":{}}`
			req := httptest.NewRequest("POST", "/api/v1/components", strings.NewReader(body))
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			// These types have no Connect() step, so the source should be saved successfully.
			if w.Code != http.StatusCreated {
				t.Errorf("type=%s: expected 201, got %d: %s", srcType, w.Code, w.Body.String())
			}
		})
	}
}

// TestHandleCreateComponent_UnregistrableTypesRejected asserts every type absent
// from the registrable set is rejected by the HTTP create endpoint with exactly the
// same invalid-type response a wholly unknown type gets. Two trims populated the
// set. trim-deadonarrival-component-types removed six with no construction path
// (oci_registry, dockerhub, artifactory, ecr have adapter packages wired into no
// construction map; cloudwatch and azuremonitor have no adapter code at all).
// trim-unsupported-component-types removed twelve that are not credential-wired, so
// promotion could never complete them into a working integration.
func TestHandleCreateComponent_UnregistrableTypesRejected(t *testing.T) {
	unregistrableTypes := []string{
		// No construction path (trim-deadonarrival-component-types).
		"oci_registry", "dockerhub", "artifactory", "ecr",
		"cloudwatch", "azuremonitor",
		// Not credential-wired (trim-unsupported-component-types).
		"azure", "helm", "nginx-ingress", "git", "aws", "datadog",
		"postgresql", "mysql", "redis", "mongodb", "kafka", "elasticsearch",
	}

	for _, srcType := range unregistrableTypes {
		t.Run(srcType, func(t *testing.T) {
			_, _, mux := setupTestServerWithStore(t)
			body := `{"id":"src-unreg","type":"` + srcType + `","name":"unregistrable","config":{}}`
			req := httptest.NewRequest("POST", "/api/v1/components", strings.NewReader(body))
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("type=%s: status = %d, want %d (same as unknown type): %s", srcType, w.Code, http.StatusBadRequest, w.Body.String())
			}
			var response struct {
				Error string `json:"error"`
			}
			if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
				t.Fatalf("type=%s: decode response: %v", srcType, err)
			}
			if response.Error != "invalid_component" {
				t.Errorf("type=%s: error code = %q, want %q (same as unknown type)", srcType, response.Error, "invalid_component")
			}
		})
	}
}

func TestHandleGetComponent_Success(t *testing.T) {
	_, sqlStore, mux := setupTestServerWithStore(t)

	sqlStore.Components.Create(context.Background(), &store.Component{
		ID:     "test-src",
		Type:   "git",
		Name:   "test repo",
		Config: json.RawMessage(`{}`),
	})

	req := httptest.NewRequest("GET", "/api/v1/components/test-src", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["id"] != "test-src" {
		t.Errorf("id = %v, want test-src", resp["id"])
	}
}

func TestHandleDeleteComponent_Success(t *testing.T) {
	_, sqlStore, mux := setupTestServerWithStore(t)

	sqlStore.Components.Create(context.Background(), &store.Component{
		ID:     "del-src",
		Type:   "kubernetes",
		Name:   "to delete",
		Config: json.RawMessage(`{}`),
	})

	req := httptest.NewRequest("DELETE", "/api/v1/components/del-src", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusNoContent, w.Body.String())
	}

	// Verify source is gone
	src, _ := sqlStore.Components.Get(context.Background(), "del-src")
	if src != nil {
		t.Error("source should have been deleted")
	}
}
