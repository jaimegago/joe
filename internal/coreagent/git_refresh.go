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

	existingNodes, existingEdges, err := LoadGraphStateForComponent(ctx, r.services.Graph, source.ID)
	if err != nil {
		return err
	}

	delta := BuildGraphDelta(existingNodes, existingEdges, desiredNodes, desiredEdges)
	if err := ApplyGraphDelta(ctx, r.services.Graph, delta); err != nil {
		return err
	}

	r.logger.Info("git refresh completed", "component_id", source.ID, "nodes", len(desiredNodes), "duration_ms", time.Since(start).Milliseconds())
	return nil
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
