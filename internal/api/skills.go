package api

import (
	"fmt"
	"net/http"

	"github.com/jaimegago/joe/internal/skills"
)

// skillsHandler exposes runtime control over the in-memory skill registry.
// Phase 3 ships one endpoint — POST /api/v1/skills/reload — which triggers a
// synchronous rescan of ~/.joe/skills/ and atomically swaps the active
// registry. The watcher does this automatically on filesystem events; the
// manual endpoint is the escape hatch for unreliable filesystems (network
// mounts, K8s volumes) and for CI/CD integrations that want to push a
// reload right after merging a skills-repo PR.
type skillsHandler struct {
	watcher *skills.Watcher
}

// registerSkillsRoutes registers skill control endpoints. If the watcher is
// nil — meaning skills hot reload was disabled or could not start — the
// reload endpoint still responds, but with a 503 and an explanatory message.
// Returning 503 rather than 404 makes it explicit that the feature exists
// but is not currently available, which is the right signal for CI/CD code
// that retries on 5xx but bails on 4xx.
func (s *Server) registerSkillsRoutes(mux *http.ServeMux, prefix string) {
	h := &skillsHandler{watcher: s.skillsWatcher()}
	mux.HandleFunc(fmt.Sprintf("POST %s/skills/reload", prefix), h.handleReload)
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
			"skills hot reload is not enabled on this joe-core instance")
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
