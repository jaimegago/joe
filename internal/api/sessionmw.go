package api

import (
	"net/http"

	"github.com/jaimegago/joe/internal/agentctx"
)

// SessionMiddleware reads the session/run/idempotency-key headers from
// the request and threads them into request context. Phase 1 Change 9
// — see the Phase 1 decomposition plan.
//
// Headers (defined in internal/agentctx):
//
//	X-Joe-Session-ID         agent_sessions.id of the current session.
//	X-Joe-Run-ID             agent_runs.id of the current run on that
//	                         session. Required by the executor wrapper
//	                         for T2/T3 tool calls — without a run there
//	                         is nowhere to anchor the idempotency key.
//	X-Joe-Idempotency-Key    Caller-supplied key. If omitted, the
//	                         executor wrapper computes one from
//	                         hash(runID, toolName, argsHash).
//
// Installed AFTER IdentityMiddleware (so the principal is already on
// the context for downstream handlers) and BEFORE the handlers themselves,
// inside which the guarded accessor performs the per-component RBAC check.
func SessionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if v := r.Header.Get(agentctx.HeaderSessionID); v != "" {
			ctx = agentctx.WithSessionID(ctx, v)
		}
		if v := r.Header.Get(agentctx.HeaderRunID); v != "" {
			ctx = agentctx.WithRunID(ctx, v)
		}
		if v := r.Header.Get(agentctx.HeaderIdempotencyKey); v != "" {
			ctx = agentctx.WithIdempotencyKey(ctx, v)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
