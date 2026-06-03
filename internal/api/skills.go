package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/jaimegago/joe/internal/skills"
)

// skillsHandler exposes runtime control over the in-memory skill registry
// and the on-disk lifecycle of installed skills. Phase 3 shipped
// POST /skills/reload; Phase 4 adds:
//
//   - GET  /skills          List installed skills with status (active,
//     quarantined) — used by the web UI and by
//     operators inspecting what's pending approval.
//   - POST /skills/approve  Move a quarantined skill into the active tree.
//
// Both new endpoints are bearer-authed (same as other admin endpoints) and
// every state change is audit-logged via the Manager.
type skillsHandler struct {
	watcher *skills.Watcher
	manager *skills.Manager
}

// registerSkillsRoutes registers skill control endpoints. If the watcher is
// nil — meaning skills hot reload was disabled or could not start — the
// reload endpoint still responds, but with a 503 and an explanatory message.
// Returning 503 rather than 404 makes it explicit that the feature exists
// but is not currently available, which is the right signal for CI/CD code
// that retries on 5xx but bails on 4xx.
func (s *Server) registerSkillsRoutes(mux *http.ServeMux, prefix string) {
	h := &skillsHandler{watcher: s.skillsWatcher(), manager: s.skillsManager()}
	mux.HandleFunc(fmt.Sprintf("POST %s/skills/reload", prefix), h.handleReload)
	mux.HandleFunc(fmt.Sprintf("GET %s/skills", prefix), h.handleList)
	mux.HandleFunc(fmt.Sprintf("POST %s/skills/approve", prefix), h.handleApprove)
	mux.HandleFunc(fmt.Sprintf("POST %s/skills/reject", prefix), h.handleReject)
}

// skillsWatcher is a tiny accessor that exists so tests (and future
// sub-services) can override the watcher without exposing it on Services
// directly. Today it just reads from the core services bag.
func (s *Server) skillsWatcher() *skills.Watcher {
	if s.services == nil {
		return nil
	}
	return s.services.SkillsWatcher
}

// skillsManager returns the install/approve/reject manager from Services.
// nil when joecored started without ever wiring one — the API handlers
// surface that as 503 ServiceUnavailable.
func (s *Server) skillsManager() *skills.Manager {
	if s.services == nil {
		return nil
	}
	return s.services.SkillsManager
}

// skillsReloadResponse is the JSON shape returned by POST /skills/reload.
type skillsReloadResponse struct {
	Status  string   `json:"status"`
	Trigger string   `json:"trigger"`
	Before  int      `json:"before"`
	After   int      `json:"after"`
	Added   []string `json:"added,omitempty"`
	Removed []string `json:"removed,omitempty"`
	Updated []string `json:"updated,omitempty"`
	Error   string   `json:"error,omitempty"`
}

func (h *skillsHandler) handleReload(w http.ResponseWriter, r *http.Request) {
	if h.watcher == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable,
			"skills hot reload is not enabled on this joe instance")
		return
	}
	result := h.watcher.Reload(r.Context(), "manual")
	resp := skillsReloadResponse{
		Trigger: result.Trigger,
		Before:  result.Before,
		After:   result.After,
		Added:   result.Added,
		Removed: result.Removed,
		Updated: result.Updated,
	}
	if result.Err != nil {
		// The previous registry stays active on failure, but the caller
		// asked for a reload and didn't get one — surface the failure as
		// 500 so CI/CD won't silently believe the new skills are live.
		resp.Status = "failed"
		resp.Error = result.Err.Error()
		writeJSON(w, http.StatusInternalServerError, resp)
		return
	}
	resp.Status = "ok"
	writeJSON(w, http.StatusOK, resp)
}

// skillStatusEntry is one row in the GET /skills response. The shape stays
// flat so a UI or operator can render it without nested lookups.
type skillStatusEntry struct {
	Name             string `json:"name"`
	Description      string `json:"description,omitempty"`
	Repo             string `json:"repo"`
	Ref              string `json:"ref,omitempty"`
	Commit           string `json:"commit,omitempty"`
	Status           string `json:"status"`
	QuarantineReason string `json:"quarantine_reason,omitempty"`
	Hash             string `json:"hash,omitempty"`
}

// skillsListResponse is the GET /skills payload. Splitting active and
// quarantined into separate slices keeps the common UI case ("show me what
// needs approval") cheap — no client-side filtering required.
type skillsListResponse struct {
	Active      []skillStatusEntry `json:"active"`
	Quarantined []skillStatusEntry `json:"quarantined"`
}

func (h *skillsHandler) handleList(w http.ResponseWriter, r *http.Request) {
	if h.manager == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable,
			"skills manager is not available on this joe instance")
		return
	}
	installs, err := h.manager.List()
	if err != nil {
		writeInternalError(w, err, "skills list")
		return
	}

	resp := skillsListResponse{Active: []skillStatusEntry{}, Quarantined: []skillStatusEntry{}}
	for _, in := range installs {
		for _, s := range in.Skills {
			entry := skillStatusEntry{
				Name:             s.Name,
				Repo:             in.Repo,
				Ref:              in.Ref,
				Commit:           in.Commit,
				Status:           in.Status,
				QuarantineReason: in.QuarantineReason,
				Hash:             s.Hash,
			}
			if entry.Status == "" {
				entry.Status = skills.InstallStatusActive
			}
			if in.IsQuarantined() {
				resp.Quarantined = append(resp.Quarantined, entry)
			} else {
				resp.Active = append(resp.Active, entry)
			}
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// skillsApprovalRequest is the body shape for both /skills/approve and
// /skills/reject — a single skill name is all the manager needs to find
// the matching install.
type skillsApprovalRequest struct {
	Name string `json:"name"`
}

// skillsApprovalResponse summarises the resulting state so callers don't
// have to follow up with GET /skills to confirm what changed.
type skillsApprovalResponse struct {
	Status string   `json:"status"`
	Name   string   `json:"name"`
	Repo   string   `json:"repo,omitempty"`
	Commit string   `json:"commit,omitempty"`
	Skills []string `json:"skills"`
}

func (h *skillsHandler) handleApprove(w http.ResponseWriter, r *http.Request) {
	if h.manager == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable,
			"skills manager is not available on this joe instance")
		return
	}
	var req skillsApprovalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, err, "skills approve", "invalid request body")
		return
	}
	if req.Name == "" {
		writeBadRequest(w, nil, "skills approve", "name is required")
		return
	}
	install, err := h.manager.Approve(r.Context(), req.Name)
	if err != nil {
		// Approve returns descriptive errors for not-installed and
		// already-active states; surface them as 400 so a polling client
		// doesn't retry on a permanent failure.
		writeBadRequest(w, err, "skills approve", err.Error())
		return
	}
	names := make([]string, 0, len(install.Skills))
	for _, s := range install.Skills {
		names = append(names, s.Name)
	}
	writeJSON(w, http.StatusOK, skillsApprovalResponse{
		Status: "ok",
		Name:   req.Name,
		Repo:   install.Repo,
		Commit: install.Commit,
		Skills: names,
	})
}

func (h *skillsHandler) handleReject(w http.ResponseWriter, r *http.Request) {
	if h.manager == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable,
			"skills manager is not available on this joe instance")
		return
	}
	var req skillsApprovalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, err, "skills reject", "invalid request body")
		return
	}
	if req.Name == "" {
		writeBadRequest(w, nil, "skills reject", "name is required")
		return
	}
	removed, err := h.manager.Reject(r.Context(), req.Name)
	if err != nil {
		writeBadRequest(w, err, "skills reject", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, skillsApprovalResponse{
		Status: "ok",
		Name:   req.Name,
		Skills: removed,
	})
}
