package api

import (
	"fmt"
	"net/http"
	"time"

	artifactoryadapter "github.com/jaimegago/joe/internal/adapters/registry/artifactory"
	ecradapter "github.com/jaimegago/joe/internal/adapters/registry/ecr"
	"github.com/jaimegago/joe/internal/rbac"
)

// registerRegistryRoutes registers artifact registry API routes.
func (s *Server) registerRegistryRoutes(mux *http.ServeMux, prefix string) {
	// OCI registry routes (DockerHub, GHCR, Harbor, Quay).
	mux.HandleFunc(fmt.Sprintf("GET %s/registry/oci/{sourceID}/repos", prefix), s.handleOCIListRepos)
	mux.HandleFunc(fmt.Sprintf("GET %s/registry/oci/{sourceID}/repos/{repo}/tags", prefix), s.handleOCIListTags)
	mux.HandleFunc(fmt.Sprintf("GET %s/registry/oci/{sourceID}/repos/{repo}/manifest", prefix), s.handleOCIGetManifest)

	// JFrog Artifactory routes.
	mux.HandleFunc(fmt.Sprintf("GET %s/registry/artifactory/{sourceID}/repos", prefix), s.handleArtifactoryListRepos)
	mux.HandleFunc(fmt.Sprintf("GET %s/registry/artifactory/{sourceID}/repos/{repo}/tags", prefix), s.handleArtifactoryListTags)
	mux.HandleFunc(fmt.Sprintf("GET %s/registry/artifactory/{sourceID}/repos/{repo}/artifact", prefix), s.handleArtifactoryGetArtifact)

	// AWS ECR routes.
	mux.HandleFunc(fmt.Sprintf("GET %s/registry/ecr/{sourceID}/repos", prefix), s.handleECRListRepos)
	mux.HandleFunc(fmt.Sprintf("GET %s/registry/ecr/{sourceID}/repos/{repo}/images", prefix), s.handleECRListImages)
	mux.HandleFunc(fmt.Sprintf("GET %s/registry/ecr/{sourceID}/repos/{repo}/images/{tag}", prefix), s.handleECRGetImage)
}

// --- OCI handlers ---

// handleOCIListRepos lists all repositories in an OCI-compatible registry.
// GET /api/v1/registry/oci/{sourceID}/repos
func (s *Server) handleOCIListRepos(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	repos, err := s.accessor.OCIListRepositories(r.Context(), principal, sourceID)
	s.services.Metrics.RecordAdapterCall(r.Context(), "oci_registry", "list_repos", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "OCI Registry") {
			return
		}
		writeInternalError(w, err, "oci list repos")
		return
	}

	if repos == nil {
		repos = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"repositories": repos,
		"count":        len(repos),
		"source_id":    sourceID,
	})
}

// handleOCIListTags lists tags for a repository in an OCI-compatible registry.
// GET /api/v1/registry/oci/{sourceID}/repos/{repo}/tags
func (s *Server) handleOCIListTags(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")
	repo := r.PathValue("repo")

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	tags, err := s.accessor.OCIListTags(r.Context(), principal, sourceID, repo)
	s.services.Metrics.RecordAdapterCall(r.Context(), "oci_registry", "list_tags", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "OCI Registry") {
			return
		}
		writeInternalError(w, err, "oci list tags")
		return
	}

	if tags == nil {
		tags = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tags":      tags,
		"count":     len(tags),
		"repo":      repo,
		"source_id": sourceID,
	})
}

// handleOCIGetManifest retrieves an image manifest from an OCI-compatible registry.
// GET /api/v1/registry/oci/{sourceID}/repos/{repo}/manifest?reference=<tag|digest>
func (s *Server) handleOCIGetManifest(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")
	repo := r.PathValue("repo")
	reference := r.URL.Query().Get("reference")

	if reference == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing required query parameter: reference", map[string]any{
			"param": "reference",
		})
		return
	}

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	manifest, err := s.accessor.OCIGetManifest(r.Context(), principal, sourceID, repo, reference)
	s.services.Metrics.RecordAdapterCall(r.Context(), "oci_registry", "get_manifest", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "OCI Registry") {
			return
		}
		writeInternalError(w, err, "oci get manifest")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"manifest":  manifest,
		"repo":      repo,
		"reference": reference,
		"source_id": sourceID,
	})
}

// --- Artifactory handlers ---

// handleArtifactoryListRepos lists Docker/Helm repositories in Artifactory.
// GET /api/v1/registry/artifactory/{sourceID}/repos
func (s *Server) handleArtifactoryListRepos(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	repos, err := s.accessor.ArtifactoryListRepositories(r.Context(), principal, sourceID)
	s.services.Metrics.RecordAdapterCall(r.Context(), "artifactory", "list_repos", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "Artifactory") {
			return
		}
		writeInternalError(w, err, "artifactory list repos")
		return
	}

	if repos == nil {
		repos = []artifactoryadapter.Repository{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"repositories": repos,
		"count":        len(repos),
		"source_id":    sourceID,
	})
}

// handleArtifactoryListTags lists Docker image tags in an Artifactory repository.
// GET /api/v1/registry/artifactory/{sourceID}/repos/{repo}/tags?image=<name>
func (s *Server) handleArtifactoryListTags(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")
	repo := r.PathValue("repo")
	image := r.URL.Query().Get("image")

	if image == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing required query parameter: image", map[string]any{
			"param": "image",
		})
		return
	}

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	tags, err := s.accessor.ArtifactoryListDockerTags(r.Context(), principal, sourceID, repo, image)
	s.services.Metrics.RecordAdapterCall(r.Context(), "artifactory", "list_docker_tags", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "Artifactory") {
			return
		}
		writeInternalError(w, err, "artifactory list docker tags")
		return
	}

	if tags == nil {
		tags = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tags":      tags,
		"count":     len(tags),
		"repo":      repo,
		"image":     image,
		"source_id": sourceID,
	})
}

// handleArtifactoryGetArtifact retrieves artifact metadata from Artifactory.
// GET /api/v1/registry/artifactory/{sourceID}/repos/{repo}/artifact?path=<path>
func (s *Server) handleArtifactoryGetArtifact(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")
	repo := r.PathValue("repo")
	path := r.URL.Query().Get("path")

	if path == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing required query parameter: path", map[string]any{
			"param": "path",
		})
		return
	}

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	info, err := s.accessor.ArtifactoryGetArtifactInfo(r.Context(), principal, sourceID, repo, path)
	s.services.Metrics.RecordAdapterCall(r.Context(), "artifactory", "get_artifact_info", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "Artifactory") {
			return
		}
		writeInternalError(w, err, "artifactory get artifact info")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"artifact":  info,
		"repo":      repo,
		"path":      path,
		"source_id": sourceID,
	})
}

// --- ECR handlers ---

// handleECRListRepos lists ECR repositories.
// GET /api/v1/registry/ecr/{sourceID}/repos
func (s *Server) handleECRListRepos(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	repos, err := s.accessor.ECRListRepositories(r.Context(), principal, sourceID)
	s.services.Metrics.RecordAdapterCall(r.Context(), "ecr", "list_repos", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "ECR") {
			return
		}
		writeInternalError(w, err, "ecr list repos")
		return
	}

	if repos == nil {
		repos = []ecradapter.Repository{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"repositories": repos,
		"count":        len(repos),
		"source_id":    sourceID,
	})
}

// handleECRListImages lists images in an ECR repository.
// GET /api/v1/registry/ecr/{sourceID}/repos/{repo}/images
func (s *Server) handleECRListImages(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")
	repo := r.PathValue("repo")

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	images, err := s.accessor.ECRListImages(r.Context(), principal, sourceID, repo)
	s.services.Metrics.RecordAdapterCall(r.Context(), "ecr", "list_images", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "ECR") {
			return
		}
		writeInternalError(w, err, "ecr list images")
		return
	}

	if images == nil {
		images = []ecradapter.ImageDetail{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"images":    images,
		"count":     len(images),
		"repo":      repo,
		"source_id": sourceID,
	})
}

// handleECRGetImage retrieves details for a specific tagged ECR image.
// GET /api/v1/registry/ecr/{sourceID}/repos/{repo}/images/{tag}
func (s *Server) handleECRGetImage(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("sourceID")
	repo := r.PathValue("repo")
	tag := r.PathValue("tag")

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	image, err := s.accessor.ECRGetImageDetails(r.Context(), principal, sourceID, repo, tag)
	s.services.Metrics.RecordAdapterCall(r.Context(), "ecr", "get_image", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "ECR") {
			return
		}
		writeInternalError(w, err, "ecr get image")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"image":     image,
		"repo":      repo,
		"tag":       tag,
		"source_id": sourceID,
	})
}
