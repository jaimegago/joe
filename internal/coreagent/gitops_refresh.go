package coreagent

import (
	"context"
	"fmt"
	"time"

	argocdadapter "github.com/jaimegago/joe/internal/adapters/gitops/argocd"
	terraformadapter "github.com/jaimegago/joe/internal/adapters/iac/terraform"
	helmadapter "github.com/jaimegago/joe/internal/adapters/packaging/helm"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/store"
)

// refreshArgoCDComponent refreshes an Argo CD source.
// Creates app nodes and builds managed_by edges from K8s workloads to their
// managing Argo CD application.
func (r *Refresher) refreshArgoCDComponent(ctx context.Context, source *store.Component, adapter argocdadapter.ArgoCDAdapter) error {
	r.logger.Info("refreshing argocd source", "component_id", source.ID)

	now := time.Now()
	sourceNodeID := gitopsNodeID(source.ID, source.Type)

	desiredNodes := []graph.Node{
		{
			ID:          sourceNodeID,
			Type:        "argocd_component",
			ComponentID: source.ID,
			Metadata:    gitopsMetadata(source),
			LastSeen:    now,
		},
	}
	desiredEdges := make([]graph.Edge, 0)

	apps, err := adapter.Apps(ctx, "")
	if err != nil {
		r.logger.Warn("failed to list argocd apps (skipping edge discovery)", "component_id", source.ID, "error", err)
	} else {
		for _, app := range apps {
			appNodeID := fmt.Sprintf("argocd/%s/%s", source.ID, app.Name)
			desiredNodes = append(desiredNodes, graph.Node{
				ID:          appNodeID,
				Type:        "argocd_app",
				ComponentID: source.ID,
				Metadata: map[string]any{
					"name":        app.Name,
					"project":     app.Project,
					"namespace":   app.Namespace,
					"sync_status": app.SyncStatus,
					"health":      app.Health,
					"repo_url":    app.RepoURL,
				},
				LastSeen: now,
			})

			// Find K8s workloads in the graph matching the app name and
			// namespace, then create managed_by edges (workload → argocd app).
			edges := r.buildManagedByEdges(ctx, source, appNodeID, app.Name, app.Namespace, now)
			desiredEdges = append(desiredEdges, edges...)
		}
	}

	existingNodes, existingEdges, err := LoadGraphStateForComponent(ctx, r.services.Graph, source.ID)
	if err != nil {
		return fmt.Errorf("load graph state for argocd source %s: %w", source.ID, err)
	}

	delta := BuildGraphDelta(existingNodes, existingEdges, desiredNodes, desiredEdges)
	if err := ApplyGraphDelta(ctx, r.services.Graph, delta); err != nil {
		return fmt.Errorf("apply graph delta for argocd source %s: %w", source.ID, err)
	}

	r.logger.Info("argocd refresh completed",
		"component_id", source.ID,
		"apps", len(apps),
		"nodes", len(desiredNodes),
		"edges", len(desiredEdges),
	)
	return nil
}

// refreshHelmComponent refreshes a Helm source.
// Creates release nodes and builds managed_by edges from K8s workloads to their
// managing Helm release.
func (r *Refresher) refreshHelmComponent(ctx context.Context, source *store.Component, adapter helmadapter.HelmAdapter) error {
	r.logger.Info("refreshing helm source", "component_id", source.ID)

	now := time.Now()
	sourceNodeID := gitopsNodeID(source.ID, source.Type)

	desiredNodes := []graph.Node{
		{
			ID:          sourceNodeID,
			Type:        "helm_component",
			ComponentID: source.ID,
			Metadata:    gitopsMetadata(source),
			LastSeen:    now,
		},
	}
	desiredEdges := make([]graph.Edge, 0)

	// List all releases across all namespaces.
	releases, err := adapter.Releases(ctx, "")
	if err != nil {
		r.logger.Warn("failed to list helm releases (skipping edge discovery)", "component_id", source.ID, "error", err)
	} else {
		for _, rel := range releases {
			releaseNodeID := fmt.Sprintf("helm/%s/%s/%s", source.ID, rel.Namespace, rel.Name)
			desiredNodes = append(desiredNodes, graph.Node{
				ID:          releaseNodeID,
				Type:        "helm_release",
				ComponentID: source.ID,
				Metadata: map[string]any{
					"name":          rel.Name,
					"namespace":     rel.Namespace,
					"chart":         rel.Chart,
					"chart_version": rel.ChartVersion,
					"status":        rel.Status,
					"revision":      rel.Revision,
				},
				LastSeen: now,
			})

			// Build managed_by edges from workloads matching the release name/namespace.
			edges := r.buildManagedByEdges(ctx, source, releaseNodeID, rel.Name, rel.Namespace, now)
			desiredEdges = append(desiredEdges, edges...)
		}
	}

	existingNodes, existingEdges, err := LoadGraphStateForComponent(ctx, r.services.Graph, source.ID)
	if err != nil {
		return fmt.Errorf("load graph state for helm source %s: %w", source.ID, err)
	}

	delta := BuildGraphDelta(existingNodes, existingEdges, desiredNodes, desiredEdges)
	if err := ApplyGraphDelta(ctx, r.services.Graph, delta); err != nil {
		return fmt.Errorf("apply graph delta for helm source %s: %w", source.ID, err)
	}

	r.logger.Info("helm refresh completed",
		"component_id", source.ID,
		"releases", len(releases),
		"nodes", len(desiredNodes),
		"edges", len(desiredEdges),
	)
	return nil
}

// refreshTerraformComponent refreshes a Terraform source.
// Creates resource nodes and builds provisions edges from TF resources to
// matching cloud nodes in the graph.
func (r *Refresher) refreshTerraformComponent(ctx context.Context, source *store.Component, adapter terraformadapter.TerraformAdapter) error {
	r.logger.Info("refreshing terraform source", "component_id", source.ID)

	now := time.Now()
	sourceNodeID := gitopsNodeID(source.ID, source.Type)

	desiredNodes := []graph.Node{
		{
			ID:          sourceNodeID,
			Type:        "terraform_component",
			ComponentID: source.ID,
			Metadata:    gitopsMetadata(source),
			LastSeen:    now,
		},
	}
	desiredEdges := make([]graph.Edge, 0)

	// List all managed resources from state (empty string = all types).
	resources, err := adapter.Resources(ctx, "")
	if err != nil {
		r.logger.Warn("failed to list terraform resources (skipping edge discovery)", "component_id", source.ID, "error", err)
	} else {
		for _, res := range resources {
			if res.Mode != "managed" {
				continue // skip data components
			}
			tfNodeID := fmt.Sprintf("terraform/%s/%s", source.ID, res.Address)
			desiredNodes = append(desiredNodes, graph.Node{
				ID:          tfNodeID,
				Type:        "terraform_resource",
				ComponentID: source.ID,
				Metadata: map[string]any{
					"address":  res.Address,
					"type":     res.Type,
					"name":     res.Name,
					"provider": res.Provider,
					"mode":     res.Mode,
				},
				LastSeen: now,
			})

			// Attempt to match this resource to an existing cloud graph node
			// (e.g., an EC2 instance or RDS instance) via the provisions edge.
			edges := r.buildProvidesEdges(ctx, source, tfNodeID, res.Name, res.Type, now)
			desiredEdges = append(desiredEdges, edges...)
		}
	}

	existingNodes, existingEdges, err := LoadGraphStateForComponent(ctx, r.services.Graph, source.ID)
	if err != nil {
		return fmt.Errorf("load graph state for terraform source %s: %w", source.ID, err)
	}

	delta := BuildGraphDelta(existingNodes, existingEdges, desiredNodes, desiredEdges)
	if err := ApplyGraphDelta(ctx, r.services.Graph, delta); err != nil {
		return fmt.Errorf("apply graph delta for terraform source %s: %w", source.ID, err)
	}

	r.logger.Info("terraform refresh completed",
		"component_id", source.ID,
		"resources", len(resources),
		"nodes", len(desiredNodes),
		"edges", len(desiredEdges),
	)
	return nil
}

// buildManagedByEdges queries the graph for K8s workload nodes (deployment/
// statefulset/daemonset) whose name matches the given workload name in the
// given namespace, then creates managed_by edges pointing to the managing node.
func (r *Refresher) buildManagedByEdges(ctx context.Context, source *store.Component, managerNodeID, name, namespace string, now time.Time) []graph.Edge {
	var edges []graph.Edge

	if name == "" {
		return edges
	}

	matchingNodes, err := r.services.Graph.Query(ctx, name)
	if err != nil {
		r.logger.Debug("graph query failed for managed_by match", "name", name, "error", err)
		return edges
	}

	for _, node := range matchingNodes {
		switch node.Type {
		case "deployment", "statefulset", "daemonset", "service":
		default:
			continue
		}

		// Prefer same-namespace matches when namespace is known.
		if namespace != "" {
			nodeNS, _ := node.Metadata["namespace"].(string)
			if nodeNS != "" && nodeNS != namespace {
				continue
			}
		}

		edges = append(edges, graph.Edge{
			From:        node.ID,
			To:          managerNodeID,
			Relation:    graph.RelationManagedBy,
			Confidence:  graph.Inferred,
			Source:      "gitops_name_match",
			ComponentID: source.ID,
			Context:     "name=" + name,
			CreatedAt:   now,
		})
	}

	return edges
}

// buildProvidesEdges queries the graph for cloud nodes matching the Terraform
// resource name, creating provisions edges (tf resource → cloud node).
func (r *Refresher) buildProvidesEdges(ctx context.Context, source *store.Component, tfNodeID, resName, resType string, now time.Time) []graph.Edge {
	var edges []graph.Edge

	if resName == "" {
		return edges
	}

	matchingNodes, err := r.services.Graph.Query(ctx, resName)
	if err != nil {
		r.logger.Debug("graph query failed for provisions match", "resource", resName, "error", err)
		return edges
	}

	for _, node := range matchingNodes {
		// Only match cloud-tier node types (EC2, RDS, EKS node, Azure VM, etc.).
		switch node.Type {
		case "ec2_instance", "rds_instance", "eks_cluster",
			"azure_vm", "azure_aks", "azure_sql",
			"k8s_node", "node":
		default:
			continue
		}

		edges = append(edges, graph.Edge{
			From:        tfNodeID,
			To:          node.ID,
			Relation:    graph.RelationProvisions,
			Confidence:  graph.Inferred,
			Source:      "terraform_name_match",
			ComponentID: source.ID,
			Context:     "type=" + resType + ",name=" + resName,
			CreatedAt:   now,
		})
	}

	return edges
}

// gitopsNodeID builds a stable graph node ID for a GitOps/IaC source.
func gitopsNodeID(sourceID, sourceType string) string {
	return fmt.Sprintf("gitops/%s/%s", sourceType, sourceID)
}

// gitopsMetadata builds the standard metadata map for a GitOps/IaC source node.
func gitopsMetadata(source *store.Component) map[string]any {
	return map[string]any{
		"component_id":   source.ID,
		"component_type": source.Type,
		"name":           source.Name,
	}
}
