package coreagent

import (
	"context"
	"fmt"
	"time"

	"github.com/jaimegago/joe/internal/adapters/git"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/store"
)

func (r *Refresher) refreshGitComponent(ctx context.Context, source *store.Component, adapter git.GitAdapter) error {
	start := time.Now()
	r.logger.Info("refreshing git component", "component_id", source.ID)

	desiredNodes := make([]graph.Node, 0)
	desiredEdges := make([]graph.Edge, 0)

	now := time.Now()

	repoInfo, err := r.buildGitRepoNode(ctx, source.ID, adapter, now)
	if err != nil {
		return fmt.Errorf("build git repo node: %w", err)
	}

	desiredNodes = append(desiredNodes, graph.Node{
		ID:          repoInfo.node.ID,
		Type:        repoInfo.node.Type,
		ComponentID: repoInfo.node.ComponentID,
		Metadata:    repoInfo.metadata,
		LastSeen:    now,
	})

	if hostNode, hostEdge, ok := r.buildGitHostingEdge(ctx, source, repoInfo.node.ID, now); ok {
		desiredNodes = append(desiredNodes, hostNode)
		desiredEdges = append(desiredEdges, hostEdge)
	}

	existingNodes, existingEdges, err := LoadGraphStateForComponent(ctx, r.services.Graph, source.ID)
	if err != nil {
		return err
	}

	delta := BuildGraphDelta(existingNodes, existingEdges, desiredNodes, desiredEdges)
	if err := ApplyGraphDelta(ctx, r.services.Graph, delta); err != nil {
		return err
	}

	r.logger.Info("git refresh completed", "component_id", source.ID,
		"nodes", len(desiredNodes), "edges", len(desiredEdges), "duration_ms", time.Since(start).Milliseconds())
	return nil
}

// buildGitHostingEdge derives the repository's hosting relationship from the git
// component's DECLARED provider_component_id — the optional config field naming
// the github or gitlab component the repository lives on (D-0150).
//
// It is a deterministic derivation of an operator-declared field, not an
// inference: nothing is guessed from the repository URL, nothing is discovered by
// reaching out, and no model is consulted (D-0110). The declaration is validated
// at registration for SHAPE only, so a dangling reference is legal and expected —
// the named component may not exist yet, or may have been deleted. That case is
// logged and skipped, yielding no node and no edge, so the graph simply carries
// no hosting claim rather than a false one.
//
// The host node is keyed under THIS git component (git/<id>/provider), not under
// the provider component. Two git components on the same forge therefore get one
// host node each. That is deliberate: every node the refresher writes must be
// owned by the component whose reconcile pass produced it, or a shared node would
// be claimed and its edges reaped in turn by each owner's per-component delta.
//
// The edge is discovery-only. It says where a repository lives so the provider's
// PR/MR surface can be found beside it; it grants nothing, and the two components
// remain governed independently.
func (r *Refresher) buildGitHostingEdge(ctx context.Context, source *store.Component, repoNodeID string, now time.Time) (graph.Node, graph.Edge, bool) {
	cfg, err := git.ParseConfig(source.Config)
	if err != nil || cfg.ProviderComponentID == "" {
		return graph.Node{}, graph.Edge{}, false
	}

	provider, err := r.services.Store.Components.Get(ctx, cfg.ProviderComponentID)
	if err != nil {
		r.logger.Warn("git hosting provider lookup failed, skipping hosting edge",
			"component_id", source.ID, "provider_component_id", cfg.ProviderComponentID, "error", err)
		return graph.Node{}, graph.Edge{}, false
	}
	if provider == nil {
		r.logger.Info("git hosting provider not found, skipping hosting edge",
			"component_id", source.ID, "provider_component_id", cfg.ProviderComponentID)
		return graph.Node{}, graph.Edge{}, false
	}
	if provider.Type != store.ComponentTypeGitHub && provider.Type != store.ComponentTypeGitLab {
		r.logger.Info("git hosting provider is not a github or gitlab component, skipping hosting edge",
			"component_id", source.ID, "provider_component_id", provider.ID, "provider_type", provider.Type)
		return graph.Node{}, graph.Edge{}, false
	}

	hostNodeID := gitNodeID(source.ID, "provider")
	node := graph.Node{
		ID:          hostNodeID,
		Type:        "code_host",
		ComponentID: source.ID,
		Metadata: map[string]any{
			"provider_component_id": provider.ID,
			"provider_type":         provider.Type,
			"provider_name":         provider.Name,
		},
		LastSeen: now,
	}
	edge := graph.Edge{
		From:        repoNodeID,
		To:          hostNodeID,
		Relation:    graph.RelationHostedBy,
		Confidence:  graph.Explicit,
		Source:      "git_provider_declaration",
		ComponentID: source.ID,
		Context:     "provider_component_id=" + provider.ID,
		CreatedAt:   now,
	}
	return node, edge, true
}

type gitRepoInfo struct {
	node     graph.Node
	metadata map[string]any
}

func (r *Refresher) buildGitRepoNode(ctx context.Context, sourceID string, adapter git.GitAdapter, now time.Time) (gitRepoInfo, error) {
	metadata := map[string]any{}

	info := gitRepoInfo{
		node: graph.Node{
			ID:          gitNodeID(sourceID, "repo"),
			Type:        "git_repo",
			ComponentID: sourceID,
			LastSeen:    now,
		},
		metadata: metadata,
	}

	logs, err := adapter.Log(ctx, 1)
	if err == nil && len(logs) > 0 {
		latest := logs[0]
		metadata["head_commit"] = latest.Hash
		metadata["latest_commit_date"] = latest.Date.UTC().Format(time.RFC3339)
		metadata["latest_author"] = latest.Author
	}

	return info, nil
}

func gitNodeID(sourceID, nodeType string) string {
	return fmt.Sprintf("git/%s/%s", sourceID, nodeType)
}
