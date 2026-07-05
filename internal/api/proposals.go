package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/jaimegago/joe/internal/knowledge/drafts"
	"github.com/jaimegago/joe/internal/knowledge/proposals"
)

// registerProposalRoutes registers documentation proposal routes.
func (s *Server) registerProposalRoutes(mux *http.ServeMux, prefix string) {
	h := &proposalHandler{server: s}
	mux.HandleFunc("POST "+prefix+"/knowledge/proposals", h.handleCreateProposal)
	mux.HandleFunc("GET "+prefix+"/knowledge/proposals", h.handleListProposals)
	mux.HandleFunc("GET "+prefix+"/knowledge/proposals/{id}", h.handleGetProposal)
	mux.HandleFunc("POST "+prefix+"/knowledge/proposals/{id}/approve", h.handleApproveProposal)
	mux.HandleFunc("POST "+prefix+"/knowledge/proposals/{id}/reject", h.handleRejectProposal)
}

type proposalHandler struct{ server *Server }

func (h *proposalHandler) handleCreateProposal(w http.ResponseWriter, r *http.Request) {
	if h.server.services.Proposals == nil || h.server.services.DocDrafter == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "proposal service not available")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeBadRequest(w, err, "read body", "failed to read request body")
		return
	}

	var req struct {
		Topic      string               `json:"topic"`
		TargetType proposals.TargetType `json:"target_type"`
		TargetID   string               `json:"target_id"`
		Context    string               `json:"context,omitempty"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeBadRequest(w, err, "parse request", "invalid JSON body")
		return
	}
	if req.Topic == "" || req.TargetType == "" || req.TargetID == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "topic, target_type, and target_id are required")
		return
	}

	proposal, err := h.server.services.DocDrafter.Generate(r.Context(), drafts.GenerateRequest{
		Topic:      req.Topic,
		TargetType: req.TargetType,
		TargetID:   req.TargetID,
		Context:    req.Context,
	})
	if err != nil {
		writeInternalError(w, err, "generate doc draft")
		return
	}
	writeJSON(w, http.StatusCreated, proposal)
}

func (h *proposalHandler) handleListProposals(w http.ResponseWriter, r *http.Request) {
	if h.server.services.Proposals == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "proposal service not available")
		return
	}

	q := r.URL.Query()
	statusFilter := proposals.ProposalStatus(q.Get("status"))
	targetType := proposals.TargetType(q.Get("target_type"))

	list, err := h.server.services.Proposals.List(r.Context(), statusFilter, targetType)
	if err != nil {
		writeInternalError(w, err, "list proposals")
		return
	}
	if list == nil {
		list = []*proposals.Proposal{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"proposals": list,
		"count":     len(list),
	})
}

func (h *proposalHandler) handleGetProposal(w http.ResponseWriter, r *http.Request) {
	if h.server.services.Proposals == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "proposal service not available")
		return
	}

	id := r.PathValue("id")
	p, err := h.server.services.Proposals.Get(r.Context(), id)
	if err != nil {
		// Only a genuine miss (the store's ErrNotFound sentinel) is a 404; any
		// other store failure is a 500 — writeInternalError logs it without
		// echoing internals to the client.
		if errors.Is(err, proposals.ErrNotFound) {
			writeError(w, http.StatusNotFound, errorCodeNotFound, "proposal not found", map[string]any{"id": id})
			return
		}
		writeInternalError(w, err, "get proposal")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (h *proposalHandler) handleApproveProposal(w http.ResponseWriter, r *http.Request) {
	if h.server.services.Proposals == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "proposal service not available")
		return
	}

	id := r.PathValue("id")
	if err := h.server.services.Proposals.Approve(r.Context(), id); err != nil {
		writeError(w, http.StatusUnprocessableEntity, errorCodeInvalidRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "approved", "id": id})
}

func (h *proposalHandler) handleRejectProposal(w http.ResponseWriter, r *http.Request) {
	if h.server.services.Proposals == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "proposal service not available")
		return
	}

	id := r.PathValue("id")

	var req struct {
		Reason string `json:"reason"`
	}
	body, _ := io.ReadAll(r.Body)
	_ = json.Unmarshal(body, &req)

	if err := h.server.services.Proposals.Reject(r.Context(), id, req.Reason); err != nil {
		writeError(w, http.StatusUnprocessableEntity, errorCodeInvalidRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "rejected", "id": id})
}
