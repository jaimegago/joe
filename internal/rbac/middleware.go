package rbac

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
)

type principalKey struct{}

// PrincipalFromContext retrieves the principal stored in the context.
// Returns Unknown if not set. Exported for handlers that operate outside
// the source-keyed EnforcementMiddleware path (e.g. regime declare/resolve,
// which need the principal but have no sourceID).
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
// It must run after BearerAuth so that invalid tokens are rejected before
// identity resolution. When auth is disabled (empty apiKey), all callers
// resolve to the configured principal.
func IdentityMiddleware(provider IdentityProvider) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal := provider.Identity(r)
			r = r.WithContext(contextWithPrincipal(r.Context(), principal))
			next.ServeHTTP(w, r)
		})
	}
}

// sourceIDFromRequest attempts to extract a source_id from the URL path.
// Joe's API uses the pattern /api/v1/{adapter}/{sourceID}/... for all
// infrastructure endpoints. Returns empty string if not found.
func sourceIDFromRequest(r *http.Request) string {
	// /api/v1/{adapter}/{sourceID}/...
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
	// parts[0]="api" parts[1]="v1" parts[2]=adapter parts[3]=sourceID
	if len(parts) >= 4 {
		return parts[3]
	}
	return ""
}

// actionFromRequest maps the HTTP method to an RBAC Action.
// GET/HEAD → ActionRead; POST/PUT/PATCH → ActionMutate; DELETE → ActionDelete.
func actionFromRequest(r *http.Request) Action {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		return ActionRead
	case http.MethodDelete:
		return ActionDelete
	default:
		return ActionMutate
	}
}

// EnforcementMiddleware checks the caller's policy before proxying requests to
// infrastructure adapters. It only evaluates paths that carry a sourceID (i.e.
// /api/v1/{adapter}/{sourceID}/...). Admin paths (/api/v1/admin/) and non-source
// paths (graph, knowledge, status) are exempt.
//
// Use WithPolicyEngine(nil) to disable enforcement (RBAC off).
func EnforcementMiddleware(engine *PolicyEngine) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if engine == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sourceID := sourceIDFromRequest(r)
			if sourceID == "" {
				// No source in path — not subject to RBAC enforcement.
				next.ServeHTTP(w, r)
				return
			}

			principal := PrincipalFromContext(r.Context())
			action := actionFromRequest(r)

			if !engine.IsAllowed(r.Context(), principal, sourceID, action) {
				slog.Warn("rbac: access denied",
					"principal", principal,
					"source_id", sourceID,
					"action", action,
					"path", r.URL.Path,
				)
				http.Error(w, `{"error":"forbidden","message":"access denied by RBAC policy"}`,
					http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
