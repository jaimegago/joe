package api

import (
	"fmt"
	"net/http"

	"github.com/jaimegago/joe/internal/warnings"
)

// warningsHandler exposes the §E warnings surface — Phase 1 Change 4.
//
// Humans cannot raise warnings via HTTP: there is no POST /warnings.
// The raise path is for joe-core internal use only (Joe raises warnings
// for incident judgments it is not authorized to act on, per §E1 / §R3).
// Humans only read the list and (in a later change) mark a warning
// reviewed.
type warningsHandler struct {
	repo warnings.Repository
}

func (s *Server) registerWarningsRoutes(mux *http.ServeMux, prefix string) {
	if s.services == nil || s.services.Warnings == nil {
		return
	}
	h := &warningsHandler{repo: s.services.Warnings}
	mux.HandleFunc(fmt.Sprintf("GET %s/warnings", prefix), h.list)
}

func (h *warningsHandler) list(w http.ResponseWriter, r *http.Request) {
	items, err := h.repo.ListWarnings(r.Context())
	if err != nil {
		writeInternalError(w, err, "list warnings")
		return
	}
	if items == nil {
		items = []warnings.Warning{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"warnings": items,
		"count":    len(items),
	})
}
