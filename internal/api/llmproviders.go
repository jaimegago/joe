package api

import (
	"fmt"
	"net/http"
	"sort"

	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/llmfactory"
)

// Stream G phase G5 — providers endpoint.
//
// GET /api/v1/llm/providers returns, for every model the deployment
// knows about, whether it has API-key credentials available — as a
// boolean. The endpoint NEVER returns key material in any form: no
// value, no prefix, no length. Key presence is determined through
// llmfactory.HasProviderAPIKey, which checks the relevant environment
// variable WITHOUT reading the key value out. The handler must not
// know or hardcode the per-provider env-var names — that mapping
// lives in the factory alongside ValidateAPIKeys so a future provider
// addition updates one place.
//
// "Current selection" comes from the live SwappableAdapter when the
// runtime LLM is swappable, mirroring handleSetCurrent in models.go;
// otherwise it falls back to cfg.LLM.Current. Identical predicate as
// the existing /api/v1/models handler.

type providerEntry struct {
	Name       string `json:"name"`
	Provider   string `json:"provider"`
	Model      string `json:"model"`
	Configured bool   `json:"configured"`
	KeyPresent bool   `json:"key_present"`
}

type providersResponse struct {
	Providers []providerEntry `json:"providers"`
	Current   string          `json:"current"`
}

func (s *Server) handleListProviders(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Config == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "LLM config not available")
		return
	}
	cfg := s.services.Config
	current := cfg.LLM.Current
	if sw, ok := s.services.LLM.(*llm.SwappableAdapter); ok {
		current = sw.Current()
	}

	names := cfg.LLM.ModelNames()
	out := make([]providerEntry, 0, len(names))
	for _, name := range names {
		mc := cfg.LLM.Available[name]
		out = append(out, providerEntry{
			Name:       name,
			Provider:   mc.Provider,
			Model:      mc.Model,
			Configured: true,
			KeyPresent: llmfactory.HasProviderAPIKey(mc.Provider),
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })

	writeJSON(w, http.StatusOK, providersResponse{
		Providers: out,
		Current:   current,
	})
}

func (s *Server) registerLLMProvidersRoutes(mux *http.ServeMux, prefix string) {
	mux.HandleFunc(fmt.Sprintf("GET %s/llm/providers", prefix), s.handleListProviders)
}
