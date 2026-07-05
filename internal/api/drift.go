package api

import (
	"errors"
	"net/http"

	"github.com/jaimegago/joe/internal/knowledge"
	"github.com/jaimegago/joe/internal/knowledge/drift"
)

// registerDriftRoutes registers documentation drift detection routes.
func (s *Server) registerDriftRoutes(mux *http.ServeMux, prefix string) {
	h := &driftHandler{server: s}
	mux.HandleFunc("GET "+prefix+"/knowledge/drift", h.handleDetectDrift)
	mux.HandleFunc("GET "+prefix+"/knowledge/drift/{id}", h.handleDetectDriftByEntry)
}

type driftHandler struct{ server *Server }

func (h *driftHandler) handleDetectDrift(w http.ResponseWriter, r *http.Request) {
	if h.server.services.DriftDet == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "drift detector not available")
		return
	}

	sourceType := knowledge.SourceType(r.URL.Query().Get("source_type"))
	reports, err := h.server.services.DriftDet.DetectAll(r.Context(), sourceType)
	if err != nil {
		writeInternalError(w, err, "detect drift")
		return
	}
	if reports == nil {
		reports = []*drift.DriftReport{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"reports": reports,
		"count":   len(reports),
	})
}

func (h *driftHandler) handleDetectDriftByEntry(w http.ResponseWriter, r *http.Request) {
	if h.server.services.DriftDet == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "drift detector not available")
		return
	}

	id := r.PathValue("id")
	report, err := h.server.services.DriftDet.Detect(r.Context(), id)
	if err != nil {
		// Distinguish the three failure classes instead of masking everything as
		// a 404 (and never echo an arbitrary err.Error() to the response body):
		// missing entry → 404; non-Tier-2 entry (caller mistake) → 400 with the
		// sentinel's fixed message; anything else is a store/fetch failure → 500
		// via writeInternalError, which logs without echoing.
		if errors.Is(err, knowledge.ErrEntryNotFound) {
			writeError(w, http.StatusNotFound, errorCodeNotFound, "knowledge entry not found", map[string]any{"id": id})
			return
		}
		if errors.Is(err, drift.ErrNotSyncedEntry) {
			writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, drift.ErrNotSyncedEntry.Error(), map[string]any{"id": id})
			return
		}
		writeInternalError(w, err, "detect drift")
		return
	}
	writeJSON(w, http.StatusOK, report)
}
