package auth

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/rbac"
)

// auditSourceBreakGlass names the credential mechanism recorded in the
// Source column of a break-glass service-account audit row.
const auditSourceBreakGlass = "break-glass"

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
	// Audit records break-glass service-account credential use (Stream
	// H3), windowed-deduplicated so it writes once per episode rather than
	// on every request. nil-safe: a nil repository disables the write and
	// leaves the request path unchanged.
	Audit audit.Repository
	// AuditDedupWindow is the suppression window for the break-glass audit
	// dedup — set to the session TTL so a service account's credential use
	// is recorded at most once per window. A non-positive value falls back
	// to defaultSessionTTL, matching SessionManager so the two stay equal.
	AuditDedupWindow time.Duration
}

// loginDedup suppresses repeated break-glass audit writes for the same
// (principal, remote-addr) key within a time window. EdgeAuth runs
// per-request, so the common case is "already recorded this window" — the
// fast path takes only a read lock and writes nothing. The state is
// in-memory only; losing it on restart at worst causes one redundant row,
// which is fail-safe.
type loginDedup struct {
	mu     sync.RWMutex
	window time.Duration
	last   map[string]time.Time
	now    func() time.Time
}

func newLoginDedup(window time.Duration) *loginDedup {
	if window <= 0 {
		window = defaultSessionTTL
	}
	return &loginDedup{
		window: window,
		last:   make(map[string]time.Time),
		now:    time.Now,
	}
}

// shouldRecord reports whether a break-glass audit row should be written for
// key now, and atomically records the timestamp when it returns true. The
// read-mostly fast path (already recorded within the window) takes only a
// read lock and never writes. The write path re-checks under the write lock
// so that among several racing first-uses of the same key, EXACTLY ONE
// returns true and records — the others observe the just-written timestamp
// and suppress.
func (d *loginDedup) shouldRecord(key string) bool {
	now := d.now()
	d.mu.RLock()
	last, ok := d.last[key]
	d.mu.RUnlock()
	if ok && now.Sub(last) < d.window {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	// Re-check under the exclusive lock: a concurrent first-use may have
	// recorded between the RUnlock and the Lock (compare-and-set).
	if last, ok := d.last[key]; ok && now.Sub(last) < d.window {
		return false
	}
	d.last[key] = now
	return true
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

	// One dedup table for the lifetime of this middleware so break-glass
	// audit rows are windowed across requests, not per-request.
	dedup := newLoginDedup(cfg.AuditDedupWindow)

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
						recordBreakGlassAudit(r.Context(), cfg.Audit, dedup, p, r)
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

// recordBreakGlassAudit writes one auth_login audit row for break-glass
// service-account credential use, deduplicated to once per episode by
// dedup. It is fail-open-but-loud: a write error never blocks the request
// — FailurePosture logs loudly and its return is discarded. nil-safe — a
// nil audit repository skips the write entirely (and the dedup work).
func recordBreakGlassAudit(ctx context.Context, repo audit.Repository, dedup *loginDedup, p rbac.Principal, r *http.Request) {
	if repo == nil {
		return
	}
	remote := r.RemoteAddr
	// Key on (principal, remote-addr) so a row is recorded once per episode
	// per source. NUL separates the two fields unambiguously.
	if !dedup.shouldRecord(string(p) + "\x00" + remote) {
		return
	}
	blob, _ := json.Marshal(map[string]string{
		"remote":     remote,
		"user_agent": r.UserAgent(),
	})
	err := repo.Insert(ctx, audit.Event{
		Principal:   string(p),
		Action:      audit.ActionBreakGlassUse,
		ComponentID: auditSourceBreakGlass,
		Decision:    audit.DecisionAllow,
		Reason:      "break_glass_credential_used",
		Kind:        audit.KindAuthLogin,
		Context:     string(blob),
	})
	// Fail-open-but-loud: pass audit.FailOpen so the loud log names the real
	// outcome (the request PROCEEDED) rather than claiming a fail-closed
	// abort, and discard the return so the request is never blocked.
	_ = audit.FailurePosture(ctx, audit.ActionBreakGlassUse, err, "auth:break_glass_use", audit.FailOpen)
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
