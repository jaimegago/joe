package rbac

import (
	"context"
	"net/http"
)

type principalKey struct{}

// PrincipalFromContext retrieves the principal stored in the context.
// Returns Unknown if not set. After Phase E (D-0008) every reader uses this:
// the guarded accessor's permit() call sites, regime declare/resolve, and the
// in-process tool client. The principal is established once by the edge
// middleware (auth.EdgeAuth → WithPrincipal) and carried by context.
func PrincipalFromContext(ctx context.Context) Principal {
	if p, ok := ctx.Value(principalKey{}).(Principal); ok {
		return p
	}
	return Unknown
}

// WithPrincipal returns a new context carrying the given principal,
// overriding whatever IdentityMiddleware put there. Phase 1 Change 10
// uses this for the §B1 principal substitution: in incident regime the
// captain-session gate, on Allow, replaces the request-time principal
// with the current captain's principal so downstream IsAllowed calls
// (or any other PrincipalFromContext reader) see the captain's
// authority, not the original caller's. In normal regime no
// substitution happens; the request-time principal is used unchanged.
//
// Exported so coreagent.DurableExecutor (outside the rbac package) can
// perform the substitution without growing a private helper in rbac.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return contextWithPrincipal(ctx, p)
}

// contextWithPrincipal returns a new context with the given principal attached.
func contextWithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// IdentityMiddleware injects the caller's Principal into the request context.
// Retained for test harnesses that build their own auth chains. The production
// chain in cmd/joe/server.go uses auth.EdgeAuth (Phase C/D), which resolves
// the principal from a session cookie or service-account key and sets it via
// WithPrincipal — exactly the same context value this middleware writes.
func IdentityMiddleware(provider IdentityProvider) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal := provider.Identity(r)
			r = r.WithContext(contextWithPrincipal(r.Context(), principal))
			next.ServeHTTP(w, r)
		})
	}
}

// EnforcementMiddleware was removed by rbac-engine-split. It had been a
// pass-through since the Phase E demotion (D-0008) — a coarse outer gate that
// discarded its engine argument and returned the next handler unchanged — while
// the guarded accessor (internal/access) became the sole authoritative RBAC gate
// on both the HTTP and agent-loop paths. Because its only production consumer
// (the transport middleware chain) fed it the governance-wired engine and then
// threw the decision away, keeping it around masked the accessor engine being
// built bare and un-governed inside api.New. Deleting it, and injecting the ONE
// composition-root engine into api.New instead, is what makes the transport and
// accessor engines provably the same object.
