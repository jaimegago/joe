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
// /api/v1/{adapter}/{sourceID}/...) and instead authorize via
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
	// without a database; production wiring in cmd/joe-core/main.go always
	// supplies a real repository.
	auditRepo audit.Repository
}

func (s *Server) registerRegimeRoutes(mux *http.ServeMux, prefix string) {
	if s.services == nil || s.services.SessionModel == nil {
		return
	}
	// The policy engine is constructed in cmd/joe-core/main.go from the
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

	// Optional body: { "declared_kind": "human" | "joe" }. Default human.
	var req struct {
		DeclaredKind string `json:"declared_kind,omitempty"`
	}
	if r.Body != nil && r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeBadRequest(w, err, "declare regime", "invalid request body")
			return
		}
	}
	declaredKind := sessionmodel.RegimeKindHuman
	if req.DeclaredKind != "" {
		switch req.DeclaredKind {
		case "human":
			declaredKind = sessionmodel.RegimeKindHuman
		case "joe":
			// Joe-autonomous declare is a Change 12 inert seam, gated on
			// the compile-time constant seams.JoeAutonomousDeclareEnabled.
			// Phase 1: the constant is false — refuse with 403 BEFORE any
			// call to sessionmodel.Repository.DeclareIncidentRegime. The
			// placeholder is structurally tied to the seam so future
			// enablement is a one-line constant change, not a wiring
			// exercise (verified by the paired build-tag-isolated test).
			if !seams.JoeAutonomousDeclareEnabled {
				writeError(w, http.StatusForbidden, "forbidden",
					"joe-autonomous declare is not enabled in Phase 1 (incremental-autonomy seam)")
				return
			}
			declaredKind = sessionmodel.RegimeKindJoe
		default:
			writeBadRequest(w, nil, "declare regime",
				"declared_kind must be 'human' or 'joe'")
			return
		}
	}

	// §6-B authorization check. Write a deny audit row before returning
	// so a denied declare is also captured in the durable trail.
	// Phase G: HasZoneAccess is set-shaped; we build the set from the
	// caller's context principal (size 1, consistent with the rest of
	// the system per D-0005). Group/multi-member sets remain a v2
	// extension behind this same call site.
	if !h.policy.HasZoneAccess(r.Context(), rbac.NewPrincipalSet(principal), "regime-control", rbac.ActionDeclareIncident) {
		if err := h.writeRegimeAudit(r.Context(), principal, audit.ActionDeclareIncident,
			audit.DecisionDeny, "no_grant",
			map[string]string{"declared_kind": string(declaredKind)}); err != nil {
			// Fail-closed on the audit write itself: refuse the deny
			// path rather than silently swallow an audit-store failure.
			writeInternalError(w, err, "declare regime audit (deny)")
			return
		}
		writeError(w, http.StatusForbidden, "forbidden",
			"principal lacks can_declare_incident (regime-control zone)")
		return
	}

	// Phase F bug #3 fix: write the durable audit row BEFORE the mutable
	// system_regime / session_captains rows are touched, so the "who
	// declared this incident and when" record survives a later resolve
	// (which nulls system_regime.declared_by_principal and cascade-deletes
	// session_captains).
	if err := h.writeRegimeAudit(r.Context(), principal, audit.ActionDeclareIncident,
		audit.DecisionAllow, "transition_recorded",
		map[string]string{"declared_kind": string(declaredKind)}); err != nil {
		// Transition is mutating → fail-closed. The mutable rows are
		// untouched.
		writeInternalError(w, err, "declare regime audit (allow)")
		return
	}

	sessionID, captainID, err := h.repo.DeclareIncidentRegime(r.Context(), string(principal), declaredKind)
	if err != nil {
		if errors.Is(err, sessionmodel.ErrRegimeAlreadyIncident) {
			writeError(w, http.StatusConflict, "conflict",
				"regime is already incident — resolve the current incident before declaring another")
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
	return audit.FailurePosture(ctx, action, err, "regime:"+action)
}
