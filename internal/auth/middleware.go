package auth

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/jaimegago/joe/internal/rbac"
)

// defaultPublicPrefix is the one path prefix that must bypass the edge gate:
// the OIDC flow endpoints are, by definition, reachable before a session
// exists (you cannot require a session to log in).
const defaultPublicPrefix = "/api/v1/auth/"

// defaultDisabledPrincipal is the principal every caller resolves to in
// auth-disabled mode (no service accounts, no OIDC). The policy engine is nil
// in that mode, so this value is never authorization-significant; it preserves
// the pre-Phase-C local/dev display/audit identity.
const defaultDisabledPrincipal rbac.Principal = "default-operator"

// EdgeConfig configures the edge authentication middleware.
type EdgeConfig struct {
	// Sessions resolves a session cookie to a principal (humans, Phase C).
	// May be nil, in which case only service-account keys are honoured.
	Sessions *SessionManager
	// ServiceAccounts resolves a bearer key to its svc:<name> principal
	// (machines, Phase D). Nil/empty means no bearer mechanism. This is the
	// SINGLE machine-authentication input — it replaces the old single
	// APIKey→single-principal field; there is no parallel bearer path.
	ServiceAccounts *ServiceAccountResolver
	// OIDCConfigured reports whether an OIDC issuer is configured. With OIDC on
	// (or any service account set), authentication is enforced; with neither,
	// the edge is in auth-disabled mode and behaves exactly as before Phase D.
	OIDCConfigured bool
	// DisabledPrincipal overrides the auth-disabled fallback principal.
	// Defaults to defaultDisabledPrincipal.
	DisabledPrincipal rbac.Principal
	// PublicPrefixes bypass authentication. Defaults to {defaultPublicPrefix}.
	PublicPrefixes []string
}

// EdgeAuth returns the single edge-authentication middleware. It resolves the
// caller principal from a session cookie (humans) or a service-account bearer
// key (machines), places it in the request context via rbac.WithPrincipal — the
// SAME mechanism Phase B/C established — and rejects unauthenticated requests on
// protected paths with 401.
//
// Two authentication mechanisms, one authorization path: a human carries a
// session cookie and a machine carries a bearer key, never both meaningfully.
// Precedence is deterministic: a valid session cookie wins, then a
// service-account key is tried. The absence of one mechanism never breaks the
// other — Sessions may be nil (machine-only deployment) and ServiceAccounts may
// be nil/empty (human-only deployment) independently. Both converge on a single
// principal in context, which EnforcementMiddleware and the accessor evaluate
// identically regardless of which mechanism produced it.
//
// It replaces the BearerAuth + IdentityMiddleware pair in the production chain;
// the source-keyed rbac.EnforcementMiddleware remains the authoritative RBAC
// gate beneath it (demotion is Phase E).
func EdgeAuth(cfg EdgeConfig) func(http.Handler) http.Handler {
	disabledPrincipal := cfg.DisabledPrincipal
	if disabledPrincipal == "" {
		disabledPrincipal = defaultDisabledPrincipal
	}
	public := cfg.PublicPrefixes
	if public == nil {
		public = []string{defaultPublicPrefix}
	}
	enabled := cfg.ServiceAccounts.Configured() || cfg.OIDCConfigured

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// The login flow is reachable without authentication.
			if isPublicPath(r.URL.Path, public) {
				next.ServeHTTP(w, r)
				return
			}

			// Auth disabled (no service accounts, no OIDC): preserve the
			// pre-Phase-D local/dev posture — every caller is the fallback
			// principal and nothing is rejected. The policy engine is nil in
			// this mode, so the downstream gate permits all.
			if !enabled {
				next.ServeHTTP(w, r.WithContext(rbac.WithPrincipal(r.Context(), disabledPrincipal)))
				return
			}

			// 1) Human session cookie (precedence over the machine key).
			if cfg.Sessions != nil {
				if id := cookieValue(r, SessionCookieName); id != "" {
					if p, ok := cfg.Sessions.Resolve(r.Context(), id); ok {
						next.ServeHTTP(w, r.WithContext(rbac.WithPrincipal(r.Context(), p)))
						return
					}
				}
			}

			// 2) Service-account bearer key → svc:<name>. An unknown key is
			//    unauthenticated, exactly as an invalid bearer token is.
			if cfg.ServiceAccounts.Configured() {
				if key := bearerToken(r); key != "" {
					if p, ok := cfg.ServiceAccounts.Resolve(key); ok {
						next.ServeHTTP(w, r.WithContext(rbac.WithPrincipal(r.Context(), p)))
						return
					}
				}
			}

			// Unauthenticated on a protected path → reject, as today.
			slog.Warn("auth: unauthenticated request rejected", "path", r.URL.Path, "remote", r.RemoteAddr)
			writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		})
	}
}

// bearerToken extracts the token from an "Authorization: Bearer <token>"
// header, or "" if the header is absent or not a bearer credential.
func bearerToken(r *http.Request) string {
	const prefix = "Bearer "
	auth := r.Header.Get("Authorization")
	if len(auth) > len(prefix) && strings.EqualFold(auth[:len(prefix)], prefix) {
		return auth[len(prefix):]
	}
	return ""
}

func isPublicPath(path string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}
