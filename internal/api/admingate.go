package api

import (
	"net/http"

	"github.com/jaimegago/joe/internal/rbac"
)

// requireAdmin is the single admin-gating mechanism for the Stream G
// phase G5 LLM-instrumentation HTTP endpoints (settings writes, the
// per-principal usage breakdown). It is the admin-side parallel to
// rbac.PrincipalFromContext: every gated handler reads the principal
// from context AND asks "is this caller an admin?" exactly once, here.
//
// The gate honours the system-wide auth-disabled permit convention.
// When RBAC enforcement is not active — i.e. services.RBACEnabled is
// false, which is the same predicate the policy engine and the
// accessor's rbac-disabled short-circuit use — the gate reports admin
// true unconditionally. This matches every other gate in the system
// (the accessor permits, the enforcement middleware is a pass-through,
// the bearer-auth middleware skips when no key is configured) and
// keeps local/dev runs unblocked without an admin row.
//
// When RBAC IS enforced, the gate consults the existing admin
// capability (services.RBAC.IsAdmin), the same call the policy engine
// uses for its admin short-circuit (rbac.PolicyEngine.Decide, Phase H,
// D-0011). On any read failure or a non-admin caller the handler
// writes the standard forbidden response shape (errorCodeForbidden,
// status 403) using writeError — exactly the body shape
// writeAccessError produces for accessor permission denials. Returns
// gated=true when the response was written and the caller must abort;
// gated=false means the caller is authorised and may proceed.
//
// The helper is intentionally NOT a wrapping http.HandlerFunc decorator
// — the gated handlers want the principal in the same scope as the
// rest of their body, and inlining `if p, gated := s.requireAdmin(...);
// gated { return }` is exactly the shape every gated endpoint needs.
func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) (principal rbac.Principal, gated bool) {
	principal = rbac.PrincipalFromContext(r.Context())
	// Auth-disabled permit. The accessor's rbac-disabled short-circuit
	// and the policy engine's nil-engine branch make the same choice;
	// re-deriving the predicate here would risk drift, so we read the
	// surfaced services.RBACEnabled signal instead.
	if s.services == nil || !s.services.RBACEnabled {
		return principal, false
	}
	// RBAC enforced. A nil repository here would be a wiring bug — the
	// engine-build site only sets RBACEnabled=true when policyEngine is
	// non-nil, which requires services.RBAC. Defend anyway so a stray
	// test harness that flips the flag without wiring the repo gets a
	// clean forbidden rather than a nil-deref.
	if s.services.RBAC == nil {
		writeError(w, http.StatusForbidden, errorCodeForbidden, "access denied: admin capability required")
		return principal, true
	}
	isAdmin, err := s.services.RBAC.IsAdmin(r.Context(), string(principal))
	if err != nil {
		// Treat a read failure as deny rather than allow. The
		// administrative surface fails CLOSED — operationally the
		// safer posture than fail-open admin access. A persistent
		// outage will be visible via the accompanying internal log.
		writeInternalError(w, err, "admin gate is_admin read")
		return principal, true
	}
	if !isAdmin {
		writeError(w, http.StatusForbidden, errorCodeForbidden, "access denied: admin capability required")
		return principal, true
	}
	return principal, false
}
