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
// The seam is built once in api.New from two adapters over core.Services:
//   - sessionModelResolver supplies the owning principal (ownership).
//   - rbacAdminChecker reuses the D-0011 dynamic admin capability (governance),
//     honoring the system-wide auth-disabled convention.

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
// requireAdmin, which PERMITS admin when RBAC is disabled: the per-user
// owner-mutate handlers wired to the seam must keep their creator-only semantics
// in local/dev runs (a disabled-RBAC caller must not be able to mutate a session
// it does not own). Admin governance routes (B006) gate with requireAdmin
// separately, so the disabled-permits-admin convention is preserved where it
// belongs.
type rbacAdminChecker struct{ services *core.Services }

func (c rbacAdminChecker) IsAdmin(ctx context.Context, principal string) (bool, error) {
	if c.services == nil || !c.services.RBACEnabled || c.services.RBAC == nil {
		return false, nil
	}
	return c.services.RBAC.IsAdmin(ctx, principal)
}

// newSessionAuthz builds the seam from the service adapters.
func newSessionAuthz(services *core.Services) *sessionauthz.Seam {
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
