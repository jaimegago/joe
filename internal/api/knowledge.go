package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/jaimegago/joe/internal/knowledge"
)

// registerKnowledgeRoutes registers all knowledge store routes.
func (s *Server) registerKnowledgeRoutes(mux *http.ServeMux, prefix string) {
	h := &knowledgeHandler{server: s}
	// Knowledge entries
	mux.HandleFunc("POST "+prefix+"/knowledge/entries", h.handleCreateEntry)
	mux.HandleFunc("GET "+prefix+"/knowledge/entries", h.handleListEntries)
	mux.HandleFunc("GET "+prefix+"/knowledge/entries/{id}", h.handleGetEntry)
	mux.HandleFunc("PUT "+prefix+"/knowledge/entries/{id}", h.handleUpdateEntry)
	mux.HandleFunc("DELETE "+prefix+"/knowledge/entries/{id}", h.handleDeleteEntry)
	// Semantic search
	mux.HandleFunc("POST "+prefix+"/knowledge/search", h.handleSearch)
	// External sync sources
	mux.HandleFunc("POST "+prefix+"/knowledge/sources", h.handleCreateSource)
	mux.HandleFunc("GET "+prefix+"/knowledge/sources", h.handleListSources)
	mux.HandleFunc("DELETE "+prefix+"/knowledge/sources/{id}", h.handleDeleteSource)
	mux.HandleFunc("POST "+prefix+"/knowledge/sources/{id}/sync", h.handleTriggerSync)
}

type knowledgeHandler struct{ server *Server }

// --- entry handlers ---

func (h *knowledgeHandler) handleCreateEntry(w http.ResponseWriter, r *http.Request) {
	svc := h.server.services.Knowledge
	if svc == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "knowledge service not available")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeBadRequest(w, err, "read body", "failed to read request body")
		return
	}

	var e knowledge.Entry
	if err := json.Unmarshal(body, &e); err != nil {
		writeBadRequest(w, err, "parse entry", "invalid JSON body")
		return
	}
	if e.Title == "" || e.Content == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "title and content are required")
		return
	}
	if e.Tier == "" {
		e.Tier = knowledge.TierCurated
	}

	if err := svc.Create(r.Context(), &e); err != nil {
		writeInternalError(w, err, "create knowledge entry")
		return
	}
	writeJSON(w, http.StatusCreated, e)
}

func (h *knowledgeHandler) handleListEntries(w http.ResponseWriter, r *http.Request) {
	svc := h.server.services.Knowledge
	if svc == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "knowledge service not available")
		return
	}

	q := r.URL.Query()
	filter := knowledge.EntryFilter{
		Tier:       knowledge.Tier(q.Get("tier")),
		SourceType: knowledge.SourceType(q.Get("source_type")),
		SourceID:   q.Get("source_id"),
	}

	entries, err := svc.List(r.Context(), filter)
	if err != nil {
		writeInternalError(w, err, "list knowledge entries")
		return
	}
	if entries == nil {
		entries = []*knowledge.Entry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"entries": entries,
		"count":   len(entries),
	})
}

func (h *knowledgeHandler) handleGetEntry(w http.ResponseWriter, r *http.Request) {
	svc := h.server.services.Knowledge
	if svc == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "knowledge service not available")
		return
	}

	id := r.PathValue("id")
	e, err := svc.Get(r.Context(), id)
	if err != nil {
		// Only a genuine miss (the store's ErrEntryNotFound sentinel) is a 404;
		// any other store failure is a 500 — writeInternalError logs it without
		// echoing internals to the client.
		if errors.Is(err, knowledge.ErrEntryNotFound) {
			writeError(w, http.StatusNotFound, errorCodeNotFound, "knowledge entry not found", map[string]any{"id": id})
			return
		}
		writeInternalError(w, err, "get knowledge entry")
		return
	}
	writeJSON(w, http.StatusOK, e)
}

func (h *knowledgeHandler) handleUpdateEntry(w http.ResponseWriter, r *http.Request) {
	svc := h.server.services.Knowledge
	if svc == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "knowledge service not available")
		return
	}

	id := r.PathValue("id")
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeBadRequest(w, err, "read body", "failed to read request body")
		return
	}

	var e knowledge.Entry
	if err := json.Unmarshal(body, &e); err != nil {
		writeBadRequest(w, err, "parse entry", "invalid JSON body")
		return
	}
	e.ID = id

	if err := svc.Update(r.Context(), &e); err != nil {
		// Update returns a meaningful error for immutable tier 1 entries.
		writeError(w, http.StatusUnprocessableEntity, errorCodeInvalidRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, e)
}

func (h *knowledgeHandler) handleDeleteEntry(w http.ResponseWriter, r *http.Request) {
	svc := h.server.services.Knowledge
	if svc == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "knowledge service not available")
		return
	}

	id := r.PathValue("id")
	if err := svc.Delete(r.Context(), id); err != nil {
		writeError(w, http.StatusUnprocessableEntity, errorCodeInvalidRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "id": id})
}

// --- search handler ---

type searchRequest struct {
	Query         string           `json:"query"`
	TopK          int              `json:"top_k"`
	TierFilter    []knowledge.Tier `json:"tier_filter,omitempty"`
	MinConfidence float64          `json:"min_confidence,omitempty"`
}

func (h *knowledgeHandler) handleSearch(w http.ResponseWriter, r *http.Request) {
	svc := h.server.services.Knowledge
	if svc == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "knowledge service not available")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeBadRequest(w, err, "read body", "failed to read request body")
		return
	}

	var req searchRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeBadRequest(w, err, "parse search request", "invalid JSON body")
		return
	}
	if req.Query == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "query is required")
		return
	}
	topK := req.TopK
	if topK <= 0 {
		cfg := h.server.services.Config
		if cfg != nil && cfg.Knowledge.SemanticTopK > 0 {
			topK = cfg.Knowledge.SemanticTopK
		} else {
			topK = 5
		}
	}

	results, err := svc.Search(r.Context(), knowledge.SearchRequest{
		Query:         req.Query,
		TopK:          topK,
		TierFilter:    req.TierFilter,
		MinConfidence: req.MinConfidence,
	})
	if err != nil {
		writeInternalError(w, err, "semantic search")
		return
	}
	if results == nil {
		results = []knowledge.SearchResult{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"results": results,
		"count":   len(results),
		"query":   req.Query,
	})
}

// --- source handlers ---

func (h *knowledgeHandler) handleCreateSource(w http.ResponseWriter, r *http.Request) {
	svc := h.server.services.Knowledge
	if svc == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "knowledge service not available")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeBadRequest(w, err, "read body", "failed to read request body")
		return
	}

	var src knowledge.KnowledgeSource
	if err := json.Unmarshal(body, &src); err != nil {
		writeBadRequest(w, err, "parse source", "invalid JSON body")
		return
	}
	if src.Type == "" || src.Name == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "type and name are required")
		return
	}

	if err := svc.CreateSource(r.Context(), &src); err != nil {
		writeInternalError(w, err, "create knowledge source")
		return
	}
	writeJSON(w, http.StatusCreated, src)
}

func (h *knowledgeHandler) handleListSources(w http.ResponseWriter, r *http.Request) {
	svc := h.server.services.Knowledge
	if svc == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "knowledge service not available")
		return
	}

	sources, err := svc.ListSources(r.Context())
	if err != nil {
		writeInternalError(w, err, "list knowledge sources")
		return
	}
	if sources == nil {
		sources = []*knowledge.KnowledgeSource{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"sources": sources,
		"count":   len(sources),
	})
}

func (h *knowledgeHandler) handleDeleteSource(w http.ResponseWriter, r *http.Request) {
	svc := h.server.services.Knowledge
	if svc == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "knowledge service not available")
		return
	}

	id := r.PathValue("id")
	if err := svc.DeleteSource(r.Context(), id); err != nil {
		writeInternalError(w, err, "delete knowledge source")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "id": id})
}

func (h *knowledgeHandler) handleTriggerSync(w http.ResponseWriter, r *http.Request) {
	svc := h.server.services.Knowledge
	if svc == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "knowledge service not available")
		return
	}

	id := r.PathValue("id")
	src, err := svc.GetSource(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, errorCodeNotFound, "knowledge source not found", map[string]any{"id": id})
		return
	}

	// The sync coordinator picks up sources via polling; for now we just validate
	// the source exists and return its current state. Background sync handles the rest.
	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":  "sync_queued",
		"source":  src,
		"message": "sync will be performed by the background coordinator",
	})
}
