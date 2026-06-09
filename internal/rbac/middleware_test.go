package rbac_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaimegago/joe/internal/rbac"
)

// staticProvider always returns the given principal.
type staticProvider struct{ p rbac.Principal }

func (s *staticProvider) Identity(_ *http.Request) rbac.Principal { return s.p }

func TestIdentityMiddleware_InjectsContext(t *testing.T) {
	var captured rbac.Principal
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = rbac.PrincipalFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	mw := rbac.IdentityMiddleware(&staticProvider{"alice"})
	handler := mw(next)

	r := httptest.NewRequest("GET", "/api/v1/k8s/cluster-1/resources", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if captured != "alice" {
		t.Errorf("expected captured principal alice, got %q", captured)
	}
}

// TestEnforcementMiddleware_Passthrough proves the Phase E demotion: the
// middleware no longer evaluates per-zone IsAllowed and instead passes every
// request through, regardless of component path, HTTP method, or engine
// configuration. Per-zone enforcement now lives EXCLUSIVELY in the guarded
// accessor (internal/access), which both HTTP handlers and the in-process
// agent-loop reach. The Phase E equivalence test in internal/api proves the
// accessor alone produces the same 200/403/401 outcomes the prior
// middleware+accessor chain produced.
func TestEnforcementMiddleware_Passthrough(t *testing.T) {
	db := openTestDB(t)
	repo := rbac.NewRepository(db, "sqlite")
	engine := rbac.NewPolicyEngine(repo)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	cases := []struct {
		name   string
		engine *rbac.PolicyEngine
		method string
		path   string
	}{
		{"non-component path with engine", engine, "GET", "/api/v1/status"},
		{"component path GET with engine", engine, "GET", "/api/v1/k8s/k8s-prod/resources"},
		{"component path POST with engine", engine, "POST", "/api/v1/k8s/k8s-prod/resources"},
		{"component path DELETE with engine", engine, "DELETE", "/api/v1/k8s/k8s-prod/resources/pod/default/p"},
		{"non-component path nil engine", nil, "GET", "/api/v1/status"},
		{"component path GET nil engine", nil, "GET", "/api/v1/k8s/k8s-prod/resources"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := rbac.EnforcementMiddleware(tc.engine)(next)
			r := httptest.NewRequest(tc.method, tc.path, nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != http.StatusOK {
				t.Errorf("expected 200 (middleware now pass-through), got %d", w.Code)
			}
		})
	}
}
