package api

import (
	"net/http"
	"strconv"

	gitadapter "github.com/jaimegago/joe/internal/adapters/git"
)

func (s *Server) getGitAdapter(w http.ResponseWriter, r *http.Request) (gitadapter.GitAdapter, string, bool) {
	sourceID := r.PathValue("sourceID")

	adapter, err := s.services.Adapters.Get(sourceID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "source not found: " + sourceID})
		return nil, "", false
	}

	ga, ok := adapter.(gitadapter.GitAdapter)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "source is not a git adapter"})
		return nil, "", false
	}

	return ga, sourceID, true
}

func (s *Server) handleGitReadFile(w http.ResponseWriter, r *http.Request) {
	ga, sourceID, ok := s.getGitAdapter(w, r)
	if !ok {
		return
	}

	path := r.URL.Query().Get("path")
	if path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing required query parameter 'path'"})
		return
	}

	content, err := ga.ReadFile(r.Context(), path)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"content":   content,
		"path":      path,
		"source_id": sourceID,
	})
}

func (s *Server) handleGitListFiles(w http.ResponseWriter, r *http.Request) {
	ga, sourceID, ok := s.getGitAdapter(w, r)
	if !ok {
		return
	}

	dir := r.URL.Query().Get("dir")

	files, err := ga.ListFiles(r.Context(), dir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if files == nil {
		files = []gitadapter.FileInfo{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"files":     files,
		"count":     len(files),
		"dir":       dir,
		"source_id": sourceID,
	})
}

func (s *Server) handleGitLog(w http.ResponseWriter, r *http.Request) {
	ga, sourceID, ok := s.getGitAdapter(w, r)
	if !ok {
		return
	}

	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		parsed, err := strconv.Atoi(l)
		if err != nil || parsed <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "limit must be a positive integer"})
			return
		}
		limit = parsed
	}

	commits, err := ga.Log(r.Context(), limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if commits == nil {
		commits = []gitadapter.CommitInfo{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"commits":   commits,
		"count":     len(commits),
		"source_id": sourceID,
	})
}

func (s *Server) handleGitDiff(w http.ResponseWriter, r *http.Request) {
	ga, sourceID, ok := s.getGitAdapter(w, r)
	if !ok {
		return
	}

	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	if from == "" || to == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing required query parameters 'from' and 'to'"})
		return
	}

	diff, err := ga.Diff(r.Context(), from, to)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"diff":      diff,
		"from":      from,
		"to":        to,
		"source_id": sourceID,
	})
}
