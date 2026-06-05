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
	"github.com/jaimegago/joe/internal/rbac"
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

	mux.HandleFunc(fmt.Sprintf("GET %s/source-zones", admin), h.listAssignments)
	mux.HandleFunc(fmt.Sprintf("POST %s/source-zones", admin), h.assignSourceZone)
	mux.HandleFunc(fmt.Sprintf("DELETE %s/source-zones/{sourceID}", admin), h.unassignSourceZone)

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
// rows cascade away; a zone still referenced by a source assignment is refused
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
				fmt.Sprintf("zone %q still has source assignments; unassign those sources before deleting", id))
			return
		}
		writeInternalError(w, err, "delete zone")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "deleted", "id": id})
}

// --- Source-zone unassign endpoint (Stage 3) ---

// unassignSourceZone removes the source→zone assignment for {sourceID}. After
// removal the source falls back to the policy engine's default unassigned
// behaviour. The repository writes the source_zone.unassign audit row in the
// same transaction; the handler threads the acting principal down.
func (h *adminHandler) unassignSourceZone(w http.ResponseWriter, r *http.Request) {
	if _, gated := h.server.requireAdmin(w, r); gated {
		return
	}
	sourceID := r.PathValue("sourceID")
	if sourceID == "" {
		writeBadRequest(w, nil, "unassign source zone", "source id is required")
		return
	}
	removed, err := h.repo.DeleteAssignment(r.Context(), sourceID, h.actor(r))
	if err != nil {
		writeInternalError(w, err, "unassign source zone")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"source_id": sourceID,
		"removed":   removed,
		// After unassignment the source falls back to the default unassigned
		// behaviour of the policy engine.
		"note": "source now falls back to the default unassigned zone",
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
