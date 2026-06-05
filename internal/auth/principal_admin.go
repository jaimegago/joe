package auth

import (
	"context"
	"fmt"

	"github.com/jaimegago/joe/internal/rbac"
)

// PrincipalAdmin performs identity-registry lifecycle operations that span BOTH
// the principals registry (rbac) and the auth_sessions store: disabling a
// principal and revoking its live sessions, or re-enabling it. It lives in the
// auth package because this is the one layer that legitimately knows about both
// stores — the rbac repository must not import the session store, and the
// session store must not import rbac, so the cross-store orchestration belongs
// here. Stage 3 exposes Disable/Enable over HTTP; this type holds the
// orchestration so that handler stays a thin adapter.
type PrincipalAdmin struct {
	principals rbac.PrincipalRepository
	sessions   Repository
}

// NewPrincipalAdmin builds a PrincipalAdmin over the identity registry and the
// session store (the same SQLite database in production).
func NewPrincipalAdmin(principals rbac.PrincipalRepository, sessions Repository) *PrincipalAdmin {
	return &PrincipalAdmin{principals: principals, sessions: sessions}
}

// Disable sets principal's registry status to disabled and then revokes every
// live session it holds, giving instant revocation without any per-request
// status check. It returns the number of registry rows changed (0 if the
// principal is not in the registry, so the caller can tell a real disable from
// a no-op) and the number of live sessions revoked (the instant-revocation
// count the HTTP handler surfaces to the operator).
//
// Sequencing — status-change FIRST, session-deletion SECOND, NOT one atomic
// cross-store transaction. SetPrincipalStatus owns its own transaction (it
// writes the status change and its audit row atomically through the rbac
// repository's mutate path), so it cannot be enlisted into an outer
// transaction without leaking that internal seam; a clean single transaction
// across both stores is therefore impractical here. The ordering is
// deliberate, not incidental: the authoritative disabled state is persisted and
// audited before the session cleanup, so if the process dies between the two
// steps the principal is durably disabled (the security-relevant fact) and only
// the cleanup is lost. The residual window is bounded and self-healing — the
// stale sessions expire on their own TTL, no NEW session can be minted because
// the login callback re-checks status, and re-running Disable purges them. The
// reverse ordering could delete sessions yet leave the principal active on a
// crash, which is strictly worse.
func (a *PrincipalAdmin) Disable(ctx context.Context, principal, actor string) (changed, sessionsRevoked int64, err error) {
	changed, err = a.principals.SetPrincipalStatus(ctx, principal, rbac.PrincipalStatusDisabled, actor)
	if err != nil {
		return 0, 0, fmt.Errorf("auth: disable principal %q: %w", principal, err)
	}
	sessionsRevoked, err = a.sessions.DeleteSessionsForPrincipal(ctx, principal)
	if err != nil {
		return changed, 0, fmt.Errorf("auth: revoke sessions for disabled principal %q: %w", principal, err)
	}
	return changed, sessionsRevoked, nil
}

// Enable restores principal's registry status to active. It does NOT restore
// any sessions — a re-enabled user logs in fresh — so there is no session-store
// interaction on this path. It returns the number of registry rows changed (0
// if the principal is not in the registry).
func (a *PrincipalAdmin) Enable(ctx context.Context, principal, actor string) (int64, error) {
	changed, err := a.principals.SetPrincipalStatus(ctx, principal, rbac.PrincipalStatusActive, actor)
	if err != nil {
		return 0, fmt.Errorf("auth: enable principal %q: %w", principal, err)
	}
	return changed, nil
}
