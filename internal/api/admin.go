package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/jaimegago/joe/internal/rbac"
)

// adminHandler exposes RBAC management endpoints.
//
// Every route under /api/v1/admin/ mutates or exposes authorization state
// (zones, policies, source-zone assignments, the unassigned-source roster),
// so EVERY handler below admin-gates via server.requireAdmin — the same gate
// Stream G applied to the LLM settings/usage endpoints. The gate was applied
// to LLM settings but not retroactively to this RBAC admin surface; the
// resulting privilege escalation (any authenticated principal could grant
// itself a policy or a zone with arbitrary allowed-actions) is documented in
// ADMIN_SURFACE_AUDIT.md (Launch Blocker 1) and DECISIONS.md (D-0012). The
// structural invariant TestAdminRoutes_AllRequireAdminGate
// (admin_gate_guard_test.go) fails the build if a future admin route is
// registered without the gate.
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

	created, err := h.repo.CreateZone(r.Context(), z)
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

	if err := h.repo.UpsertAssignment(r.Context(), a); err != nil {
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

	created, err := h.repo.CreatePolicy(r.Context(), p)
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

	if err := h.repo.DeletePolicy(r.Context(), id); err != nil {
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
	writeJSON(w, http.StatusOK, map[string]any{
		"source_ids": ids,
		"count":      len(ids),
	})
}
