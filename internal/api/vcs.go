package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	githubadapter "github.com/jaimegago/joe/internal/adapters/github"
	gitlabadapter "github.com/jaimegago/joe/internal/adapters/gitlab"
	"github.com/jaimegago/joe/internal/rbac"
)

// registerVCSRoutes registers GitHub PR / GitLab MR operation routes. These
// back the code-review core tools (internal/tools/core) reached via the agentic
// loop / MCP path.
func (s *Server) registerVCSRoutes(mux *http.ServeMux, prefix string) {
	h := &vcsHandler{server: s}

	// GitHub PR operations (T1: observe).
	mux.HandleFunc("GET "+prefix+"/github/{componentID}/pulls/{number}", h.handleGitHubGetPR)
	mux.HandleFunc("GET "+prefix+"/github/{componentID}/pulls/{number}/diff", h.handleGitHubGetPRDiff)
	mux.HandleFunc("GET "+prefix+"/github/{componentID}/pulls", h.handleGitHubListPRs)
	// GitHub PR mutations (T2/T3 via tool executor — guarded by safety policy).
	mux.HandleFunc("POST "+prefix+"/github/{componentID}/pulls/{number}/comments", h.handleGitHubPostComment)
	mux.HandleFunc("POST "+prefix+"/github/{componentID}/pulls/{number}/reviews", h.handleGitHubRequestChanges)

	// GitLab MR operations (T1: observe).
	mux.HandleFunc("GET "+prefix+"/gitlab/{componentID}/projects/{projectID}/mrs/{iid}", h.handleGitLabGetMR)
	mux.HandleFunc("GET "+prefix+"/gitlab/{componentID}/projects/{projectID}/mrs/{iid}/diff", h.handleGitLabGetMRDiff)
	mux.HandleFunc("GET "+prefix+"/gitlab/{componentID}/projects/{projectID}/mrs", h.handleGitLabListMRs)
	// GitLab MR mutations (T2 — guarded by safety policy).
	mux.HandleFunc("POST "+prefix+"/gitlab/{componentID}/projects/{projectID}/mrs/{iid}/notes", h.handleGitLabPostNote)
}

type vcsHandler struct{ server *Server }

// --- GitHub PR operations ---

func (h *vcsHandler) handleGitHubGetPR(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("componentID")
	number, err := strconv.Atoi(r.PathValue("number"))
	if err != nil || number <= 0 {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "pr number must be a positive integer")
		return
	}
	owner := r.URL.Query().Get("owner")
	repo := r.URL.Query().Get("repo")
	if owner == "" || repo == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "owner and repo query parameters are required")
		return
	}

	principal := rbac.PrincipalFromContext(r.Context())
	pr, err := h.server.accessor.GitHubGetPR(r.Context(), principal, sourceID, owner, repo, number)
	if err != nil {
		if handleAccessError(w, err, sourceID, "github") {
			return
		}
		writeInternalError(w, err, "get github pr")
		return
	}
	writeJSON(w, http.StatusOK, pr)
}

func (h *vcsHandler) handleGitHubGetPRDiff(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("componentID")
	number, err := strconv.Atoi(r.PathValue("number"))
	if err != nil || number <= 0 {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "pr number must be a positive integer")
		return
	}
	owner := r.URL.Query().Get("owner")
	repo := r.URL.Query().Get("repo")
	if owner == "" || repo == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "owner and repo query parameters are required")
		return
	}

	principal := rbac.PrincipalFromContext(r.Context())
	diff, err := h.server.accessor.GitHubGetPRDiff(r.Context(), principal, sourceID, owner, repo, number)
	if err != nil {
		if handleAccessError(w, err, sourceID, "github") {
			return
		}
		writeInternalError(w, err, "get github pr diff")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"diff": diff})
}

func (h *vcsHandler) handleGitHubListPRs(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("componentID")
	owner := r.URL.Query().Get("owner")
	repo := r.URL.Query().Get("repo")
	if owner == "" || repo == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "owner and repo query parameters are required")
		return
	}
	state := r.URL.Query().Get("state")

	principal := rbac.PrincipalFromContext(r.Context())
	prs, err := h.server.accessor.GitHubListPRs(r.Context(), principal, sourceID, owner, repo, state)
	if err != nil {
		if handleAccessError(w, err, sourceID, "github") {
			return
		}
		writeInternalError(w, err, "list github prs")
		return
	}
	if prs == nil {
		prs = []*githubadapter.PRInfo{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"prs": prs, "count": len(prs)})
}

func (h *vcsHandler) handleGitHubPostComment(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("componentID")
	number, err := strconv.Atoi(r.PathValue("number"))
	if err != nil || number <= 0 {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "pr number must be a positive integer")
		return
	}

	var payload struct {
		Owner string `json:"owner"`
		Repo  string `json:"repo"`
		Body  string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeBadRequest(w, err, "parse body", "invalid JSON body")
		return
	}
	if payload.Owner == "" || payload.Repo == "" || payload.Body == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "owner, repo, and body are required")
		return
	}

	principal := rbac.PrincipalFromContext(r.Context())
	if err := h.server.accessor.GitHubPostComment(r.Context(), principal, sourceID, payload.Owner, payload.Repo, number, payload.Body); err != nil {
		if handleAccessError(w, err, sourceID, "github") {
			return
		}
		writeInternalError(w, err, "post github comment")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"posted": true, "pr": number})
}

func (h *vcsHandler) handleGitHubRequestChanges(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("componentID")
	number, err := strconv.Atoi(r.PathValue("number"))
	if err != nil || number <= 0 {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "pr number must be a positive integer")
		return
	}

	var payload struct {
		Owner string `json:"owner"`
		Repo  string `json:"repo"`
		Body  string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeBadRequest(w, err, "parse body", "invalid JSON body")
		return
	}
	if payload.Owner == "" || payload.Repo == "" || payload.Body == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "owner, repo, and body are required")
		return
	}

	principal := rbac.PrincipalFromContext(r.Context())
	if err := h.server.accessor.GitHubRequestChanges(r.Context(), principal, sourceID, payload.Owner, payload.Repo, number, payload.Body); err != nil {
		if handleAccessError(w, err, sourceID, "github") {
			return
		}
		writeInternalError(w, err, "request github changes")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"changes_requested": true, "pr": number})
}

// --- GitLab MR operations ---

func (h *vcsHandler) handleGitLabGetMR(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("componentID")
	projectID := r.PathValue("projectID")
	iid, err := strconv.Atoi(r.PathValue("iid"))
	if err != nil || iid <= 0 {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "mr iid must be a positive integer")
		return
	}

	principal := rbac.PrincipalFromContext(r.Context())
	mr, err := h.server.accessor.GitLabGetMR(r.Context(), principal, sourceID, projectID, iid)
	if err != nil {
		if handleAccessError(w, err, sourceID, "gitlab") {
			return
		}
		writeInternalError(w, err, "get gitlab mr")
		return
	}
	writeJSON(w, http.StatusOK, mr)
}

func (h *vcsHandler) handleGitLabGetMRDiff(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("componentID")
	projectID := r.PathValue("projectID")
	iid, err := strconv.Atoi(r.PathValue("iid"))
	if err != nil || iid <= 0 {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "mr iid must be a positive integer")
		return
	}

	principal := rbac.PrincipalFromContext(r.Context())
	diff, err := h.server.accessor.GitLabGetMRDiff(r.Context(), principal, sourceID, projectID, iid)
	if err != nil {
		if handleAccessError(w, err, sourceID, "gitlab") {
			return
		}
		writeInternalError(w, err, "get gitlab mr diff")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"diff": diff})
}

func (h *vcsHandler) handleGitLabListMRs(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("componentID")
	projectID := r.PathValue("projectID")
	state := r.URL.Query().Get("state")

	principal := rbac.PrincipalFromContext(r.Context())
	mrs, err := h.server.accessor.GitLabListMRs(r.Context(), principal, sourceID, projectID, state)
	if err != nil {
		if handleAccessError(w, err, sourceID, "gitlab") {
			return
		}
		writeInternalError(w, err, "list gitlab mrs")
		return
	}
	if mrs == nil {
		mrs = []*gitlabadapter.MRInfo{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"mrs": mrs, "count": len(mrs)})
}

func (h *vcsHandler) handleGitLabPostNote(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("componentID")
	projectID := r.PathValue("projectID")
	iid, err := strconv.Atoi(r.PathValue("iid"))
	if err != nil || iid <= 0 {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "mr iid must be a positive integer")
		return
	}

	var payload struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeBadRequest(w, err, "parse body", "invalid JSON body")
		return
	}
	if payload.Body == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "body is required")
		return
	}

	principal := rbac.PrincipalFromContext(r.Context())
	if err := h.server.accessor.GitLabPostNote(r.Context(), principal, sourceID, projectID, iid, payload.Body); err != nil {
		if handleAccessError(w, err, sourceID, "gitlab") {
			return
		}
		writeInternalError(w, err, "post gitlab note")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"posted": true, "mr_iid": iid})
}
