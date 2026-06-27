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

// EnforcementMiddleware is a coarse outer gate that survives in the chain as a
// defence-in-depth seam (docs/reference/joe-identity-design.md §3, Phase E demotion). Its
// former per-zone IsAllowed decision has been moved into the guarded accessor
// (internal/access), which is now the AUTHORITATIVE RBAC gate on BOTH the HTTP
// path (Phase A) and the in-process agent-loop path (Phase E). The accessor
// evaluates the real caller principal carried by Go context — there is no
// loopback HTTP self-call any more, no svc:server re-authentication on the loop,
// and no second IsAllowed call to keep in sync.
//
// This middleware is now a pass-through. It is kept (a) so existing test
// harnesses that wire it in continue to compile, and (b) as a documented seam
// for a future coarse "authenticated principal required on component-keyed paths"
// belt-and-suspenders — EdgeAuth already rejects unauthenticated protected
// paths, so requiring a principal here would only be redundant defence. The
// engine argument is retained so the call sites are unchanged.
//
// The Phase E equivalence test
// (internal/api/access_phasee_test.go::TestPhaseE_AccessorAloneMatchesPriorOutcomes)
// gates this demotion: it proves that the accessor alone produces the same
// allow/deny/unauth (200/403/401) outcomes the prior middleware+accessor chain
// produced.
func EnforcementMiddleware(engine *PolicyEngine) func(http.Handler) http.Handler {
	_ = engine // intentionally unused after Phase E demotion
	return func(next http.Handler) http.Handler {
		return next
	}
}
