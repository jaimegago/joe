package api

import (
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/rbac"
)

// TestingPolicyEngine reproduces the engine the deleted api.newPolicyEngine used
// to build internally for the guarded accessor, so the ~test callers of api.New
// preserve their exact pre-refactor accessor behaviour after api.New became
// engine-injected (rbac-engine-split). It is the same three-part predicate:
// nil when Config or RBAC is unwired, nil when RBAC is not enabled, and a bare
// rbac.NewPolicyEngine(RBAC) otherwise (no read-posture / promote resolvers —
// tests that need those inject them explicitly).
//
// It lives in a package api _test file so it is visible unqualified to internal
// (package api) tests and as api.TestingPolicyEngine to external (package
// api_test) tests, and — being test-only — is exempt from the static guard that
// forbids rbac.NewPolicyEngine* outside cmd/joe.
func TestingPolicyEngine(services *core.Services) *rbac.PolicyEngine {
	if services == nil || services.Config == nil || services.RBAC == nil {
		return nil
	}
	if !services.Config.RBACEnabled() {
		return nil
	}
	return rbac.NewPolicyEngine(services.RBAC)
}
