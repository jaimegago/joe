package coreagent

import (
	"context"
	"fmt"
	"time"

	"github.com/jaimegago/joe/internal/adapters/git"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/llm"
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

	// Process .joe/ files with caching
	toolCalls, err := r.joeFileService.ProcessJoeFiles(ctx, adapter, source.ID)
	if err != nil {
		r.logger.Warn("failed to process .joe files", "source_id", source.ID, "error", err)
		repoInfo.metadata["joe_dir_present"] = false
	} else {
		// toolCalls == nil means no .joe/ files found, !nil means files exist
		hasJoeFiles := toolCalls != nil
		repoInfo.metadata["joe_dir_present"] = hasJoeFiles

		if len(toolCalls) > 0 {
			r.logger.Info("executing tool calls from .joe/ files", "source_id", source.ID, "tool_calls", len(toolCalls))
			if err := r.executeJoeFileToolCalls(ctx, toolCalls, source.ID); err != nil {
				r.logger.Warn("failed to execute .joe/ file tool calls", "source_id", source.ID, "error", err)
			}
		}
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

	r.logger.Info("git refresh completed", "source_id", source.ID, "nodes", len(desiredNodes), "tool_calls", len(toolCalls), "duration_ms", time.Since(start).Milliseconds())
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

// executeJoeFileToolCalls executes tool calls returned from .joe/ file interpretation.
// Errors during tool execution are logged but not bubbled up.
// This provides graceful degradation: partial tool call failures don't fail the entire git refresh.
func (r *Refresher) executeJoeFileToolCalls(ctx context.Context, toolCalls []llm.ToolCall, sourceID string) error {
	for i, toolCall := range toolCalls {
		r.logger.Debug("executing .joe/ tool call",
			"source_id", sourceID,
			"tool", toolCall.Name,
			"call_index", i+1,
			"total_calls", len(toolCalls))

		switch toolCall.Name {
		case "graph_add_node":
			if err := r.executeAddNode(ctx, toolCall.Args, sourceID); err != nil {
				r.logger.Warn("failed to execute graph_add_node", "source_id", sourceID, "error", err)
			}
		case "graph_add_edge":
			if err := r.executeAddEdge(ctx, toolCall.Args, sourceID); err != nil {
				r.logger.Warn("failed to execute graph_add_edge", "source_id", sourceID, "error", err)
			}
		case "save_onboarding_fact":
			if err := r.executeSaveOnboardingFact(ctx, toolCall.Args, sourceID); err != nil {
				r.logger.Warn("failed to execute save_onboarding_fact", "source_id", sourceID, "error", err)
			}
		default:
			r.logger.Warn("unknown tool call from .joe/ file", "source_id", sourceID, "tool", toolCall.Name)
		}
	}
	return nil
}

// executeAddNode executes a graph_add_node tool call
func (r *Refresher) executeAddNode(ctx context.Context, args map[string]any, sourceID string) error {
	nodeID, _ := args["node_id"].(string)
	nodeType, _ := args["node_type"].(string)
	metadata, _ := args["metadata"].(map[string]any)

	if nodeID == "" || nodeType == "" {
		return fmt.Errorf("node_id and node_type are required")
	}

	if metadata == nil {
		metadata = make(map[string]any)
	}

	node := graph.Node{
		ID:       nodeID,
		Type:     nodeType,
		SourceID: sourceID,
		Metadata: metadata,
		LastSeen: time.Now(),
	}

	if err := r.services.Graph.AddNode(ctx, node); err != nil {
		return fmt.Errorf("add node: %w", err)
	}

	r.logger.Debug("added node from .joe/ file", "node_id", nodeID, "node_type", nodeType)
	return nil
}

// executeAddEdge executes a graph_add_edge tool call
func (r *Refresher) executeAddEdge(ctx context.Context, args map[string]any, sourceID string) error {
	from, _ := args["from"].(string)
	to, _ := args["to"].(string)
	relation, _ := args["relation"].(string)

	if from == "" || to == "" || relation == "" {
		return fmt.Errorf("from, to, and relation are required")
	}

	edge := graph.Edge{
		From:       from,
		To:         to,
		Relation:   relation,
		Confidence: graph.Explicit,
		Source:     "joe_file",
		SourceID:   sourceID,
		Context:    ".joe/ file interpretation",
		CreatedAt:  time.Now(),
	}

	if err := r.services.Graph.AddEdge(ctx, edge); err != nil {
		return fmt.Errorf("add edge: %w", err)
	}

	r.logger.Debug("added edge from .joe/ file", "from", from, "to", to, "relation", relation)
	return nil
}

// executeSaveOnboardingFact executes a save_onboarding_fact tool call
func (r *Refresher) executeSaveOnboardingFact(ctx context.Context, args map[string]any, sourceID string) error {
	factType, _ := args["fact_type"].(string)
	subject, _ := args["subject"].(string)
	content, _ := args["content"].(string)

	if factType == "" || subject == "" || content == "" {
		return fmt.Errorf("fact_type, subject, and content are required")
	}

	fact := &store.OnboardingFact{
		FactType: factType,
		Subject:  subject,
		Content:  content,
		Source:   "joe_file",
		SourceID: sourceID,
	}

	if err := r.services.Store.Facts.Create(ctx, fact); err != nil {
		return fmt.Errorf("save onboarding fact: %w", err)
	}

	r.logger.Debug("saved onboarding fact from .joe/ file", "fact_type", factType, "subject", subject)
	return nil
}

func gitNodeID(sourceID, nodeType string) string {
	return fmt.Sprintf("git/%s/%s", sourceID, nodeType)
}
