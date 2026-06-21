package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/seams"
	"github.com/jaimegago/joe/internal/sessionmodel"
)

// regimeHandler exposes the system regime read + human declare/resolve
// endpoints — Phase 1 Changes 4 (read) and 5 (declare/resolve).
//
// Declare and resolve are sourceless RBAC capabilities; they bypass the
// source-keyed EnforcementMiddleware (which only fires on paths with
// /api/v1/{adapter}/{componentID}/...) and instead authorize via
// PolicyEngine.HasZoneAccess against the seeded "regime-control" zone.
// See the §6-B finding in internal/store/migrations/012_regime_rbac.up.sql.
//
// Joe-autonomous declare/resolve are defined-but-inert seams added in
// Change 12; this file does not implement them.
type regimeHandler struct {
	repo   sessionmodel.Repository
	policy *rbac.PolicyEngine // nil when RBAC is disabled
	// auditRepo writes the durable record of who declared/resolved an
	// incident and when. Phase F redirects this history out of the mutable
	// system_regime row (which currently gets nulled on resolve — bug #3)
	// and into the append-only audit table. May be nil in dev/local runs
	// without a database; production wiring in cmd/joe/server.go always
	// supplies a real repository.
	auditRepo audit.Repository
}

func (s *Server) registerRegimeRoutes(mux *http.ServeMux, prefix string) {
	if s.services == nil || s.services.SessionModel == nil {
		return
	}
	// The policy engine is constructed in cmd/joe/server.go from the
	// same rbacRepo wired into services.RBAC. The Server doesn't currently
	// hold a reference to the engine, so we construct one on demand from
	// services.RBAC. When RBAC is unconfigured (nil), declare/resolve
	// return 503 (no authorization layer to gate the regime-control
	// capability).
	var engine *rbac.PolicyEngine
	if s.services.RBAC != nil {
		engine = rbac.NewPolicyEngine(s.services.RBAC)
	}
	h := &regimeHandler{repo: s.services.SessionModel, policy: engine, auditRepo: s.services.Audit}
	mux.HandleFunc(fmt.Sprintf("GET %s/regime", prefix), h.read)
	mux.HandleFunc(fmt.Sprintf("POST %s/regime/declare", prefix), h.declare)
	mux.HandleFunc(fmt.Sprintf("POST %s/regime/resolve", prefix), h.resolve)
	// Per-user promote-incident route (§12.8, B005): the chat-view / sessions-tab
	// promote-this-session affordance. It is the SAME promote-in-place transition
	// as /regime/declare (the global declare control), but takes the session to
	// promote from the path. Authorization is the regime-control zone — NOT the
	// session seam (§12.7 keeps the regime state machine out of the seam
	// vocabulary). Both entry paths resolve to one promote (§12.3, §12.10).
	//
	// DUAL-DECLARE DISPOSITION (B006). §12 specifies ONE promote-in-place
	// TRANSITION reached by TWO UI ENTRY POINTS (§12.3 "both UI entry paths
	// resolve to a promote"; §12.10 "both resolve to the promote-in-place
	// transition"); §12.8's session API contract names a SINGLE promote route
	// (`promote-incident`). It does NOT name `/regime/declare`, and it does NOT
	// mandate two distinct backend routes — so this is a single-backend-surface
	// design, not a two-route one. That single surface ALREADY EXISTS below: BOTH
	// handlers funnel through the one authorizeDeclare → promoteInPlace →
	// DeclareIncidentRegime call site, share the regime-control-zone authz, and
	// honour the B004 session_id-required contract; neither routes through the
	// session seam.
	//
	// CANONICAL vs ALIAS. `POST /sessions/{id}/promote-incident` is the CANONICAL
	// per-user promote surface (the §12.8-named route; both UI entry points target
	// it — the chat-view affordance directly, the global declare control after its
	// promote-or-start-new disambiguation, where start-new is
	// create-empty-then-promote). `POST /regime/declare` is retained as the
	// control-plane / CLI ALIAS of the IDENTICAL backend surface: it is the reused
	// regime control plane (§12.10 "reused, not rebuilt") and the transport the
	// `joe incident declare` CLI + internal/client use (body-carried session_id
	// instead of a path id). It is NOT removed — removing it would break the CLI
	// and rebuild the control plane §12 says to reuse. The consolidation is at the
	// SURFACE: one transition, one call site, two thin transports.
	mux.HandleFunc(fmt.Sprintf("POST %s/sessions/{id}/promote-incident", prefix), h.promoteSessionIncident)
}

func (h *regimeHandler) read(w http.ResponseWriter, r *http.Request) {
	reg, err := h.repo.GetRegime(r.Context())
	if err != nil {
		writeInternalError(w, err, "get regime")
		return
	}
	writeJSON(w, http.StatusOK, reg)
}

// declare is the §R2 human path: declare an incident regime. R-CAP1 — the
// declaring human becomes captain in the same atomic transaction.
//
// The Joe-autonomous declare path is a Change 12 inert seam and is NOT
// implemented here. A declare request that explicitly asks for
// declared_kind="joe" is refused with 403 in Phase 1.
func (h *regimeHandler) declare(w http.ResponseWriter, r *http.Request) {
	if h.policy == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable,
			"RBAC not configured — regime declare/resolve unavailable")
		return
	}
	principal := rbac.PrincipalFromContext(r.Context())
	if principal == rbac.Unknown {
		writeError(w, http.StatusUnauthorized, "unauthorized", "principal not resolved")
		return
	}

	// Body: { "session_id": "...", "declared_kind": "human" | "joe" }.
	// session_id designates the existing 'default' session to PROMOTE IN
	// PLACE (§12.3) — declaration no longer mints a fresh incident row.
	// declared_kind defaults to human. (The create-empty-then-promote
	// fallback for a caller with no session in hand is per-user-API / UI
	// orchestration, B005/B008 — it creates a default session and then
	// calls this endpoint with its id.)
	var req struct {
		SessionID    string `json:"session_id,omitempty"`
		DeclaredKind string `json:"declared_kind,omitempty"`
	}
	if r.Body != nil && r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeBadRequest(w, err, "declare regime", "invalid request body")
			return
		}
	}
	declaredKind, ok := parseDeclaredKind(w, req.DeclaredKind)
	if !ok {
		return
	}

	// §6-B authorization check. Write a deny audit row before returning
	// so a denied declare is also captured in the durable trail.
	// Phase G: HasZoneAccess is set-shaped; we build the set from the
	// caller's context principal (size 1, consistent with the rest of
	// the system per D-0005). Group/multi-member sets remain a v2
	// extension behind this same call site.
	if !h.authorizeDeclare(w, r, principal, declaredKind) {
		return
	}

	// session_id is required: declaration is promote-in-place (§12.3), so a
	// declare always designates an existing 'default' session to promote.
	// Checked AFTER authorization so a denied caller gets 403 (not 400) and
	// the session identifier is never probed by an unauthorized principal.
	if req.SessionID == "" {
		writeBadRequest(w, nil, "declare regime",
			"session_id is required — incident declaration promotes an existing session in place")
		return
	}

	h.promoteInPlace(w, r, principal, req.SessionID, declaredKind)
}

// promoteSessionIncident is the per-user promote-in-place route
// (POST /api/v1/sessions/{id}/promote-incident, §12.8 / B005): it promotes the
// path-named session into the incident master. It is the SAME §12.3 transition
// as (*regimeHandler).declare — declaration and per-user promote both resolve to
// one promote (§12.10) — and carries the SAME regime-control-zone authorization
// (NOT the session seam, §12.7). The session to promote comes from the path
// rather than the body; everything else is shared.
func (h *regimeHandler) promoteSessionIncident(w http.ResponseWriter, r *http.Request) {
	if h.policy == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable,
			"RBAC not configured — incident promotion unavailable")
		return
	}
	principal := rbac.PrincipalFromContext(r.Context())
	if principal == rbac.Unknown {
		writeError(w, http.StatusUnauthorized, "unauthorized", "principal not resolved")
		return
	}
	sessionID := r.PathValue("id")
	if sessionID == "" {
		writeBadRequest(w, nil, "promote incident", "missing session id")
		return
	}

	// Optional body: { "declared_kind": "human" | "joe" }. Defaults to human;
	// joe is the Change 12 inert seam (403). Mirrors declare exactly.
	var req struct {
		DeclaredKind string `json:"declared_kind,omitempty"`
	}
	if r.Body != nil && r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeBadRequest(w, err, "promote incident", "invalid request body")
			return
		}
	}
	declaredKind, ok := parseDeclaredKind(w, req.DeclaredKind)
	if !ok {
		return
	}
	if !h.authorizeDeclare(w, r, principal, declaredKind) {
		return
	}
	h.promoteInPlace(w, r, principal, sessionID, declaredKind)
}

// parseDeclaredKind validates the declared_kind field shared by declare and the
// per-user promote route. Empty defaults to human; "joe" is the Change 12
// autonomous-declare inert seam, refused with 403 BEFORE any repository call so
// the placeholder stays structurally tied to seams.JoeAutonomousDeclareEnabled.
// Returns ok=false (and writes the error) on an invalid kind or the disabled
// joe seam.
func parseDeclaredKind(w http.ResponseWriter, raw string) (sessionmodel.RegimeKind, bool) {
	switch raw {
	case "", "human":
		return sessionmodel.RegimeKindHuman, true
	case "joe":
		if !seams.JoeAutonomousDeclareEnabled {
			writeError(w, http.StatusForbidden, "forbidden",
				"joe-autonomous declare is not enabled in Phase 1 (incremental-autonomy seam)")
			return "", false
		}
		return sessionmodel.RegimeKindJoe, true
	default:
		writeBadRequest(w, nil, "declare regime",
			"declared_kind must be 'human' or 'joe'")
		return "", false
	}
}

// authorizeDeclare runs the §6-B regime-control-zone check shared by declare and
// the per-user promote route. On deny it writes the durable deny audit row and a
// 403 and returns false; on an audit-store failure it fails closed (500, false).
func (h *regimeHandler) authorizeDeclare(w http.ResponseWriter, r *http.Request, principal rbac.Principal, declaredKind sessionmodel.RegimeKind) bool {
	if h.policy.HasZoneAccess(r.Context(), rbac.NewPrincipalSet(principal), "regime-control", rbac.ActionDeclareIncident) {
		return true
	}
	if err := h.writeRegimeAudit(r.Context(), principal, audit.ActionDeclareIncident,
		audit.DecisionDeny, "no_grant",
		map[string]string{"declared_kind": string(declaredKind)}); err != nil {
		// Fail-closed on the audit write itself: refuse the deny path rather than
		// silently swallow an audit-store failure.
		writeInternalError(w, err, "declare regime audit (deny)")
		return false
	}
	writeError(w, http.StatusForbidden, "forbidden",
		"principal lacks can_declare_incident (regime-control zone)")
	return false
}

// promoteInPlace performs the authorized §12.3 promote-in-place transition for
// sessionID and writes the HTTP response on every outcome. It is the shared tail
// of declare and the per-user promote route: write the durable allow-audit row
// BEFORE the mutable rows are touched (Phase F bug #3), then run the one-
// transaction promote and map its errors. Callers must have already resolved the
// principal, validated declared_kind, and passed authorizeDeclare.
func (h *regimeHandler) promoteInPlace(w http.ResponseWriter, r *http.Request, principal rbac.Principal, sessionID string, declaredKind sessionmodel.RegimeKind) {
	if err := h.writeRegimeAudit(r.Context(), principal, audit.ActionDeclareIncident,
		audit.DecisionAllow, "transition_recorded",
		map[string]string{"declared_kind": string(declaredKind), "session_id": sessionID}); err != nil {
		// Transition is mutating → fail-closed. The mutable rows are untouched.
		writeInternalError(w, err, "declare regime audit (allow)")
		return
	}

	sessionID, captainID, err := h.repo.DeclareIncidentRegime(r.Context(), string(principal), sessionID, declaredKind)
	if err != nil {
		switch {
		case errors.Is(err, sessionmodel.ErrRegimeAlreadyIncident):
			writeError(w, http.StatusConflict, "conflict",
				"regime is already incident — resolve the current incident before declaring another")
			return
		case errors.Is(err, sessionmodel.ErrNotFound):
			writeError(w, http.StatusNotFound, errorCodeNotFound,
				"session to promote not found")
			return
		case errors.Is(err, sessionmodel.ErrSessionAlreadyIncident):
			writeError(w, http.StatusConflict, "conflict",
				"session is already an incident — cannot promote it again")
			return
		}
		writeInternalError(w, err, "declare regime")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"session_id":  sessionID,
		"captain_id":  captainID,
		"declared_by": string(principal),
	})
}

// resolve is the §R4 human path: transition the active incident session
// from believed_mitigated to resolved and clear the regime to normal.
// This is the SOLE production-code caller of
// sessionmodel.Repository.ResolveIncidentRegime; the AST invariant guard
// in regime_invariant_test.go enforces that.
//
// Joe-autonomous resolve is a Change 12 inert seam gated on
// seams.JoeAutonomousResolveEnabled. A request that signals as_joe=true
// is refused with 403 BEFORE any call to ResolveIncidentRegime — this
// preserves Change 5's single-call-site AST guard (Invariant 4 /
// no-auto-resolve-via-confirm_close).
func (h *regimeHandler) resolve(w http.ResponseWriter, r *http.Request) {
	if h.policy == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable,
			"RBAC not configured — regime declare/resolve unavailable")
		return
	}
	principal := rbac.PrincipalFromContext(r.Context())
	if principal == rbac.Unknown {
		writeError(w, http.StatusUnauthorized, "unauthorized", "principal not resolved")
		return
	}

	// Optional body: { "as_joe": true } selects the Joe-autonomous
	// resolve seam (Change 12). Absent or false → normal human path.
	var req struct {
		AsJoe bool `json:"as_joe,omitempty"`
	}
	if r.Body != nil && r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeBadRequest(w, err, "resolve regime", "invalid request body")
			return
		}
	}
	if req.AsJoe && !seams.JoeAutonomousResolveEnabled {
		// Joe-autonomous resolve is the inert seam. Refuse BEFORE the
		// ResolveIncidentRegime call so Change 5's AST guard
		// (regime_invariant_test.go) keeps its invariant: exactly one
		// production-code caller, the human-resolve fall-through below.
		writeError(w, http.StatusForbidden, "forbidden",
			"joe-autonomous resolve is not enabled in Phase 1 (incremental-autonomy seam)")
		return
	}

	if !h.policy.HasZoneAccess(r.Context(), rbac.NewPrincipalSet(principal), "regime-control", rbac.ActionResolveIncident) {
		if err := h.writeRegimeAudit(r.Context(), principal, audit.ActionResolveIncident,
			audit.DecisionDeny, "no_grant", nil); err != nil {
			writeInternalError(w, err, "resolve regime audit (deny)")
			return
		}
		writeError(w, http.StatusForbidden, "forbidden",
			"principal lacks can_resolve_incident (regime-control zone)")
		return
	}

	// Phase F: durable audit row written BEFORE the resolve mutation so
	// the resolved-by/declared-by history is independent of the
	// system_regime UPDATE that nulls declared_by_principal. After this
	// row, even a destructive write to system_regime cannot erase the
	// trail.
	if err := h.writeRegimeAudit(r.Context(), principal, audit.ActionResolveIncident,
		audit.DecisionAllow, "transition_recorded", nil); err != nil {
		writeInternalError(w, err, "resolve regime audit (allow)")
		return
	}

	sessionID, err := h.repo.ResolveIncidentRegime(r.Context(), string(principal))
	if err != nil {
		switch {
		case errors.Is(err, sessionmodel.ErrRegimeNotIncident):
			writeError(w, http.StatusConflict, "conflict", "regime is not incident")
			return
		case errors.Is(err, sessionmodel.ErrNoActiveIncident):
			writeError(w, http.StatusConflict, "conflict",
				"no active incident session — regime state is inconsistent")
			return
		case errors.Is(err, sessionmodel.ErrIncidentNotMitigated):
			writeError(w, http.StatusConflict, "conflict",
				"active incident must reach 'believed_mitigated' before resolve")
			return
		}
		writeInternalError(w, err, "resolve regime")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"session_id":  sessionID,
		"resolved_by": string(principal),
	})
}

// writeRegimeAudit records one regime-transition row in the append-only
// audit log. Always succeeds when auditRepo is nil (dev/local). On a
// repository error it returns the wrapped audit error after applying the
// fail-closed posture (regime transitions are mutating; reads stay
// fail-open elsewhere). The caller must abort the regime mutation on a
// non-nil return so the durable trail and the mutable state cannot
// disagree.
func (h *regimeHandler) writeRegimeAudit(ctx context.Context, principal rbac.Principal, action string, decision audit.Decision, reason string, extra map[string]string) error {
	if h.auditRepo == nil {
		return nil
	}
	ctxJSON := "{}"
	if len(extra) > 0 {
		b, err := json.Marshal(extra)
		if err == nil {
			ctxJSON = string(b)
		}
	}
	err := h.auditRepo.Insert(ctx, audit.Event{
		Principal: string(principal),
		Action:    action,
		Zone:      "regime-control",
		Decision:  decision,
		Reason:    reason,
		Kind:      audit.KindRegimeTransition,
		Context:   ctxJSON,
	})
	return audit.FailurePosture(ctx, action, err, "regime:"+action, audit.FailClosed)
}
