package coreagent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jaimegago/joe/internal/adapters/git"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/store"
)

func (r *Refresher) refreshGitSource(ctx context.Context, source *store.Source, adapter git.GitAdapter) error {
	start := time.Now()
	r.logger.Info("refreshing git source", "source_id", source.ID)

	desiredNodes := make([]graph.Node, 0)
	desiredEdges := make([]graph.Edge, 0)

	now := time.Now()

	repoInfo, err := r.buildGitRepoNode(source.ID, adapter, now)
	if err != nil {
		return fmt.Errorf("build git repo node: %w", err)
	}
	desiredNodes = append(desiredNodes, repoInfo.node)

	joeFiles, err := r.detectJoeFiles(ctx, adapter)
	if err != nil {
		repoInfo.metadata["joe_dir_present"] = false
		r.logger.Debug("failed to detect .joe/ files", "source_id", source.ID, "error", err)
	} else {
		repoInfo.metadata["joe_dir_present"] = len(joeFiles) > 0
	}

	desiredNodes[0] = graph.Node{
		ID:       repoInfo.node.ID,
		Type:     repoInfo.node.Type,
		SourceID: repoInfo.node.SourceID,
		Metadata: repoInfo.metadata,
		LastSeen: now,
	}

	existingNodes, existingEdges, err := LoadGraphStateForSource(ctx, r.services.Graph, source.ID)
	if err != nil {
		return err
	}

	delta := BuildGraphDelta(existingNodes, existingEdges, desiredNodes, desiredEdges)
	if err := ApplyGraphDelta(ctx, r.services.Graph, delta); err != nil {
		return err
	}

	r.logger.Info("git refresh completed", "source_id", source.ID, "nodes", len(desiredNodes), "joe_files", len(joeFiles), "duration_ms", time.Since(start).Milliseconds())
	return nil
}

type gitRepoInfo struct {
	node     graph.Node
	metadata map[string]any
}

func (r *Refresher) buildGitRepoNode(sourceID string, adapter git.GitAdapter, now time.Time) (gitRepoInfo, error) {
	metadata := map[string]any{}

	info := gitRepoInfo{
		node: graph.Node{
			ID:       gitNodeID(sourceID, "repo"),
			Type:     "git_repo",
			SourceID: sourceID,
			LastSeen: now,
		},
		metadata: metadata,
	}

	logs, err := adapter.Log(context.Background(), 1)
	if err == nil && len(logs) > 0 {
		latest := logs[0]
		metadata["head_commit"] = latest.Hash
		metadata["latest_commit_date"] = latest.Date.UTC().Format(time.RFC3339)
		metadata["latest_author"] = latest.Author
	}

	return info, nil
}

func (r *Refresher) detectJoeFiles(_ context.Context, adapter git.GitAdapter) ([]string, error) {
	files, err := adapter.ListFiles(context.Background(), ".joe")
	if err != nil {
		return nil, err
	}

	joeFiles := make([]string, 0)
	for _, file := range files {
		if !file.IsDir && strings.HasSuffix(file.Path, ".yaml") || strings.HasSuffix(file.Path, ".yml") {
			joeFiles = append(joeFiles, file.Path)
		}
	}

	return joeFiles, nil
}

func gitNodeID(sourceID, nodeType string) string {
	return fmt.Sprintf("git/%s/%s", sourceID, nodeType)
}
