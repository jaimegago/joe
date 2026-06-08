package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	githubadapter "github.com/jaimegago/joe/internal/adapters/github"
	gitlabadapter "github.com/jaimegago/joe/internal/adapters/gitlab"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/review"
)

// registerReviewRoutes registers code review routes.
func (s *Server) registerReviewRoutes(mux *http.ServeMux, prefix string) {
	h := &reviewHandler{server: s}

	// Webhook receivers.
	mux.HandleFunc("POST "+prefix+"/webhooks/github", h.handleGitHubWebhook)
	mux.HandleFunc("POST "+prefix+"/webhooks/gitlab", h.handleGitLabWebhook)

	// Review job management.
	mux.HandleFunc("POST "+prefix+"/reviews", h.handleEnqueueReview)
	mux.HandleFunc("GET "+prefix+"/reviews", h.handleListReviews)
	mux.HandleFunc("GET "+prefix+"/reviews/{id}", h.handleGetReview)

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

type reviewHandler struct{ server *Server }

// --- Webhook receivers ---

func (h *reviewHandler) handleGitHubWebhook(w http.ResponseWriter, r *http.Request) {
	if h.server.services.Review == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "review service not available")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeBadRequest(w, err, "read webhook body", "failed to read request body")
		return
	}

	// The component_id can be passed as a query param or X-Joe-Source-Id header.
	sourceID := r.URL.Query().Get("component_id")
	if sourceID == "" {
		sourceID = r.Header.Get("X-Joe-Source-Id")
	}

	// Validate HMAC signature if the adapter has a secret configured.
	if sourceID != "" {
		if secret, err := h.server.accessor.GitHubWebhookSecret(sourceID); err == nil {
			if secret != "" && !verifyGitHubSignature(body, secret, r.Header.Get("X-Hub-Signature-256")) {
				writeError(w, http.StatusUnauthorized, "unauthorized", "invalid webhook signature")
				return
			}
		}
	}

	// Only process PR events.
	event := r.Header.Get("X-GitHub-Event")
	if event != "pull_request" {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored", "reason": "not a pull_request event"})
		return
	}

	var payload struct {
		Action      string `json:"action"`
		PullRequest struct {
			Number int `json:"number"`
			Head   struct {
				SHA string `json:"sha"`
			} `json:"head"`
		} `json:"pull_request"`
		Repository struct {
			Name  string `json:"name"`
			Owner struct {
				Login string `json:"login"`
			} `json:"owner"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		writeBadRequest(w, err, "parse webhook", "invalid JSON body")
		return
	}

	// Only review on opened / synchronize.
	if payload.Action != "opened" && payload.Action != "synchronize" {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored", "reason": "action not reviewed"})
		return
	}

	owner := payload.Repository.Owner.Login
	repo := payload.Repository.Name
	prNumber := payload.PullRequest.Number
	headSHA := payload.PullRequest.Head.SHA

	if owner == "" || repo == "" || prNumber == 0 || headSHA == "" || sourceID == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest,
			"component_id, owner, repo, pr_number, and head_sha are required")
		return
	}

	job := &review.ReviewJob{
		Platform:    review.PlatformGitHub,
		ComponentID: sourceID,
		Owner:       owner,
		Repo:        repo,
		PRNumber:    prNumber,
		HeadSHA:     headSHA,
		EventID:     review.BuildEventID(review.PlatformGitHub, owner, repo, prNumber, headSHA),
	}

	created, err := h.server.services.Review.Enqueue(r.Context(), job)
	if errors.Is(err, review.ErrDuplicateEvent) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "skipped", "reason": "duplicate event"})
		return
	}
	if err != nil {
		writeInternalError(w, err, "enqueue review job")
		return
	}

	// Dispatch in background if a ReviewAgent is configured.
	if h.server.services.ReviewAgent != nil {
		agent := h.server.services.ReviewAgent
		go func() {
			if runErr := agent.Run(r.Context(), created); runErr != nil {
				// Logged inside agent.Run.
				_ = runErr
			}
		}()
	}

	writeJSON(w, http.StatusAccepted, created)
}

func (h *reviewHandler) handleGitLabWebhook(w http.ResponseWriter, r *http.Request) {
	if h.server.services.Review == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "review service not available")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeBadRequest(w, err, "read webhook body", "failed to read request body")
		return
	}

	sourceID := r.URL.Query().Get("component_id")
	if sourceID == "" {
		sourceID = r.Header.Get("X-Joe-Source-Id")
	}

	// Validate X-Gitlab-Token if secret is configured.
	if sourceID != "" {
		if secret, err := h.server.accessor.GitLabWebhookSecret(sourceID); err == nil {
			if secret != "" && r.Header.Get("X-Gitlab-Token") != secret {
				writeError(w, http.StatusUnauthorized, "unauthorized", "invalid webhook token")
				return
			}
		}
	}

	// Only process merge_requests events.
	event := r.Header.Get("X-Gitlab-Event")
	if event != "Merge Request Hook" {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored", "reason": "not a Merge Request Hook"})
		return
	}

	var payload struct {
		ObjectKind       string `json:"object_kind"`
		ObjectAttributes struct {
			IID        int `json:"iid"`
			LastCommit struct {
				ID string `json:"id"`
			} `json:"last_commit"`
			Action       string `json:"action"`
			TargetBranch string `json:"target_branch"`
		} `json:"object_attributes"`
		Project struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"project"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		writeBadRequest(w, err, "parse webhook", "invalid JSON body")
		return
	}

	action := payload.ObjectAttributes.Action
	if action != "open" && action != "update" {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored", "reason": "action not reviewed"})
		return
	}

	projectID := strconv.Itoa(payload.Project.ID)
	iid := payload.ObjectAttributes.IID
	headSHA := payload.ObjectAttributes.LastCommit.ID

	if projectID == "0" || iid == 0 || headSHA == "" || sourceID == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest,
			"component_id, project_id, mr_iid, and head_sha are required")
		return
	}

	job := &review.ReviewJob{
		Platform:    review.PlatformGitLab,
		ComponentID: sourceID,
		Owner:       projectID, // GitLab uses projectID in the owner field
		Repo:        payload.Project.Name,
		PRNumber:    iid,
		HeadSHA:     headSHA,
		EventID:     review.BuildEventID(review.PlatformGitLab, projectID, payload.Project.Name, iid, headSHA),
	}

	created, err := h.server.services.Review.Enqueue(r.Context(), job)
	if errors.Is(err, review.ErrDuplicateEvent) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "skipped", "reason": "duplicate event"})
		return
	}
	if err != nil {
		writeInternalError(w, err, "enqueue review job")
		return
	}

	if h.server.services.ReviewAgent != nil {
		agent := h.server.services.ReviewAgent
		go func() {
			_ = agent.Run(r.Context(), created)
		}()
	}

	writeJSON(w, http.StatusAccepted, created)
}

// --- Review job management ---

func (h *reviewHandler) handleEnqueueReview(w http.ResponseWriter, r *http.Request) {
	if h.server.services.Review == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "review service not available")
		return
	}

	var job review.ReviewJob
	if err := json.NewDecoder(r.Body).Decode(&job); err != nil {
		writeBadRequest(w, err, "parse review job", "invalid JSON body")
		return
	}
	if job.ComponentID == "" || job.Owner == "" || job.Repo == "" || job.PRNumber == 0 || job.HeadSHA == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest,
			"component_id, owner, repo, pr_number, and head_sha are required")
		return
	}
	if job.Platform == "" {
		job.Platform = review.PlatformGitHub
	}
	if job.EventID == "" {
		job.EventID = review.BuildEventID(job.Platform, job.Owner, job.Repo, job.PRNumber, job.HeadSHA)
	}

	created, err := h.server.services.Review.Enqueue(r.Context(), &job)
	if errors.Is(err, review.ErrDuplicateEvent) {
		writeError(w, http.StatusConflict, "duplicate_event", "review job already exists for this event")
		return
	}
	if err != nil {
		writeInternalError(w, err, "enqueue review job")
		return
	}

	if h.server.services.ReviewAgent != nil {
		agent := h.server.services.ReviewAgent
		go func() {
			_ = agent.Run(r.Context(), created)
		}()
	}

	writeJSON(w, http.StatusCreated, created)
}

func (h *reviewHandler) handleListReviews(w http.ResponseWriter, r *http.Request) {
	if h.server.services.Review == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "review service not available")
		return
	}

	q := r.URL.Query()
	platform := review.Platform(q.Get("platform"))
	status := review.JobStatus(q.Get("status"))
	limit := 50
	if l := q.Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	jobs, err := h.server.services.Review.List(r.Context(), platform, status, limit)
	if err != nil {
		writeInternalError(w, err, "list reviews")
		return
	}
	if jobs == nil {
		jobs = []*review.ReviewJob{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs, "count": len(jobs)})
}

func (h *reviewHandler) handleGetReview(w http.ResponseWriter, r *http.Request) {
	if h.server.services.Review == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeServiceUnavailable, "review service not available")
		return
	}

	id := r.PathValue("id")
	job, err := h.server.services.Review.Get(r.Context(), id)
	if errors.Is(err, review.ErrNotFound) {
		writeError(w, http.StatusNotFound, errorCodeNotFound, "review job not found", map[string]any{"id": id})
		return
	}
	if err != nil {
		writeInternalError(w, err, "get review job")
		return
	}
	writeJSON(w, http.StatusOK, job)
}

// --- GitHub PR operations ---

func (h *reviewHandler) handleGitHubGetPR(w http.ResponseWriter, r *http.Request) {
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

func (h *reviewHandler) handleGitHubGetPRDiff(w http.ResponseWriter, r *http.Request) {
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

func (h *reviewHandler) handleGitHubListPRs(w http.ResponseWriter, r *http.Request) {
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

func (h *reviewHandler) handleGitHubPostComment(w http.ResponseWriter, r *http.Request) {
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

func (h *reviewHandler) handleGitHubRequestChanges(w http.ResponseWriter, r *http.Request) {
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

func (h *reviewHandler) handleGitLabGetMR(w http.ResponseWriter, r *http.Request) {
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

func (h *reviewHandler) handleGitLabGetMRDiff(w http.ResponseWriter, r *http.Request) {
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

func (h *reviewHandler) handleGitLabListMRs(w http.ResponseWriter, r *http.Request) {
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

func (h *reviewHandler) handleGitLabPostNote(w http.ResponseWriter, r *http.Request) {
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

// --- Helpers ---

// verifyGitHubSignature validates the X-Hub-Signature-256 header.
func verifyGitHubSignature(body []byte, secret, sigHeader string) bool {
	if len(sigHeader) < 7 || sigHeader[:7] != "sha256=" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(sigHeader), []byte(expected))
}
