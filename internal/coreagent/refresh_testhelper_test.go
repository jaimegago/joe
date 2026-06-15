package coreagent

import (
	"github.com/jaimegago/joe/internal/access"
	"github.com/jaimegago/joe/internal/core"
)

// permitAllAccessor builds a REAL *access.Accessor over the test's registry and
// graph store with a NIL policy engine. A nil engine means "RBAC disabled", so
// every decision is permitted — the same fail-open-for-tests posture the
// transport uses when auth is not configured (rbac.EnforcementMiddleware(nil)).
//
// This is the test-side replacement for the OLD production raw-registry fallback
// that resolveAdapter used when r.accessor == nil (removed in A001-COREGOV
// CC-08, which made that nil path fail CLOSED). Harnesses that exercise
// refresh/refreshComponent/resolveAdapter must now route through the guarded
// seam; this helper gives them a permit-all seam over their own registry so
// behavior is unchanged WITHOUT reintroducing any production fail-open path.
//
// It deliberately does NOT use the promote-aware engine: the CC-05 behavior
// tests in refresh_access_test.go own the deny/promote/live-flip/non-agent-core
// matrix with a real promote-aware engine. The helper's job is only to keep the
// pre-existing "adapter resolves, switch on type" coverage tests green.
func permitAllAccessor(svc *core.Services) *access.Accessor {
	return access.New(svc.Adapters, svc.Graph, nil, nil)
}

// withPermitAllAccessor wires a permit-all accessor (over r's own services) onto
// the Refresher and returns it, so a test literal can be written as
// withPermitAllAccessor(&Refresher{services: svc, logger: ...}).
func withPermitAllAccessor(r *Refresher) *Refresher {
	r.accessor = permitAllAccessor(r.services)
	return r
}
