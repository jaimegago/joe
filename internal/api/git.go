package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/jaimegago/joe/internal/adapters/git"
	"github.com/jaimegago/joe/internal/constants"
	"github.com/jaimegago/joe/internal/rbac"
)

func (s *Server) handleGitReadFile(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")

	path := r.URL.Query().Get("path")
	if path == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing required query parameter 'path'", map[string]any{
			"param": "path",
		})
		return
	}

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	content, err := s.accessor.GitReadFile(r.Context(), principal, sourceID, path)
	s.services.Metrics.RecordAdapterCall(r.Context(), "git", "read_file", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "git") {
			return
		}
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

	dir := r.URL.Query().Get("dir")

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	files, err := s.accessor.GitListFiles(r.Context(), principal, sourceID, dir)
	s.services.Metrics.RecordAdapterCall(r.Context(), "git", "list_files", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "git") {
			return
		}
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

	limit := constants.DefaultGitLogLimit
	if l := r.URL.Query().Get("limit"); l != "" {
		parsed, err := strconv.Atoi(l)
		if err != nil || parsed <= 0 {
			writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "limit must be a positive integer", map[string]any{
				"param": "limit",
				"value": l,
			})
			return
		}
		limit = parsed
	}

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	commits, err := s.accessor.GitLog(r.Context(), principal, sourceID, limit)
	s.services.Metrics.RecordAdapterCall(r.Context(), "git", "log", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "git") {
			return
		}
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

	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	if from == "" || to == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing required query parameters 'from' and 'to'", map[string]any{
			"params": []string{"from", "to"},
		})
		return
	}

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	diff, err := s.accessor.GitDiff(r.Context(), principal, sourceID, from, to)
	s.services.Metrics.RecordAdapterCall(r.Context(), "git", "diff", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "git") {
			return
		}
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
