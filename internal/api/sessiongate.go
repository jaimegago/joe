package api

import (
	"context"

	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/sessionauthz"
)

// sessiongate.go wires the dedicated session-authorization seam
// (internal/sessionauthz, DESIGN-CHAT-SESSIONS.md §12.7 / ledger node B003) into
// the HTTP layer. The seam is the SINGLE enforcement point for session
// authorization: the per-user owner-mutate handlers call (*Server).sessionAccess
// and act on the returned Decision; no handler reimplements ownership. The
// structural bypass guard in sessionauthz_guard_test.go fails the build if a
// session-mutation call site appears outside the seam-gated allowlist.
//
// Two-instance defense in depth (DESIGN-CHAT-SESSIONS.md §12.8, ledger node
// B005). The seam is constructed PER RESOURCE CLASS, but it is ALSO constructed
// per ROUTE NAMESPACE with a different admin checker so that the per-user routes
// can never resolve an admin relationship:
//   - The PER-USER instance (built here, held in Server.sessionAuthz and reached
//     by every /api/v1/sessions mutation handler via (*Server).sessionAccess)
//     uses alwaysFalseAdminChecker. On a per-user route an admin is therefore
//     treated as a team-member for read and a non-owner for mutation — an admin
//     can NEVER owner-mutate a session it does not own through a per-user route.
//     This is structural (a distinct always-false checker), not a per-call flag.
//   - The ADMIN instance (real IsAdmin via rbacAdminChecker, plus the
//     /api/v1/admin/sessions route prefix) is explicitly DEFERRED to B006. No
//     per-user route may use a real-admin checker; B006 owns the admin seam.
//
// The seam is built from adapters over core.Services:
//   - sessionModelResolver supplies the owning principal (ownership).
//   - alwaysFalseAdminChecker suppresses the admin relationship on the per-user
//     instance (B005). rbacAdminChecker — the D-0011 dynamic admin reuse — is
//     retained for B006's admin instance, NOT wired into the per-user instance.

// sessionModelResolver adapts core.Services.SessionModel to
// sessionauthz.SessionResolver. It reads the service at call time so a nil
// session store (local/dev without a DB) resolves cleanly to "not found"; the
// handlers already short-circuit on a nil SessionModel before authorizing.
type sessionModelResolver struct{ services *core.Services }

func (r sessionModelResolver) SessionCreator(ctx context.Context, sessionID string) (string, bool, error) {
	if r.services == nil || r.services.SessionModel == nil {
		return "", false, nil
	}
	sess, err := r.services.SessionModel.GetSession(ctx, sessionID)
	if err != nil {
		return "", false, err
	}
	if sess == nil {
		return "", false, nil
	}
	return sess.CreatorPrincipal, true, nil
}

// rbacAdminChecker adapts core.Services to sessionauthz.AdminChecker, reusing
// the D-0011 dynamic admin capability (services.RBAC.IsAdmin) — the same call
// the policy engine's admin short-circuit and the requireAdmin gate use.
//
// Auth-disabled posture: when RBAC enforcement is off (the predicate
// services.RBACEnabled, mirroring requireAdmin's guard) there is no dynamic
// admin, so this returns (false, nil). That deliberately DIFFERS from
// requireAdmin, which PERMITS admin when RBAC is disabled.
//
// As of B005 this checker is NOT wired into the per-user seam instance (which
// uses alwaysFalseAdminChecker — see below). It is retained for B006, which
// constructs the admin seam instance over the /api/v1/admin/sessions prefix and
// is the ONLY place a real-admin relationship may be resolved.
type rbacAdminChecker struct{ services *core.Services }

func (c rbacAdminChecker) IsAdmin(ctx context.Context, principal string) (bool, error) {
	if c.services == nil || !c.services.RBACEnabled || c.services.RBAC == nil {
		return false, nil
	}
	return c.services.RBAC.IsAdmin(ctx, principal)
}

// alwaysFalseAdminChecker is the structural admin-suppressor for the per-user
// seam instance (§12.8 defense-in-depth, B005). It returns (false, nil)
// UNCONDITIONALLY — independent of RBAC state — so that on a per-user
// /api/v1/sessions route the seam can never resolve the admin relationship: an
// admin is a team-member for read and a non-owner for mutation. Admin
// owner-mutation of a non-owned session is impossible through a per-user route
// by construction, not by a per-call flag. B006's admin instance uses
// rbacAdminChecker; this one never does.
type alwaysFalseAdminChecker struct{}

func (alwaysFalseAdminChecker) IsAdmin(context.Context, string) (bool, error) {
	return false, nil
}

// newPerUserSessionAuthz builds the PER-USER seam instance: real ownership
// resolution, but admin suppressed by construction (alwaysFalseAdminChecker).
// This is the instance every /api/v1/sessions route authorizes through.
func newPerUserSessionAuthz(services *core.Services) *sessionauthz.Seam {
	return sessionauthz.New(
		sessionModelResolver{services: services},
		alwaysFalseAdminChecker{},
	)
}

// newAdminSessionAuthz builds the ADMIN seam instance (B006): the SAME ownership
// resolver, but the REAL D-0011 admin checker (rbacAdminChecker). It is the ONLY
// place a real admin relationship may be resolved over a session. The
// /api/v1/admin/sessions governance routes authorize through this instance (via
// (*Server).sessionAccessAdmin) AFTER the requireAdmin prefix gate, so
// cross-tenant governance requires BOTH (§12.8 defense-in-depth).
//
// DELIBERATE THREE-WAY ASYMMETRY — do NOT "fix" this into false consistency:
//
//   - The PER-USER instance (newPerUserSessionAuthz) uses alwaysFalseAdminChecker:
//     an admin can NEVER resolve the admin relationship on a per-user route, so
//     an admin cannot owner-mutate a session it does not own through
//     /api/v1/sessions. Suppression is structural (a distinct checker), RBAC
//     state notwithstanding.
//   - The ADMIN instance (here) uses rbacAdminChecker: when RBAC is ENABLED it
//     resolves the genuine dynamic admin; when RBAC is DISABLED it resolves
//     (false, nil) — NO admin. So with RBAC off, the admin SEAM denies the
//     admin-govern actions.
//   - The requireAdmin ROUTE gate (admingate.go) does the OPPOSITE under RBAC-off:
//     it PERMITS (auth-disabled permit convention, keeping local/dev unblocked).
//
// The net effect under RBAC-off is the SAFE intersection: requireAdmin permits at
// the prefix, but the admin seam denies the govern action, so cross-tenant
// governance still cannot fire without a real admin — exactly the BOTH-conditions
// posture §12.8 wants. The two checkers are intentionally NOT reconciled: the gate
// keeps dev usable, the seam keeps governance honest.
func newAdminSessionAuthz(services *core.Services) *sessionauthz.Seam {
	return sessionauthz.New(
		sessionModelResolver{services: services},
		rbacAdminChecker{services: services},
	)
}

// sessionAccess is the HTTP-layer entry to the §12.7 seam. It normalizes the
// rbac.Unknown sentinel to "" (the seam's single unauthenticated marker) and
// delegates the one decision. This is the ONLY function the per-user
// owner-mutate handlers call to authorize a session action; the bypass guard
// asserts each such handler reaches the store only after calling it.
func (s *Server) sessionAccess(ctx context.Context, principal rbac.Principal, sessionID string, action sessionauthz.Action) (sessionauthz.Decision, error) {
	p := string(principal)
	if principal == rbac.Unknown {
		p = ""
	}
	return s.sessionAuthz.SessionAccess(ctx, p, sessionID, action)
}

// sessionAccessAdmin is the HTTP-layer entry to the ADMIN seam instance (B006).
// It is identical in shape to sessionAccess but delegates to s.sessionAuthzAdmin
// (rbacAdminChecker), the only seam instance that can resolve a real admin
// relationship. The /api/v1/admin/sessions govern handlers call this AFTER the
// requireAdmin gate so a govern action requires BOTH the admin prefix AND a
// resolved admin relationship (§12.8). It is never reachable from a per-user
// route — those hold only s.sessionAuthz.
func (s *Server) sessionAccessAdmin(ctx context.Context, principal rbac.Principal, sessionID string, action sessionauthz.Action) (sessionauthz.Decision, error) {
	p := string(principal)
	if principal == rbac.Unknown {
		p = ""
	}
	return s.sessionAuthzAdmin.SessionAccess(ctx, p, sessionID, action)
}
