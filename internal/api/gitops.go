package api

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	argocdadapter "github.com/jaimegago/joe/internal/adapters/gitops/argocd"
	terraformadapter "github.com/jaimegago/joe/internal/adapters/iac/terraform"
	helmadapter "github.com/jaimegago/joe/internal/adapters/packaging/helm"
	"github.com/jaimegago/joe/internal/rbac"
)

// =========================
// Argo CD handlers
// =========================

// handleArgoCDApps lists Argo CD applications.
// GET /api/v1/argocd/{componentID}/apps?project=
func (s *Server) handleArgoCDApps(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("componentID")
	project := r.URL.Query().Get("project")

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	apps, err := s.accessor.ArgoCDApps(r.Context(), principal, sourceID, project)
	s.services.Metrics.RecordAdapterCall(r.Context(), "argocd", "apps", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "Argo CD") {
			return
		}
		writeInternalError(w, err, "argocd apps")
		return
	}

	if apps == nil {
		apps = []argocdadapter.App{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"apps":         apps,
		"count":        len(apps),
		"component_id": sourceID,
	})
}

// handleArgoCDGetApp returns full details for one Argo CD application.
// GET /api/v1/argocd/{componentID}/apps/{name}
func (s *Server) handleArgoCDGetApp(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("componentID")
	name := r.PathValue("name")

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	detail, err := s.accessor.ArgoCDGetApp(r.Context(), principal, sourceID, name)
	s.services.Metrics.RecordAdapterCall(r.Context(), "argocd", "get_app", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "Argo CD") {
			return
		}
		writeInternalError(w, err, "argocd get app")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"detail":       detail,
		"component_id": sourceID,
	})
}

// handleArgoCDDiff returns the sync diff for an Argo CD application.
// GET /api/v1/argocd/{componentID}/apps/{name}/diff
func (s *Server) handleArgoCDDiff(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("componentID")
	name := r.PathValue("name")

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	diff, err := s.accessor.ArgoCDGetDiff(r.Context(), principal, sourceID, name)
	s.services.Metrics.RecordAdapterCall(r.Context(), "argocd", "diff", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "Argo CD") {
			return
		}
		writeInternalError(w, err, "argocd diff")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"diff":         diff,
		"component_id": sourceID,
	})
}

// handleArgoCDHistory returns the sync history for an Argo CD application.
// GET /api/v1/argocd/{componentID}/apps/{name}/history?limit=10
func (s *Server) handleArgoCDHistory(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("componentID")
	name := r.PathValue("name")

	limit := 10
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	history, err := s.accessor.ArgoCDGetHistory(r.Context(), principal, sourceID, name, limit)
	s.services.Metrics.RecordAdapterCall(r.Context(), "argocd", "history", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "Argo CD") {
			return
		}
		writeInternalError(w, err, "argocd history")
		return
	}

	if history == nil {
		history = []argocdadapter.SyncOperation{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"history":      history,
		"count":        len(history),
		"component_id": sourceID,
	})
}

// =========================
// Terraform handlers
// =========================

// handleTerraformResources lists managed resources from a Terraform state.
// GET /api/v1/terraform/{componentID}/state?type=aws_instance
func (s *Server) handleTerraformResources(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("componentID")
	resourceType := r.URL.Query().Get("type")

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	resources, err := s.accessor.TerraformResources(r.Context(), principal, sourceID, resourceType)
	s.services.Metrics.RecordAdapterCall(r.Context(), "terraform", "resources", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "Terraform") {
			return
		}
		writeInternalError(w, err, "terraform resources")
		return
	}

	if resources == nil {
		resources = []terraformadapter.Resource{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"resources":    resources,
		"count":        len(resources),
		"component_id": sourceID,
	})
}

// handleTerraformGetResource returns details for a specific Terraform resource.
// GET /api/v1/terraform/{componentID}/state/resource?address=aws_instance.web
func (s *Server) handleTerraformGetResource(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("componentID")
	address := r.URL.Query().Get("address")
	if address == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing required parameter: address", map[string]any{
			"param": "address",
		})
		return
	}

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	resource, err := s.accessor.TerraformGetResource(r.Context(), principal, sourceID, address)
	s.services.Metrics.RecordAdapterCall(r.Context(), "terraform", "get_resource", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "Terraform") {
			return
		}
		writeInternalError(w, err, "terraform get resource")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"resource":     resource,
		"component_id": sourceID,
	})
}

// handleTerraformOutputs lists output values from a Terraform state.
// GET /api/v1/terraform/{componentID}/outputs
func (s *Server) handleTerraformOutputs(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("componentID")

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	outputs, err := s.accessor.TerraformOutputs(r.Context(), principal, sourceID)
	s.services.Metrics.RecordAdapterCall(r.Context(), "terraform", "outputs", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "Terraform") {
			return
		}
		writeInternalError(w, err, "terraform outputs")
		return
	}

	if outputs == nil {
		outputs = map[string]terraformadapter.Output{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"outputs":      outputs,
		"count":        len(outputs),
		"component_id": sourceID,
	})
}

// =========================
// Helm handlers
// =========================

// handleHelmReleases lists Helm releases.
// GET /api/v1/helm/{componentID}/releases?namespace=production
func (s *Server) handleHelmReleases(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("componentID")
	namespace := r.URL.Query().Get("namespace")

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	releases, err := s.accessor.HelmReleases(r.Context(), principal, sourceID, namespace)
	s.services.Metrics.RecordAdapterCall(r.Context(), "helm", "releases", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "Helm") {
			return
		}
		writeInternalError(w, err, "helm releases")
		return
	}

	if releases == nil {
		releases = []helmadapter.Release{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"releases":     releases,
		"count":        len(releases),
		"component_id": sourceID,
	})
}

// handleHelmGetRelease returns details for one Helm release.
// GET /api/v1/helm/{componentID}/releases/{namespace}/{name}
func (s *Server) handleHelmGetRelease(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("componentID")
	namespace := r.PathValue("namespace")
	name := r.PathValue("name")

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	detail, err := s.accessor.HelmGetRelease(r.Context(), principal, sourceID, namespace, name)
	s.services.Metrics.RecordAdapterCall(r.Context(), "helm", "get_release", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "Helm") {
			return
		}
		writeInternalError(w, err, "helm get release")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"detail":       detail,
		"component_id": sourceID,
	})
}

// handleHelmHistory returns the revision history for a Helm release.
// GET /api/v1/helm/{componentID}/releases/{namespace}/{name}/history?limit=10
func (s *Server) handleHelmHistory(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("componentID")
	namespace := r.PathValue("namespace")
	name := r.PathValue("name")

	limit := 10
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	history, err := s.accessor.HelmHistory(r.Context(), principal, sourceID, namespace, name, limit)
	s.services.Metrics.RecordAdapterCall(r.Context(), "helm", "history", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "Helm") {
			return
		}
		writeInternalError(w, err, "helm history")
		return
	}

	if history == nil {
		history = []helmadapter.RevisionEntry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"history":      history,
		"count":        len(history),
		"component_id": sourceID,
	})
}

// registerGitOpsRoutes registers GitOps, CD, and IaC routes.
func (s *Server) registerGitOpsRoutes(mux *http.ServeMux, prefix string) {
	h := &gitOpsHandler{server: s}

	// Argo CD
	mux.HandleFunc(fmt.Sprintf("GET %s/argocd/{componentID}/apps", prefix), h.handleArgoCDApps)
	mux.HandleFunc(fmt.Sprintf("GET %s/argocd/{componentID}/apps/{name}", prefix), h.handleArgoCDGetApp)
	mux.HandleFunc(fmt.Sprintf("GET %s/argocd/{componentID}/apps/{name}/diff", prefix), h.handleArgoCDDiff)
	mux.HandleFunc(fmt.Sprintf("GET %s/argocd/{componentID}/apps/{name}/history", prefix), h.handleArgoCDHistory)

	// Terraform
	mux.HandleFunc(fmt.Sprintf("GET %s/terraform/{componentID}/state", prefix), h.handleTerraformResources)
	mux.HandleFunc(fmt.Sprintf("GET %s/terraform/{componentID}/state/resource", prefix), h.handleTerraformGetResource)
	mux.HandleFunc(fmt.Sprintf("GET %s/terraform/{componentID}/outputs", prefix), h.handleTerraformOutputs)

	// Helm
	mux.HandleFunc(fmt.Sprintf("GET %s/helm/{componentID}/releases", prefix), h.handleHelmReleases)
	mux.HandleFunc(fmt.Sprintf("GET %s/helm/{componentID}/releases/{namespace}/{name}", prefix), h.handleHelmGetRelease)
	mux.HandleFunc(fmt.Sprintf("GET %s/helm/{componentID}/releases/{namespace}/{name}/history", prefix), h.handleHelmHistory)
}

// gitOpsHandler delegates to Server GitOps methods.
type gitOpsHandler struct{ server *Server }

func (h *gitOpsHandler) handleArgoCDApps(w http.ResponseWriter, r *http.Request) {
	h.server.handleArgoCDApps(w, r)
}
func (h *gitOpsHandler) handleArgoCDGetApp(w http.ResponseWriter, r *http.Request) {
	h.server.handleArgoCDGetApp(w, r)
}
func (h *gitOpsHandler) handleArgoCDDiff(w http.ResponseWriter, r *http.Request) {
	h.server.handleArgoCDDiff(w, r)
}
func (h *gitOpsHandler) handleArgoCDHistory(w http.ResponseWriter, r *http.Request) {
	h.server.handleArgoCDHistory(w, r)
}
func (h *gitOpsHandler) handleTerraformResources(w http.ResponseWriter, r *http.Request) {
	h.server.handleTerraformResources(w, r)
}
func (h *gitOpsHandler) handleTerraformGetResource(w http.ResponseWriter, r *http.Request) {
	h.server.handleTerraformGetResource(w, r)
}
func (h *gitOpsHandler) handleTerraformOutputs(w http.ResponseWriter, r *http.Request) {
	h.server.handleTerraformOutputs(w, r)
}
func (h *gitOpsHandler) handleHelmReleases(w http.ResponseWriter, r *http.Request) {
	h.server.handleHelmReleases(w, r)
}
func (h *gitOpsHandler) handleHelmGetRelease(w http.ResponseWriter, r *http.Request) {
	h.server.handleHelmGetRelease(w, r)
}
func (h *gitOpsHandler) handleHelmHistory(w http.ResponseWriter, r *http.Request) {
	h.server.handleHelmHistory(w, r)
}
