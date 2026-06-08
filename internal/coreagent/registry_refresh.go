package coreagent

import (
	"context"
	"fmt"
	"time"

	artifactoryadapter "github.com/jaimegago/joe/internal/adapters/registry/artifactory"
	ecradapter "github.com/jaimegago/joe/internal/adapters/registry/ecr"
	ociadapter "github.com/jaimegago/joe/internal/adapters/registry/oci"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/store"
)

// refreshOCIComponent refreshes an OCI-compatible container registry source
// (DockerHub, GHCR, Harbor, Quay, or any OCI Distribution Spec v2 registry).
// Creates a source node and repository nodes, then attempts image_stored_in
// edge discovery by matching repository names to existing deployment nodes.
func (r *Refresher) refreshOCIComponent(ctx context.Context, source *store.Component, adapter ociadapter.OCIAdapter) error {
	r.logger.Info("refreshing oci registry source", "component_id", source.ID)
	return r.refreshRegistryComponent(ctx, source, "oci_registry", func() ([]string, error) {
		return adapter.ListRepositories(ctx)
	})
}

// refreshDockerHubComponent refreshes a DockerHub source using the OCI adapter.
func (r *Refresher) refreshDockerHubComponent(ctx context.Context, source *store.Component, adapter ociadapter.OCIAdapter) error {
	r.logger.Info("refreshing dockerhub source", "component_id", source.ID)
	return r.refreshRegistryComponent(ctx, source, "dockerhub", func() ([]string, error) {
		return adapter.ListRepositories(ctx)
	})
}

// refreshArtifactoryComponent refreshes a JFrog Artifactory registry source.
// Lists Docker and Helm repositories and creates image_stored_in edges.
func (r *Refresher) refreshArtifactoryComponent(ctx context.Context, source *store.Component, adapter artifactoryadapter.ArtifactoryAdapter) error {
	r.logger.Info("refreshing artifactory source", "component_id", source.ID)

	now := time.Now()
	sourceNodeID := registryNodeID(source.ID, source.Type)

	desiredNodes := []graph.Node{
		{
			ID:          sourceNodeID,
			Type:        "artifact_registry",
			ComponentID: source.ID,
			Metadata:    registryMetadata(source),
			LastSeen:    now,
		},
	}
	desiredEdges := make([]graph.Edge, 0)

	repos, err := adapter.ListRepositories(ctx)
	if err != nil {
		r.logger.Warn("failed to list artifactory repositories (skipping repo nodes)", "component_id", source.ID, "error", err)
	} else {
		for _, repo := range repos {
			repoID := repoNodeID(source.ID, repo.Key)
			desiredNodes = append(desiredNodes, graph.Node{
				ID:          repoID,
				Type:        "image_repository",
				ComponentID: source.ID,
				Metadata: map[string]any{
					"component_id":   source.ID,
					"component_type": source.Type,
					"name":           repo.Key,
					"package_type":   repo.PackageType,
					"description":    repo.Description,
				},
				LastSeen: now,
			})
			edges := r.buildImageStoredInEdges(ctx, source, repoID, repo.Key, now)
			desiredEdges = append(desiredEdges, edges...)
		}
	}

	return r.applyRegistryDelta(ctx, source, desiredNodes, desiredEdges, "artifactory")
}

// refreshECRComponent refreshes an AWS ECR source.
func (r *Refresher) refreshECRComponent(ctx context.Context, source *store.Component, adapter ecradapter.ECRAdapter) error {
	r.logger.Info("refreshing ecr source", "component_id", source.ID)

	now := time.Now()
	sourceNodeID := registryNodeID(source.ID, source.Type)

	desiredNodes := []graph.Node{
		{
			ID:          sourceNodeID,
			Type:        "artifact_registry",
			ComponentID: source.ID,
			Metadata:    registryMetadata(source),
			LastSeen:    now,
		},
	}
	desiredEdges := make([]graph.Edge, 0)

	repos, err := adapter.ListRepositories(ctx)
	if err != nil {
		r.logger.Warn("failed to list ECR repositories (skipping repo nodes)", "component_id", source.ID, "error", err)
	} else {
		for _, repo := range repos {
			repoID := repoNodeID(source.ID, repo.Name)
			desiredNodes = append(desiredNodes, graph.Node{
				ID:          repoID,
				Type:        "image_repository",
				ComponentID: source.ID,
				Metadata: map[string]any{
					"component_id":   source.ID,
					"component_type": source.Type,
					"name":           repo.Name,
					"uri":            repo.URI,
					"created_at":     repo.CreatedAt,
				},
				LastSeen: now,
			})
			edges := r.buildImageStoredInEdges(ctx, source, repoID, repo.Name, now)
			desiredEdges = append(desiredEdges, edges...)
		}
	}

	return r.applyRegistryDelta(ctx, source, desiredNodes, desiredEdges, "ecr")
}

// refreshRegistryComponent is the common refresh path for OCI-compatible registries.
// listRepos is a closure that fetches the repository list from the adapter.
func (r *Refresher) refreshRegistryComponent(ctx context.Context, source *store.Component, tag string, listRepos func() ([]string, error)) error {
	now := time.Now()
	sourceNodeID := registryNodeID(source.ID, source.Type)

	desiredNodes := []graph.Node{
		{
			ID:          sourceNodeID,
			Type:        "artifact_registry",
			ComponentID: source.ID,
			Metadata:    registryMetadata(source),
			LastSeen:    now,
		},
	}
	desiredEdges := make([]graph.Edge, 0)

	repos, err := listRepos()
	if err != nil {
		r.logger.Warn("failed to list registry repositories (skipping repo nodes)", "component_id", source.ID, "type", tag, "error", err)
	} else {
		for _, repoName := range repos {
			repoID := repoNodeID(source.ID, repoName)
			desiredNodes = append(desiredNodes, graph.Node{
				ID:          repoID,
				Type:        "image_repository",
				ComponentID: source.ID,
				Metadata: map[string]any{
					"component_id":   source.ID,
					"component_type": source.Type,
					"name":           repoName,
				},
				LastSeen: now,
			})
			edges := r.buildImageStoredInEdges(ctx, source, repoID, repoName, now)
			desiredEdges = append(desiredEdges, edges...)
		}
	}

	return r.applyRegistryDelta(ctx, source, desiredNodes, desiredEdges, tag)
}

// applyRegistryDelta loads existing graph state, computes a delta, and applies it.
func (r *Refresher) applyRegistryDelta(ctx context.Context, source *store.Component, desiredNodes []graph.Node, desiredEdges []graph.Edge, tag string) error {
	existingNodes, existingEdges, err := LoadGraphStateForComponent(ctx, r.services.Graph, source.ID)
	if err != nil {
		return fmt.Errorf("load graph state for %s source %s: %w", tag, source.ID, err)
	}

	delta := BuildGraphDelta(existingNodes, existingEdges, desiredNodes, desiredEdges)
	if err := ApplyGraphDelta(ctx, r.services.Graph, delta); err != nil {
		return fmt.Errorf("apply graph delta for %s source %s: %w", tag, source.ID, err)
	}

	r.logger.Info("registry refresh completed",
		"component_id", source.ID,
		"type", tag,
		"nodes", len(desiredNodes),
		"edges", len(desiredEdges),
	)
	return nil
}

// buildImageStoredInEdges queries the graph for deployment/service nodes whose
// name matches repoName, creating image_stored_in edges (low-confidence, inferred).
// Explicit edges are expected to come from .joe/ file processing.
func (r *Refresher) buildImageStoredInEdges(ctx context.Context, source *store.Component, repoNodeID, repoName string, now time.Time) []graph.Edge {
	var edges []graph.Edge

	if repoName == "" {
		return edges
	}

	matchingNodes, err := r.services.Graph.Query(ctx, repoName)
	if err != nil {
		r.logger.Debug("graph query failed for image_stored_in name match", "repo", repoName, "error", err)
		return edges
	}

	for _, node := range matchingNodes {
		if node.Type != "deployment" && node.Type != "service" {
			continue
		}
		edges = append(edges, graph.Edge{
			From:        node.ID,
			To:          repoNodeID,
			Relation:    graph.RelationImageStoredIn,
			Confidence:  graph.Inferred,
			Source:      source.Type + "_name_match",
			ComponentID: source.ID,
			Context:     "repo=" + repoName,
			CreatedAt:   now,
		})
	}

	return edges
}

// registryNodeID returns the stable node ID for a registry source node.
// Format: registry/<sourceType>/<sourceID>
func registryNodeID(sourceID, sourceType string) string {
	return "registry/" + sourceType + "/" + sourceID
}

// repoNodeID returns the stable node ID for a repository within a registry.
// Format: registry/<sourceID>/repo/<repoName>
func repoNodeID(sourceID, repoName string) string {
	return "registry/" + sourceID + "/repo/" + repoName
}

// registryMetadata returns standard metadata for a registry source node.
func registryMetadata(source *store.Component) map[string]any {
	return map[string]any{
		"component_id":   source.ID,
		"component_type": source.Type,
		"name":           source.Name,
	}
}
