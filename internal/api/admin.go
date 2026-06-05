package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/rbac"
)

// adminHandler exposes RBAC management endpoints.
//
// Every route under /api/v1/admin/ mutates or exposes authorization state
// (zones, policies, source-zone assignments, the unassigned-source roster),
// so EVERY handler below does two things:
//
//  1. Admin-gates via server.requireAdmin — the same gate Stream G applied
//     to the LLM settings/usage endpoints. The gate was applied to LLM
//     settings but not retroactively to this RBAC admin surface; the
//     resulting privilege escalation (any authenticated principal could
//     grant itself a policy or a zone with arbitrary allowed-actions) is
//     documented in ADMIN_SURFACE_AUDIT.md (Launch Blocker 1) and
//     DECISIONS.md (D-0012). TestAdminRoutes_AllRequireAdminGate
//     (admin_gate_guard_test.go) fails the build if a future admin route is
//     registered without the gate.
//
//  2. Writes one KindAdminAccess audit row (D-0013). D-0012 closed the GATE
//     gap but the surface still wrote ZERO audit rows — the most
//     authorization-critical mutations in the system went unrecorded.
//     Phase F's audit covered the guarded accessor's DECISION point, not
//     mutations of the authorization CONFIGURATION the accessor reads. Each
//     handler now records its event through recordAdminAudit with Phase F's
//     §4 failure posture: mutating actions fail CLOSED (no row ⇒ no
//     mutation), the .read actions fail OPEN (the read proceeds, the failure
//     is logged loudly). TestAdminRoutes_AllAuditOnAllow
//     (admin_audit_guard_test.go) fails the build if a future admin route is
//     registered without an audit write in its allow path.
type adminHandler struct {
	repo   rbac.Repository
	server *Server
}

func (s *Server) registerAdminRoutes(mux *http.ServeMux, prefix string) {
	if s.services == nil || s.services.RBAC == nil {
		return // RBAC not configured — skip
	}
	h := &adminHandler{repo: s.services.RBAC, server: s}
	admin := prefix + "/admin"

	mux.HandleFunc(fmt.Sprintf("GET %s/zones", admin), h.listZones)
	mux.HandleFunc(fmt.Sprintf("POST %s/zones", admin), h.createZone)

	mux.HandleFunc(fmt.Sprintf("GET %s/source-zones", admin), h.listAssignments)
	mux.HandleFunc(fmt.Sprintf("POST %s/source-zones", admin), h.assignSourceZone)

	mux.HandleFunc(fmt.Sprintf("GET %s/policies", admin), h.listPolicies)
	mux.HandleFunc(fmt.Sprintf("POST %s/policies", admin), h.createPolicy)
	mux.HandleFunc(fmt.Sprintf("DELETE %s/policies/{id}", admin), h.deletePolicy)

	mux.HandleFunc(fmt.Sprintf("GET %s/unassigned", admin), h.listUnassigned)
}

// recordAdminAudit writes one KindAdminAccess audit row for an admin-surface
// event and applies Phase F's §4 failure posture (audit.FailurePosture) for
// the action.
//
// Returns a non-nil error ONLY for a mutating (fail-closed) action whose
// audit write failed: the caller MUST abort the mutation (no durable row ⇒
// no mutation). The .read actions are read-class and always return nil — the
// read proceeds and the failure is logged loudly (fail-open per §4). When
// the audit repository is not wired (nil — unit-test harnesses that don't
// exercise the trail) this is a no-op returning nil, the same carve-out
// captaingate.Wrapper uses for a nil audit repo.
//
// Mutations call this BEFORE performing the repository write, so a failed
// audit write aborts before any state change. This is the same audit-before-
// act ordering the Phase F accessor uses (it records the decision before the
// caller performs the infra call). It over-records rather than under-records:
// a mutation whose audit row landed but whose repository write then failed
// leaves a row for a mutation that did not commit. Closing that residual
// window would require a transaction shared between the rbac repository and
// the audit repository — D-0013 deliberately leaves that out of scope (it
// would refactor the rbac storage layer, which the fix's scope forbids).
// Over-recording is the safe direction for a forensic trail.
func (h *adminHandler) recordAdminAudit(ctx context.Context, action string, decision audit.Decision, reason string, d audit.Details, where string) error {
	if h.server.services == nil || h.server.services.Audit == nil {
		return nil
	}
	blob, err := json.Marshal(d)
	if err != nil {
		// Practically unreachable for the resource shapes admin rows carry,
		// but a marshal failure on a mutating action must abort it (the row
		// could not be formed). Route through FailurePosture so the §4 split
		// — and the loud log — applies uniformly.
		return audit.FailurePosture(ctx, action,
			fmt.Errorf("%w: marshal admin audit details: %v", audit.ErrAuditWriteFailed, err),
			where, audit.PostureForAction(action))
	}
	insErr := h.server.services.Audit.Insert(ctx, audit.Event{
		Principal: string(rbac.PrincipalFromContext(ctx)),
		Action:    action,
		Decision:  decision,
		Reason:    reason,
		Kind:      audit.KindAdminAccess,
		Context:   string(blob),
	})
	return audit.FailurePosture(ctx, action, insErr, where, audit.PostureForAction(action))
}

// actor returns the acting principal for the request — the authenticated
// caller. It is threaded into the repository's mutating methods so the audit
// row the repository writes (in the same transaction as the mutation) records
// who performed it. Same source as recordAdminAudit's principal field, kept in
// one helper so the two cannot drift.
func (h *adminHandler) actor(r *http.Request) string {
	return string(rbac.PrincipalFromContext(r.Context()))
}

// --- Zone endpoints ---

func (h *adminHandler) listZones(w http.ResponseWriter, r *http.Request) {
	if _, gated := h.server.requireAdmin(w, r); gated {
		return
	}
	zones, err := h.repo.ListZones(r.Context())
	if err != nil {
		writeInternalError(w, err, "list zones")
		return
	}
	// Read-class audit: GET /admin/zones leaks the authz topology (which zone
	// permits what), so the access is recorded (D-0012). Fail-open per §4 —
	// the read proceeds even if the row cannot be written.
	_ = h.recordAdminAudit(r.Context(), audit.ActionAdminZoneRead, audit.DecisionAllow,
		"admin_read", audit.Details{Target: "zones"}, "admin:zone.read")
	writeJSON(w, http.StatusOK, map[string]any{
		"zones": zones,
		"count": len(zones),
	})
}

func (h *adminHandler) createZone(w http.ResponseWriter, r *http.Request) {
	if _, gated := h.server.requireAdmin(w, r); gated {
		return
	}
	var z rbac.Zone
	if err := json.NewDecoder(r.Body).Decode(&z); err != nil {
		writeBadRequest(w, err, "create zone", "invalid request body")
		return
	}
	if z.ID == "" || z.Name == "" {
		writeBadRequest(w, nil, "create zone", "id and name are required")
		return
	}
	if len(z.AllowedActions) == 0 {
		z.AllowedActions = []rbac.Action{rbac.ActionRead}
	}

	// The repository writes the KindAdminAccess audit row in the same
	// transaction as the insert (atomic), so the handler no longer calls
	// recordAdminAudit for this mutation. The acting principal is threaded down.
	created, err := h.repo.CreateZone(r.Context(), z, h.actor(r))
	if err != nil {
		writeInternalError(w, err, "create zone")
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

// --- Source zone assignment endpoints ---

func (h *adminHandler) listAssignments(w http.ResponseWriter, r *http.Request) {
	if _, gated := h.server.requireAdmin(w, r); gated {
		return
	}
	assignments, err := h.repo.ListAssignments(r.Context())
	if err != nil {
		writeInternalError(w, err, "list source-zone assignments")
		return
	}
	// Read-class audit (fail-open): the source→zone map is authz topology.
	_ = h.recordAdminAudit(r.Context(), audit.ActionAdminSourceZoneRead, audit.DecisionAllow,
		"admin_read", audit.Details{Target: "source_zones"}, "admin:source_zone.read")
	writeJSON(w, http.StatusOK, map[string]any{
		"assignments": assignments,
		"count":       len(assignments),
	})
}

func (h *adminHandler) assignSourceZone(w http.ResponseWriter, r *http.Request) {
	if _, gated := h.server.requireAdmin(w, r); gated {
		return
	}
	var a rbac.SourceZoneAssignment
	if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
		writeBadRequest(w, err, "assign source zone", "invalid request body")
		return
	}
	if a.SourceID == "" || a.ZoneID == "" || a.AssignedBy == "" {
		writeBadRequest(w, nil, "assign source zone", "source_id, zone_id, and assigned_by are required")
		return
	}

	// The repository captures the prior assignment and writes the audit row in
	// the same transaction as the upsert; the handler threads the actor down.
	if err := h.repo.UpsertAssignment(r.Context(), a, h.actor(r)); err != nil {
		writeInternalError(w, err, "assign source zone")
		return
	}
	writeJSON(w, http.StatusOK, a)
}

// --- Policy endpoints ---

func (h *adminHandler) listPolicies(w http.ResponseWriter, r *http.Request) {
	if _, gated := h.server.requireAdmin(w, r); gated {
		return
	}
	policies, err := h.repo.ListPolicies(r.Context())
	if err != nil {
		writeInternalError(w, err, "list policies")
		return
	}
	// Read-class audit (fail-open): GET /admin/policies leaks who holds which
	// zone — the access-control map (D-0012).
	_ = h.recordAdminAudit(r.Context(), audit.ActionAdminPolicyRead, audit.DecisionAllow,
		"admin_read", audit.Details{Target: "policies"}, "admin:policy.read")
	writeJSON(w, http.StatusOK, map[string]any{
		"policies": policies,
		"count":    len(policies),
	})
}

func (h *adminHandler) createPolicy(w http.ResponseWriter, r *http.Request) {
	if _, gated := h.server.requireAdmin(w, r); gated {
		return
	}
	var p rbac.Policy
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeBadRequest(w, err, "create policy", "invalid request body")
		return
	}
	if p.Principal == "" || p.ZoneID == "" {
		writeBadRequest(w, nil, "create policy", "principal and zone_id are required")
		return
	}

	// The repository writes the policy.grant audit row atomically with the
	// insert; the handler threads the acting principal down.
	created, err := h.repo.CreatePolicy(r.Context(), p, h.actor(r))
	if err != nil {
		writeInternalError(w, err, "create policy")
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (h *adminHandler) deletePolicy(w http.ResponseWriter, r *http.Request) {
	if _, gated := h.server.requireAdmin(w, r); gated {
		return
	}
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeBadRequest(w, err, "delete policy", "invalid policy id")
		return
	}

	// The repository captures the revoked grant for the audit Before and writes
	// the policy.revoke row in the same transaction as the delete; the handler
	// threads the acting principal down.
	if err := h.repo.DeletePolicy(r.Context(), id, h.actor(r)); err != nil {
		writeInternalError(w, err, "delete policy")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// --- Unassigned sources endpoint ---

func (h *adminHandler) listUnassigned(w http.ResponseWriter, r *http.Request) {
	if _, gated := h.server.requireAdmin(w, r); gated {
		return
	}
	ids, err := h.repo.ListUnassignedSourceIDs(r.Context())
	if err != nil {
		writeInternalError(w, err, "list unassigned sources")
		return
	}
	// Read-class audit (fail-open): the unassigned roster is part of the
	// source→zone authz map; recorded under the same source_zone.read verb.
	_ = h.recordAdminAudit(r.Context(), audit.ActionAdminSourceZoneRead, audit.DecisionAllow,
		"admin_read", audit.Details{Target: "unassigned"}, "admin:source_zone.read")
	writeJSON(w, http.StatusOK, map[string]any{
		"source_ids": ids,
		"count":      len(ids),
	})
}
