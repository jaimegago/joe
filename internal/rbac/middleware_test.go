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

	r := httptest.NewRequest("GET", "/api/v1/probe/cluster-1/read", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if captured != "alice" {
		t.Errorf("expected captured principal alice, got %q", captured)
	}
}

// TestEnforcementMiddleware_Passthrough was deleted by rbac-engine-split along
// with rbac.EnforcementMiddleware itself. The middleware had been a pass-through
// since the Phase E demotion (D-0008); with it removed the guarded accessor
// (internal/access) is the sole authoritative RBAC gate, and the accessor's own
// tests plus the cmd/joe rbac-engine-split regression pin cover the 200/403/401
// outcomes this test used to assert against the pass-through.
