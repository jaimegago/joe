package api

import (
	"net/http"
	"strconv"

	"github.com/jaimegago/joe/internal/adapters/git"
	"github.com/jaimegago/joe/internal/constants"
)

func (s *Server) handleGitReadFile(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")
	ga, err := s.getGitAdapter(sourceID)
	if handleAdapterLookupError(w, err, sourceID, "git") {
		return
	}

	path := r.URL.Query().Get("path")
	if path == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing required query parameter 'path'")
		return
	}

	content, err := ga.ReadFile(r.Context(), path)
	if err != nil {
		writeInternalError(w, err, "git read file")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"content":   content,
		"path":      path,
		"source_id": sourceID,
	})
}

func (s *Server) handleGitListFiles(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")
	ga, err := s.getGitAdapter(sourceID)
	if handleAdapterLookupError(w, err, sourceID, "git") {
		return
	}

	dir := r.URL.Query().Get("dir")

	files, err := ga.ListFiles(r.Context(), dir)
	if err != nil {
		writeInternalError(w, err, "git list files")
		return
	}

	if files == nil {
		files = []git.FileInfo{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"files":     files,
		"count":     len(files),
		"dir":       dir,
		"source_id": sourceID,
	})
}

func (s *Server) handleGitLog(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")
	ga, err := s.getGitAdapter(sourceID)
	if handleAdapterLookupError(w, err, sourceID, "git") {
		return
	}

	limit := constants.DefaultGitLogLimit
	if l := r.URL.Query().Get("limit"); l != "" {
		parsed, err := strconv.Atoi(l)
		if err != nil || parsed <= 0 {
			writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "limit must be a positive integer")
			return
		}
		limit = parsed
	}

	commits, err := ga.Log(r.Context(), limit)
	if err != nil {
		writeInternalError(w, err, "git log")
		return
	}

	if commits == nil {
		commits = []git.CommitInfo{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"commits":   commits,
		"count":     len(commits),
		"source_id": sourceID,
	})
}

func (s *Server) handleGitDiff(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")
	ga, err := s.getGitAdapter(sourceID)
	if handleAdapterLookupError(w, err, sourceID, "git") {
		return
	}

	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	if from == "" || to == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing required query parameters 'from' and 'to'")
		return
	}

	diff, err := ga.Diff(r.Context(), from, to)
	if err != nil {
		writeInternalError(w, err, "git diff")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"diff":      diff,
		"from":      from,
		"to":        to,
		"source_id": sourceID,
	})
}
