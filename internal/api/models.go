package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/llmfactory"
)

// newModelAdapter builds an LLM adapter for a model config. It is a package
// var so tests can substitute a fake without real provider credentials, mirror-
// ing the seam pattern used elsewhere (e.g. repl.runModelSelector).
var newModelAdapter = llmfactory.NewAdapter

// modelHandler serves the /model control plane: list available models and
// hot-swap the active one. After the Phase 2 runtime collapse this is the
// single place a model switch happens — the CLI's /model command drives it
// over HTTP instead of swapping a CLI-local adapter.
type modelHandler struct{ server *Server }

type modelsListResponse struct {
	Available []string `json:"available"`
	Current   string   `json:"current"`
}

type setModelRequest struct {
	Name string `json:"name"`
}

type setModelResponse struct {
	Current  string `json:"current"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

// handleList returns the configured model keys and the active one.
func (h *modelHandler) handleList(w http.ResponseWriter, r *http.Request) {
	cfg := h.server.services.Config
	if cfg == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "LLM config not available")
		return
	}
	// The live current model is authoritative on the swappable adapter; fall
	// back to config for the (non-prod) case where LLM is not swappable.
	current := cfg.LLM.Current
	if sw, ok := h.server.services.LLM.(*llm.SwappableAdapter); ok {
		current = sw.Current()
	}
	writeJSON(w, http.StatusOK, modelsListResponse{
		Available: cfg.LLM.ModelNames(),
		Current:   current,
	})
}

// handleSetCurrent hot-swaps the active model on the single LLM contact point.
func (h *modelHandler) handleSetCurrent(w http.ResponseWriter, r *http.Request) {
	var req setModelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, err, "set model", "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "name is required")
		return
	}

	cfg := h.server.services.Config
	if cfg == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "LLM config not available")
		return
	}
	mc, ok := cfg.LLM.Available[req.Name]
	if !ok {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, fmt.Sprintf("unknown model %q", req.Name))
		return
	}

	sw, ok := h.server.services.LLM.(*llm.SwappableAdapter)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "model switching not available")
		return
	}

	adapter, err := newModelAdapter(r.Context(), mc)
	if err != nil {
		// Most commonly a missing API key for the target provider.
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, fmt.Sprintf("cannot switch to %q: %s", req.Name, err))
		return
	}

	sw.Swap(adapter, req.Name)
	writeJSON(w, http.StatusOK, setModelResponse{
		Current:  req.Name,
		Provider: mc.Provider,
		Model:    mc.Model,
	})
}

func (s *Server) registerModelRoutes(mux *http.ServeMux, prefix string) {
	h := &modelHandler{server: s}
	mux.HandleFunc(fmt.Sprintf("GET %s/models", prefix), h.handleList)
	mux.HandleFunc(fmt.Sprintf("POST %s/models/current", prefix), h.handleSetCurrent)
}
