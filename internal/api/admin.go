package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/credential"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/store"
)

// adminProvisioner is the admin-grant seam the admin-add handler wraps —
// auth.Provisioner.GrantAdmin. It is an interface (not the concrete
// *auth.Provisioner) so the handler depends on the capability rather than the
// type, keeping the api package's import surface narrow and the handler
// trivially fakeable in tests. GrantAdmin grants admin authority AND deletes
// the principal's now-redundant policy grants in one call, writing its audit
// row transactionally via the repository's AddAdmin — so wrapping it (rather
// than calling AddAdmin directly) avoids re-implementing the cleanup invariant.
type adminProvisioner interface {
	GrantAdmin(ctx context.Context, principal rbac.Principal) (wasNew bool, err error)
}

// adminPrincipalLifecycle is the disable/enable seam the principal-status
// handlers wrap — auth.PrincipalAdmin. Disable sets registry status to disabled
// (auditing in-transaction) and then revokes the principal's live sessions for
// instant revocation; Enable restores active status. Both return the number of
// registry rows changed.
type adminPrincipalLifecycle interface {
	Disable(ctx context.Context, principal, actor string) (changed, sessionsRevoked int64, err error)
	Enable(ctx context.Context, principal, actor string) (int64, error)
}

// adminHandler exposes RBAC management endpoints.
//
// Every route under /api/v1/admin/ mutates or exposes authorization state
// (zones, policies, component-zone assignments, the unassigned-component roster),
// so EVERY handler below does two things:
//
//  1. Admin-gates via server.requireAdmin — the same gate Stream G applied
//     to the LLM settings/usage endpoints. The gate was applied to LLM
//     settings but not retroactively to this RBAC admin surface; the
//     resulting privilege escalation (any authenticated principal could
//     grant itself a policy or a zone with arbitrary allowed-actions) is
//     documented in DECISIONS.md (D-0012). TestAdminRoutes_AllRequireAdminGate
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
	repo rbac.Repository
	// principals is the identity-registry read path for the Users page
	// (GET /admin/principals). nil-safe: handlers that need it report 503 when
	// it is unwired, the same posture the surface uses for any optional dep.
	principals rbac.PrincipalRepository
	// provisioner is the admin-grant seam (GrantAdmin) the admin-add handler
	// wraps. nil-safe as above.
	provisioner adminProvisioner
	// principalAdmin is the disable/enable orchestration the principal-status
	// handlers wrap. nil-safe as above.
	principalAdmin adminPrincipalLifecycle
	// adminEmail is the configured bootstrap admin (auth.admin_email). Empty
	// when unset. The admin-remove handler refuses to demote the principal it
	// derives to (user:<adminEmail>) because the OIDC bootstrap re-grants admin
	// on that principal's next login unless the config is changed first.
	adminEmail string
	server     *Server
}

func (s *Server) registerAdminRoutes(mux *http.ServeMux, prefix string) {
	if s.services == nil || s.services.RBAC == nil {
		return // RBAC not configured — skip
	}
	h := &adminHandler{repo: s.services.RBAC, server: s}
	// Optional Stage 3 dependencies — wired together with RBAC in server
	// startup. Read defensively so a partially-wired test harness degrades to
	// a clean 503 from the handlers that need them rather than a nil-deref.
	h.principals = s.services.Principals
	h.provisioner = s.services.Provisioner
	h.principalAdmin = s.services.PrincipalAdmin
	if s.services.Config != nil {
		h.adminEmail = s.services.Config.Auth.AdminEmail
	}
	admin := prefix + "/admin"

	mux.HandleFunc(fmt.Sprintf("GET %s/zones", admin), h.listZones)
	mux.HandleFunc(fmt.Sprintf("POST %s/zones", admin), h.createZone)
	mux.HandleFunc(fmt.Sprintf("PATCH %s/zones/{id}", admin), h.updateZone)
	mux.HandleFunc(fmt.Sprintf("DELETE %s/zones/{id}", admin), h.deleteZone)

	mux.HandleFunc(fmt.Sprintf("GET %s/component-zones", admin), h.listAssignments)
	mux.HandleFunc(fmt.Sprintf("POST %s/component-zones", admin), h.assignComponentZone)
	mux.HandleFunc(fmt.Sprintf("DELETE %s/component-zones/{componentID}", admin), h.unassignComponentZone)

	mux.HandleFunc(fmt.Sprintf("GET %s/policies", admin), h.listPolicies)
	mux.HandleFunc(fmt.Sprintf("POST %s/policies", admin), h.createPolicy)
	mux.HandleFunc(fmt.Sprintf("POST %s/policies/revoke", admin), h.revokePolicy)
	mux.HandleFunc(fmt.Sprintf("DELETE %s/policies/{id}", admin), h.deletePolicy)

	mux.HandleFunc(fmt.Sprintf("GET %s/unassigned", admin), h.listUnassigned)

	mux.HandleFunc(fmt.Sprintf("GET %s/admins", admin), h.listAdmins)
	mux.HandleFunc(fmt.Sprintf("POST %s/admins", admin), h.addAdmin)
	mux.HandleFunc(fmt.Sprintf("DELETE %s/admins/{principal}", admin), h.removeAdmin)

	mux.HandleFunc(fmt.Sprintf("GET %s/principals", admin), h.listPrincipals)
	mux.HandleFunc(fmt.Sprintf("POST %s/principals/{principal}/disable", admin), h.disablePrincipal)
	mux.HandleFunc(fmt.Sprintf("POST %s/principals/{principal}/enable", admin), h.enablePrincipal)

	// A001-COREGOV CC-04 — per-component-type auto_promote_reads flag. GET
	// lists the full per-type view; POST flips one type's flag. Both
	// admin-gated; the setter validates the type against the authoritative
	// component-type enum and the mutation service writes value+audit atomically.
	mux.HandleFunc(fmt.Sprintf("GET %s/read-promotions", admin), h.listReadPromotions)
	mux.HandleFunc(fmt.Sprintf("POST %s/read-promotions", admin), h.setReadPromotion)

	// D-0026 unit 3 — per-component credential authz/connectivity status surface.
	// A passive Describe listing (no backend contact), a deliberate live Probe,
	// and the explicit captured-stderr fetch. All admin-gated + audited like the
	// rest of this surface; see credential_status.go for the serialization-boundary
	// rationale (only the Diagnostic/Describe halves ever reach a response).
	mux.HandleFunc(fmt.Sprintf("GET %s/credential-status", admin), h.listCredentialStatus)
	mux.HandleFunc(fmt.Sprintf("POST %s/credential-status/{componentID}/probe", admin), h.probeCredentialStatus)
	mux.HandleFunc(fmt.Sprintf("POST %s/credential-status/{componentID}/probe/stderr", admin), h.credentialStderr)
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

// --- Component zone assignment endpoints ---

func (h *adminHandler) listAssignments(w http.ResponseWriter, r *http.Request) {
	if _, gated := h.server.requireAdmin(w, r); gated {
		return
	}
	assignments, err := h.repo.ListAssignments(r.Context())
	if err != nil {
		writeInternalError(w, err, "list component-zone assignments")
		return
	}
	// Read-class audit (fail-open): the component→zone map is authz topology.
	_ = h.recordAdminAudit(r.Context(), audit.ActionAdminComponentZoneRead, audit.DecisionAllow,
		"admin_read", audit.Details{Target: "component_zones"}, "admin:component_zone.read")
	writeJSON(w, http.StatusOK, map[string]any{
		"assignments": assignments,
		"count":       len(assignments),
	})
}

func (h *adminHandler) assignComponentZone(w http.ResponseWriter, r *http.Request) {
	if _, gated := h.server.requireAdmin(w, r); gated {
		return
	}
	var a rbac.ComponentZoneAssignment
	if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
		writeBadRequest(w, err, "assign component zone", "invalid request body")
		return
	}
	if a.ComponentID == "" || a.ZoneID == "" || a.AssignedBy == "" {
		writeBadRequest(w, nil, "assign component zone", "component_id, zone_id, and assigned_by are required")
		return
	}

	// The repository captures the prior assignment and writes the audit row in
	// the same transaction as the upsert; the handler threads the actor down.
	if err := h.repo.UpsertAssignment(r.Context(), a, h.actor(r)); err != nil {
		writeInternalError(w, err, "assign component zone")
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
	// Validate the principal carries a reserved kind prefix (user:/group:/svc:)
	// via rbac.HasReservedPrefix. Without it an operator typo grants an unprefixed
	// string that gates nobody. Both grant paths (this and addAdmin) validate
	// identically — this is the single audited writer now that the CLI grant path
	// is gone (identity Stage 4).
	if !rbac.HasReservedPrefix(p.Principal) {
		writeBadRequest(w, nil, "create policy",
			fmt.Sprintf("principal %q must carry a reserved prefix (%q, %q, or %q)",
				p.Principal, rbac.PrefixUser, rbac.PrefixGroup, rbac.PrefixSvc))
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

// --- Unassigned components endpoint ---

func (h *adminHandler) listUnassigned(w http.ResponseWriter, r *http.Request) {
	if _, gated := h.server.requireAdmin(w, r); gated {
		return
	}
	ids, err := h.repo.ListUnassignedComponentIDs(r.Context())
	if err != nil {
		writeInternalError(w, err, "list unassigned components")
		return
	}
	// Read-class audit (fail-open): the unassigned roster is part of the
	// component→zone authz map; recorded under the same component_zone.read verb.
	_ = h.recordAdminAudit(r.Context(), audit.ActionAdminComponentZoneRead, audit.DecisionAllow,
		"admin_read", audit.Details{Target: "unassigned"}, "admin:component_zone.read")
	writeJSON(w, http.StatusOK, map[string]any{
		"component_ids": ids,
		"count":         len(ids),
	})
}

// --- Zone edit / delete endpoints (Stage 3) ---

// updateZone applies a partial update (name, description, allowed_actions; any
// subset) to the zone identified by the {id} path value. The repository writes
// the zone.update audit row in the same transaction as the update; the handler
// threads the acting principal down. A missing zone maps to the standard 404
// not-found shape.
func (h *adminHandler) updateZone(w http.ResponseWriter, r *http.Request) {
	if _, gated := h.server.requireAdmin(w, r); gated {
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeBadRequest(w, nil, "update zone", "zone id is required")
		return
	}
	// Pointer/slice fields distinguish "field omitted" (leave as-is) from
	// "field present" (apply, including an explicit empty value).
	var patch struct {
		Name           *string       `json:"name"`
		Description    *string       `json:"description"`
		AllowedActions []rbac.Action `json:"allowed_actions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeBadRequest(w, err, "update zone", "invalid request body")
		return
	}

	existing, err := h.repo.GetZone(r.Context(), id)
	if err != nil {
		writeInternalError(w, err, "update zone")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, errorCodeNotFound, fmt.Sprintf("zone not found: %s", id))
		return
	}

	merged := *existing
	if patch.Name != nil {
		merged.Name = *patch.Name
	}
	if patch.Description != nil {
		merged.Description = *patch.Description
	}
	if patch.AllowedActions != nil {
		merged.AllowedActions = patch.AllowedActions
	}

	updated, err := h.repo.UpdateZone(r.Context(), merged, h.actor(r))
	if err != nil {
		writeInternalError(w, err, "update zone")
		return
	}
	if updated == nil {
		// Raced with a concurrent delete between the existence check and the
		// update — treat as not-found rather than returning a misleading 200.
		writeError(w, http.StatusNotFound, errorCodeNotFound, fmt.Sprintf("zone not found: %s", id))
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// deleteZone deletes the zone identified by {id}. Referencing rbac_policies
// rows cascade away; a zone still referenced by a component assignment is refused
// with 409 (rbac.ErrZoneInUse from the RESTRICT foreign key). The repository
// writes the zone.delete audit row in the same transaction.
func (h *adminHandler) deleteZone(w http.ResponseWriter, r *http.Request) {
	if _, gated := h.server.requireAdmin(w, r); gated {
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeBadRequest(w, nil, "delete zone", "zone id is required")
		return
	}
	if err := h.repo.DeleteZone(r.Context(), id, h.actor(r)); err != nil {
		if errors.Is(err, rbac.ErrZoneInUse) {
			writeError(w, http.StatusConflict, errorCodeConflict,
				fmt.Sprintf("zone %q still has component assignments; unassign those components before deleting", id))
			return
		}
		writeInternalError(w, err, "delete zone")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "deleted", "id": id})
}

// --- Component-zone unassign endpoint (Stage 3) ---

// unassignComponentZone removes the component→zone assignment for {componentID}. After
// removal the component falls back to the policy engine's default unassigned
// behaviour. The repository writes the component_zone.unassign audit row in the
// same transaction; the handler threads the acting principal down.
func (h *adminHandler) unassignComponentZone(w http.ResponseWriter, r *http.Request) {
	if _, gated := h.server.requireAdmin(w, r); gated {
		return
	}
	componentID := r.PathValue("componentID")
	if componentID == "" {
		writeBadRequest(w, nil, "unassign component zone", "component id is required")
		return
	}
	removed, err := h.repo.DeleteAssignment(r.Context(), componentID, h.actor(r))
	if err != nil {
		writeInternalError(w, err, "unassign component zone")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"component_id": componentID,
		"removed":      removed,
		// After unassignment the component falls back to the default unassigned
		// behaviour of the policy engine.
		"note": "component now falls back to the default unassigned zone",
	})
}

// --- Policy revoke-by-natural-key endpoint (Stage 3) ---

// revokePolicy revokes a single principal→zone grant by its natural key,
// wrapping DeletePolicyForPrincipalZone. This is the shape the UI can use
// without first resolving the synthetic policy id (the existing
// DELETE /policies/{id} endpoint is kept). The repository writes the
// policy.revoke audit row in the same transaction.
func (h *adminHandler) revokePolicy(w http.ResponseWriter, r *http.Request) {
	if _, gated := h.server.requireAdmin(w, r); gated {
		return
	}
	var req struct {
		Principal string `json:"principal"`
		ZoneID    string `json:"zone_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, err, "revoke policy", "invalid request body")
		return
	}
	if req.Principal == "" || req.ZoneID == "" {
		writeBadRequest(w, nil, "revoke policy", "principal and zone_id are required")
		return
	}
	removed, err := h.repo.DeletePolicyForPrincipalZone(r.Context(), req.Principal, req.ZoneID, h.actor(r))
	if err != nil {
		writeInternalError(w, err, "revoke policy")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "revoked",
		"principal": req.Principal,
		"zone_id":   req.ZoneID,
		"removed":   removed,
	})
}

// --- Admin roster endpoints (Stage 3) ---

// listAdmins returns the admin roster (principal, granted_by, granted_at,
// reason) and a count. Read-class: the roster leaks who holds admin authority,
// so the access is audited fail-open per §4.
func (h *adminHandler) listAdmins(w http.ResponseWriter, r *http.Request) {
	if _, gated := h.server.requireAdmin(w, r); gated {
		return
	}
	admins, err := h.repo.ListAdmins(r.Context())
	if err != nil {
		writeInternalError(w, err, "list admins")
		return
	}
	_ = h.recordAdminAudit(r.Context(), audit.ActionAdminAdminRead, audit.DecisionAllow,
		"admin_read", audit.Details{Target: "admins"}, "admin:admin.read")
	writeJSON(w, http.StatusOK, map[string]any{
		"admins": admins,
		"count":  len(admins),
	})
}

// addAdmin promotes a principal to admin by wrapping Provisioner.GrantAdmin
// (NOT AddAdmin directly), so the grant-plus-redundant-policy-cleanup invariant
// is not re-implemented. GrantAdmin writes its audit row transactionally via
// the repository's AddAdmin, so the handler writes none. The principal prefix is
// validated with the same guard createPolicy applies.
func (h *adminHandler) addAdmin(w http.ResponseWriter, r *http.Request) {
	if _, gated := h.server.requireAdmin(w, r); gated {
		return
	}
	if h.provisioner == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "admin provisioning is not configured")
		return
	}
	var req struct {
		Principal string `json:"principal"`
		Reason    string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, err, "add admin", "invalid request body")
		return
	}
	if req.Principal == "" {
		writeBadRequest(w, nil, "add admin", "principal is required")
		return
	}
	if !rbac.HasReservedPrefix(req.Principal) {
		writeBadRequest(w, nil, "add admin",
			fmt.Sprintf("principal %q must carry a reserved prefix (%q, %q, or %q)",
				req.Principal, rbac.PrefixUser, rbac.PrefixGroup, rbac.PrefixSvc))
		return
	}

	wasNew, err := h.provisioner.GrantAdmin(r.Context(), rbac.Principal(req.Principal))
	if err != nil {
		writeInternalError(w, err, "add admin")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"principal": req.Principal,
		"granted":   wasNew,
	})
}

// removeAdmin demotes the principal named by {principal}, wrapping RemoveAdmin
// (which writes the admin.revoke audit row in the same transaction). Two guards
// run BEFORE the removal:
//
//   - Bootstrap admin: if the target equals the principal the OIDC bootstrap
//     derives from auth.admin_email (user:<adminEmail>, compared
//     case-insensitively the same way the bootstrap email match is), the
//     removal is refused with 409 — the next matching login would re-grant it,
//     so auth.admin_email must be changed first.
//   - Last admin: removing the sole remaining admin would leave the system with
//     no admin (and no non-circular way to mint one over HTTP), so it is
//     refused with 409.
func (h *adminHandler) removeAdmin(w http.ResponseWriter, r *http.Request) {
	if _, gated := h.server.requireAdmin(w, r); gated {
		return
	}
	principal := r.PathValue("principal")
	if principal == "" {
		writeBadRequest(w, nil, "remove admin", "principal is required")
		return
	}

	// Bootstrap-admin guard. Derive the bootstrap principal exactly as the OIDC
	// callback does — user:<adminEmail> — and compare case-insensitively, the
	// same EqualFold the bootstrap email match uses (internal/auth/handlers.go).
	if h.adminEmail != "" {
		bootstrapPrincipal := rbac.PrefixUser + h.adminEmail
		if strings.EqualFold(principal, bootstrapPrincipal) {
			writeError(w, http.StatusConflict, errorCodeConflict,
				"cannot remove the configured bootstrap admin: change auth.admin_email first, "+
					"otherwise this principal is re-granted admin on its next login")
			return
		}
	}

	// Last-admin guard. Refuse to remove the only remaining admin.
	admins, err := h.repo.ListAdmins(r.Context())
	if err != nil {
		writeInternalError(w, err, "remove admin")
		return
	}
	if len(admins) == 1 && admins[0].Principal == principal {
		writeError(w, http.StatusConflict, errorCodeConflict,
			"cannot remove the last remaining admin: grant another principal admin first")
		return
	}

	removed, err := h.repo.RemoveAdmin(r.Context(), principal, h.actor(r))
	if err != nil {
		writeInternalError(w, err, "remove admin")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"principal": principal,
		"removed":   removed,
	})
}

// --- Principal (identity-registry) endpoints (Stage 3) ---

// listPrincipals returns the identity registry — every provisioned principal
// with its status, timestamps, and disable provenance. This is the data source
// for the Users page. Read-class: audited fail-open per §4.
func (h *adminHandler) listPrincipals(w http.ResponseWriter, r *http.Request) {
	if _, gated := h.server.requireAdmin(w, r); gated {
		return
	}
	if h.principals == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "identity registry is not configured")
		return
	}
	principals, err := h.principals.ListPrincipals(r.Context())
	if err != nil {
		writeInternalError(w, err, "list principals")
		return
	}
	_ = h.recordAdminAudit(r.Context(), audit.ActionAdminPrincipalRead, audit.DecisionAllow,
		"admin_read", audit.Details{Target: "principals"}, "admin:principal.read")
	writeJSON(w, http.StatusOK, map[string]any{
		"principals": principals,
		"count":      len(principals),
	})
}

// disablePrincipal disables the principal named by {principal}, wrapping
// PrincipalAdmin.Disable (sets status disabled, audits in-transaction, and
// deletes the principal's sessions for instant revocation). Self-disable is
// refused with 409 to prevent an admin locking itself out.
func (h *adminHandler) disablePrincipal(w http.ResponseWriter, r *http.Request) {
	caller, gated := h.server.requireAdmin(w, r)
	if gated {
		return
	}
	if h.principalAdmin == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "principal lifecycle is not configured")
		return
	}
	principal := r.PathValue("principal")
	if principal == "" {
		writeBadRequest(w, nil, "disable principal", "principal is required")
		return
	}
	// Self-lockout guard: an admin disabling its own account would revoke its
	// own sessions and (if no other admin exists) strand the surface.
	if string(caller) == principal {
		writeError(w, http.StatusConflict, errorCodeConflict,
			"cannot disable your own account")
		return
	}

	changed, sessionsRevoked, err := h.principalAdmin.Disable(r.Context(), principal, string(caller))
	if err != nil {
		writeInternalError(w, err, "disable principal")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"principal":         principal,
		"status":            rbac.PrincipalStatusDisabled,
		"sessions_revoked":  sessionsRevoked,
		"registry_modified": changed,
	})
}

// enablePrincipal re-enables the principal named by {principal}, wrapping
// PrincipalAdmin.Enable (restores active status, audits in-transaction; no
// sessions are resurrected). The handler writes no audit row — the lower layer
// owns it.
func (h *adminHandler) enablePrincipal(w http.ResponseWriter, r *http.Request) {
	if _, gated := h.server.requireAdmin(w, r); gated {
		return
	}
	if h.principalAdmin == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "principal lifecycle is not configured")
		return
	}
	principal := r.PathValue("principal")
	if principal == "" {
		writeBadRequest(w, nil, "enable principal", "principal is required")
		return
	}
	if _, err := h.principalAdmin.Enable(r.Context(), principal, h.actor(r)); err != nil {
		writeInternalError(w, err, "enable principal")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"principal": principal,
		"status":    rbac.PrincipalStatusActive,
	})
}

// --- Credential authz/connectivity status surface (D-0026 unit 3) ---
//
// These three read-class endpoints are the human instrumentation for threat T4
// ("no Joe to troubleshoot Joe"): they answer "is Joe's credential resolution to
// component X healthy?" without ever exposing the credential itself.
//
// The serialization boundary is enforced STRUCTURALLY, not by discipline. The
// response types below embed ONLY the credential package's serializable halves —
// credential.Descriptor (from the pure Describe) and credential.Diagnostic (the
// staged Resolve/Probe outcome) — plus a plain stderr string sourced through the
// deliberate CapturedStderr() accessor. A *credential.Resolution or a
// credential.Credential is NEVER placed in a response struct, so the credential
// half and the captured plugin stderr have no route into the passive listing or
// the probe response. The captured stderr is reachable only through the third,
// explicitly-requested endpoint — the FE's "show plugin output" affordance — and
// even there it travels in its own response type, never alongside the diagnostic.

// credentialStatusEntry is one component's passive, config-derived credential
// descriptor (from Describe). Descriptor is nil and Error is set when the
// component's config carries no usable/parseable provider; Error is a generic,
// non-sensitive string and never echoes config contents.
type credentialStatusEntry struct {
	ComponentID string                 `json:"component_id"`
	Type        string                 `json:"type"`
	Name        string                 `json:"name"`
	Descriptor  *credential.Descriptor `json:"descriptor,omitempty"`
	Error       string                 `json:"error,omitempty"`
}

// credentialProbeResponse is the staged outcome of a live Resolve(+Probe). It
// carries the Diagnostic half only; StderrAvailable signals that a failure
// captured plugin stderr WITHOUT including the stderr text — the FE fetches that
// separately and deliberately. The struct has no field that can hold credential
// material or stderr.
type credentialProbeResponse struct {
	ComponentID     string                `json:"component_id"`
	Diagnostic      credential.Diagnostic `json:"diagnostic"`
	StderrAvailable bool                  `json:"stderr_available"`
}

// credentialStderrResponse is the ONLY response type that carries the captured
// exec-plugin stderr, returned ONLY by the explicit probe/stderr endpoint. Kept
// structurally separate from credentialProbeResponse so the stderr cannot ride
// along the default probe path.
type credentialStderrResponse struct {
	ComponentID string `json:"component_id"`
	Stderr      string `json:"stderr"`
}

// listCredentialStatus returns the passive, config-derived credential descriptor
// for every registered component via the pure Describe (no backend contact, so a
// page load never probes). Read-class: audited fail-open per §4.
func (h *adminHandler) listCredentialStatus(w http.ResponseWriter, r *http.Request) {
	if _, gated := h.server.requireAdmin(w, r); gated {
		return
	}
	components, err := h.server.services.Store.Components.List(r.Context())
	if err != nil {
		writeInternalError(w, err, "list components for credential status")
		return
	}
	statuses := make([]credentialStatusEntry, 0, len(components))
	for _, c := range components {
		entry := credentialStatusEntry{ComponentID: c.ID, Type: c.Type, Name: c.Name}
		provider, err := credential.Select(c.Config)
		if err != nil {
			// Generic, non-sensitive: never echo the config contents.
			entry.Error = "unknown or unparseable credential provider"
			statuses = append(statuses, entry)
			continue
		}
		desc, err := provider.Describe(c.Config)
		if err != nil {
			entry.Error = "credential configuration could not be described"
			statuses = append(statuses, entry)
			continue
		}
		d := desc // take the address of a loop-local copy
		entry.Descriptor = &d
		statuses = append(statuses, entry)
	}
	_ = h.recordAdminAudit(r.Context(), audit.ActionAdminCredentialStatusRead, audit.DecisionAllow,
		"admin_read", audit.Details{Target: "credential_status"}, "admin:credential_status.read")
	writeJSON(w, http.StatusOK, map[string]any{
		"statuses": statuses,
		"count":    len(statuses),
	})
}

// probeCredentialStatus runs a live Resolve(+Probe) for one component and returns
// the staged Diagnostic. This is the deliberate "does it actually work right now"
// check — it is never triggered automatically. The captured stderr (if any) is
// signalled but NOT included; the FE fetches it through credentialStderr only on
// an explicit human action.
func (h *adminHandler) probeCredentialStatus(w http.ResponseWriter, r *http.Request) {
	if _, gated := h.server.requireAdmin(w, r); gated {
		return
	}
	res, comp, ok := h.resolveAndProbe(w, r)
	if !ok {
		return
	}
	_ = h.recordAdminAudit(r.Context(), audit.ActionAdminCredentialStatusRead, audit.DecisionAllow,
		"admin_probe", audit.Details{Target: "credential_status:" + comp.ID}, "admin:credential_status.read")
	writeJSON(w, http.StatusOK, credentialProbeResponse{
		ComponentID:     comp.ID,
		Diagnostic:      res.Diagnostic,
		StderrAvailable: res.CapturedStderr() != "",
	})
}

// credentialStderr is the deliberate "show plugin output" path: it re-runs
// Resolve(+Probe) for the component and returns ONLY the captured exec-plugin
// stderr via the CapturedStderr accessor. This is the sole endpoint that surfaces
// the untrusted, possibly-secret-bearing plugin text, and it does so only when a
// human explicitly asks — preserving R3 (the stderr is never swept into the
// passive listing, the probe response, or any log/trace).
func (h *adminHandler) credentialStderr(w http.ResponseWriter, r *http.Request) {
	if _, gated := h.server.requireAdmin(w, r); gated {
		return
	}
	res, comp, ok := h.resolveAndProbe(w, r)
	if !ok {
		return
	}
	_ = h.recordAdminAudit(r.Context(), audit.ActionAdminCredentialStatusRead, audit.DecisionAllow,
		"admin_probe_stderr", audit.Details{Target: "credential_status:" + comp.ID}, "admin:credential_status.read")
	writeJSON(w, http.StatusOK, credentialStderrResponse{
		ComponentID: comp.ID,
		Stderr:      res.CapturedStderr(),
	})
}

// resolveAndProbe is the shared body of the two probe endpoints: it looks up the
// component, selects its provider, Resolves, and — only when the Resolve
// succeeded — Probes for live connectivity. A failed Resolve already carries the
// staged failure diagnostic (it stops at mint-attempted), so there is nothing to
// probe. On any error it writes the response and returns ok=false. It returns the
// final *credential.Resolution to the caller, which extracts ONLY the Diagnostic
// half and (for the stderr endpoint) the CapturedStderr accessor — the Resolution
// itself never reaches a response.
func (h *adminHandler) resolveAndProbe(w http.ResponseWriter, r *http.Request) (*credential.Resolution, *store.Component, bool) {
	componentID := r.PathValue("componentID")
	if componentID == "" {
		writeBadRequest(w, nil, "probe credential", "component id is required")
		return nil, nil, false
	}
	comp, err := h.server.services.Store.Components.Get(r.Context(), componentID)
	if err != nil {
		writeInternalError(w, err, "get component for credential probe")
		return nil, nil, false
	}
	if comp == nil {
		writeError(w, http.StatusNotFound, errorCodeNotFound, fmt.Sprintf("component not found: %s", componentID))
		return nil, nil, false
	}
	provider, err := credential.Select(comp.Config)
	if err != nil {
		writeBadRequest(w, err, "probe credential", "component has no usable credential provider")
		return nil, nil, false
	}
	res, err := provider.Resolve(r.Context(), comp.ID, comp.Config)
	if err != nil {
		writeBadRequest(w, err, "probe credential", "credential configuration could not be resolved")
		return nil, nil, false
	}
	if res.Diagnostic.OK {
		probed, err := provider.Probe(r.Context(), res)
		if err != nil {
			writeInternalError(w, err, "probe credential connectivity")
			return nil, nil, false
		}
		res = probed
	}
	return res, comp, true
}

// --- auto_promote_reads flags (A001-COREGOV CC-04) ---
//
// The per-component-type auto_promote_reads flag is the dynamic admit predicate
// the policy engine consults for the agent:core principal on ActionRead. ABSENT
// row == OFF; the table is not seeded, so the GET handler composes the full
// per-type view over the authoritative component-type enum
// (store.AllowedComponentTypes) and overlays the stored ON rows. The setter
// validates the type against that same enum and rejects unknown types (400),
// so arbitrary keys never reach the table; writes go through the mutation
// service, which commits the flag and its admin_access audit row atomically.

// readPromotionView is one component-type's auto_promote_reads state.
type readPromotionView struct {
	ComponentType string `json:"component_type"`
	Enabled       bool   `json:"enabled"`
}

// listReadPromotions returns the auto_promote_reads flag for every known
// component type (the full enum, with OFF for types that have no row). Admin-
// gated; read-class audit (fail-open).
func (h *adminHandler) listReadPromotions(w http.ResponseWriter, r *http.Request) {
	if _, gated := h.server.requireAdmin(w, r); gated {
		return
	}
	if h.server.services == nil || h.server.services.PromoteReads == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "read-promotions service not available")
		return
	}
	stored, err := h.server.services.PromoteReads.Repo().List(r.Context())
	if err != nil {
		writeInternalError(w, err, "list read promotions")
		return
	}
	types := store.AllowedComponentTypes()
	views := make([]readPromotionView, 0, len(types))
	for _, t := range types {
		views = append(views, readPromotionView{ComponentType: t, Enabled: stored[t]})
	}
	// Read-class audit: the promotion topology is authz-adjacent, so the access
	// is recorded (parallel to the other admin .read verbs). Fail-open per §4.
	_ = h.recordAdminAudit(r.Context(), audit.ActionAdminReadPromoteRead, audit.DecisionAllow,
		"admin_read", audit.Details{Target: "read_promotions"}, "admin:read_promotion.read")
	writeJSON(w, http.StatusOK, map[string]any{
		"read_promotions": views,
		"count":           len(views),
	})
}

type setReadPromotionRequest struct {
	ComponentType string `json:"component_type"`
	Enabled       bool   `json:"enabled"`
}

// setReadPromotion flips a single component-type's auto_promote_reads flag.
// Admin-gated; the type is validated against the authoritative component-type
// enum (unknown types are rejected 400 before any write). The mutation service
// commits the flag and its audit row in one transaction (fail-closed). The
// audit row is written by the mutation service itself, so the handler writes
// none of its own — same division the principal disable/enable handlers use.
func (h *adminHandler) setReadPromotion(w http.ResponseWriter, r *http.Request) {
	if _, gated := h.server.requireAdmin(w, r); gated {
		return
	}
	var req setReadPromotionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, err, "set read promotion", "invalid request body")
		return
	}
	if req.ComponentType == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "component_type is required")
		return
	}
	if !store.IsValidComponentType(req.ComponentType) {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest,
			fmt.Sprintf("unknown component type %q", req.ComponentType))
		return
	}
	if h.server.services == nil || h.server.services.PromoteReads == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "read-promotions service not available")
		return
	}
	if err := h.server.services.PromoteReads.SetPromoted(r.Context(), req.ComponentType, req.Enabled); err != nil {
		writeInternalError(w, err, "set read promotion")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"component_type": req.ComponentType,
		"enabled":        req.Enabled,
	})
}
