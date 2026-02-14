package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/store"
)

// clarificationHandler wraps clarification-related HTTP handlers
type clarificationHandler struct {
	storeInst            *store.Store
	clarificationService *core.ClarificationService
}

// handleListClarifications returns all pending clarifications
// GET /api/v1/clarifications
func (h *clarificationHandler) handleListClarifications(w http.ResponseWriter, r *http.Request) {
	if h.storeInst == nil || h.storeInst.Clarifications == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "store not available")
		return
	}

	clarifications, err := h.storeInst.Clarifications.ListPending(r.Context())
	if err != nil {
		writeInternalError(w, err, "list pending clarifications")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"clarifications": clarifications,
		"count":          len(clarifications),
	})
}

// handleAnswerClarification marks a clarification as answered
// POST /api/v1/clarifications/{id}/answer
func (h *clarificationHandler) handleAnswerClarification(w http.ResponseWriter, r *http.Request) {
	if h.storeInst == nil || h.storeInst.Clarifications == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "store not available")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing clarification id in path")
		return
	}

	var req struct {
		Answer     string `json:"answer"`
		AnsweredBy string `json:"answered_by,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "invalid JSON payload", map[string]any{
			"error": err.Error(),
		})
		return
	}

	if req.Answer == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing required field 'answer'")
		return
	}

	// Default answered_by to "user" if not provided
	if req.AnsweredBy == "" {
		req.AnsweredBy = "user"
	}

	if err := h.storeInst.Clarifications.Answer(r.Context(), id, req.Answer, req.AnsweredBy); err != nil {
		writeInternalError(w, err, "answer clarification")
		return
	}

	// Apply graph operations from the answered clarification
	if h.clarificationService != nil {
		if err := h.clarificationService.ApplyAnswer(r.Context(), id, req.Answer, req.AnsweredBy); err != nil {
			slog.WarnContext(r.Context(), "failed to apply graph operations from clarification answer",
				"clarification_id", id,
				"error", err)
			// Don't fail the response - clarification was already answered successfully
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"message": fmt.Sprintf("clarification %s answered successfully", id),
	})
}

// handleDismissClarification marks a clarification as dismissed
// POST /api/v1/clarifications/{id}/dismiss
func (h *clarificationHandler) handleDismissClarification(w http.ResponseWriter, r *http.Request) {
	if h.storeInst == nil || h.storeInst.Clarifications == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "store not available")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing clarification id in path")
		return
	}

	if err := h.storeInst.Clarifications.Dismiss(r.Context(), id); err != nil {
		writeInternalError(w, err, "dismiss clarification")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"message": fmt.Sprintf("clarification %s dismissed successfully", id),
	})
}
