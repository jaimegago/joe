package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	mcpgo "github.com/mark3labs/mcp-go/mcp"

	"github.com/jaimegago/joe/internal/client"
	"github.com/jaimegago/joe/internal/knowledge"
)

// Dispatcher routes MCP tool calls to the appropriate CoreClient method.
type Dispatcher struct {
	c *client.Client
}

// NewDispatcher creates a new Dispatcher backed by the given CoreClient.
func NewDispatcher(c *client.Client) *Dispatcher {
	return &Dispatcher{c: c}
}

// HandleGraphQuery handles the joe_graph_query MCP tool.
func (d *Dispatcher) HandleGraphQuery(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	query, err := req.RequireString("query")
	if err != nil {
		return nil, err
	}

	nodes, err := d.c.GraphQuery(ctx, query)
	if err != nil {
		return errorResult(fmt.Errorf("graph query failed: %w", err)), nil
	}

	return jsonResult(map[string]any{
		"nodes": nodes,
		"count": len(nodes),
	})
}

// HandleGraphRelated handles the joe_graph_related MCP tool.
func (d *Dispatcher) HandleGraphRelated(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	nodeID, err := req.RequireString("node_id")
	if err != nil {
		return nil, err
	}
	depth := int(req.GetFloat("depth", 1))
	if depth < 1 {
		depth = 1
	}
	if depth > 3 {
		depth = 3
	}

	subgraph, err := d.c.GraphRelated(ctx, nodeID, depth)
	if err != nil {
		return errorResult(fmt.Errorf("graph related failed: %w", err)), nil
	}

	return jsonResult(subgraph)
}

// HandleK8s handles the joe_k8s MCP tool.
func (d *Dispatcher) HandleK8s(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	service, err := req.RequireString("service")
	if err != nil {
		return nil, err
	}
	question, err := req.RequireString("question")
	if err != nil {
		return nil, err
	}

	result, err := d.c.QueryK8s(ctx, service, question)
	if err != nil {
		return errorResult(fmt.Errorf("k8s query failed: %w", err)), nil
	}

	return jsonResult(result)
}

// HandleMetrics handles the joe_metrics MCP tool.
func (d *Dispatcher) HandleMetrics(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	service, err := req.RequireString("service")
	if err != nil {
		return nil, err
	}
	question, err := req.RequireString("question")
	if err != nil {
		return nil, err
	}

	result, err := d.c.QueryMetrics(ctx, service, question)
	if err != nil {
		return errorResult(fmt.Errorf("metrics query failed: %w", err)), nil
	}

	return jsonResult(result)
}

// HandleLogs handles the joe_logs MCP tool.
func (d *Dispatcher) HandleLogs(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	service, err := req.RequireString("service")
	if err != nil {
		return nil, err
	}
	question, err := req.RequireString("question")
	if err != nil {
		return nil, err
	}

	result, err := d.c.QueryLogs(ctx, service, question)
	if err != nil {
		return errorResult(fmt.Errorf("logs query failed: %w", err)), nil
	}

	return jsonResult(result)
}

// HandleTraces handles the joe_traces MCP tool.
func (d *Dispatcher) HandleTraces(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	service, err := req.RequireString("service")
	if err != nil {
		return nil, err
	}
	question, err := req.RequireString("question")
	if err != nil {
		return nil, err
	}

	result, err := d.c.QueryTraces(ctx, service, question)
	if err != nil {
		return errorResult(fmt.Errorf("traces query failed: %w", err)), nil
	}

	return jsonResult(result)
}

// HandleAlerts handles the joe_alerts MCP tool.
func (d *Dispatcher) HandleAlerts(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	service, err := req.RequireString("service")
	if err != nil {
		return nil, err
	}
	question := req.GetString("question", "")

	result, err := d.c.QueryAlerts(ctx, service, question)
	if err != nil {
		return errorResult(fmt.Errorf("alerts query failed: %w", err)), nil
	}

	return jsonResult(result)
}

// HandleKnowledgeSearch handles the joe_knowledge_search MCP tool.
func (d *Dispatcher) HandleKnowledgeSearch(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	query, err := req.RequireString("query")
	if err != nil {
		return nil, err
	}
	topK := int(req.GetFloat("top_k", 5))
	if topK < 1 {
		topK = 5
	}

	results, err := d.c.SearchKnowledge(ctx, query, topK, []knowledge.Tier{})
	if err != nil {
		return errorResult(fmt.Errorf("knowledge search failed: %w", err)), nil
	}

	return jsonResult(map[string]any{
		"results": results,
		"count":   len(results),
	})
}

// --- helpers ---

func jsonResult(v any) (*mcpgo.CallToolResult, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal result: %w", err)
	}
	return &mcpgo.CallToolResult{
		Content: []mcpgo.Content{
			mcpgo.TextContent{
				Type: "text",
				Text: string(data),
			},
		},
	}, nil
}

func errorResult(err error) *mcpgo.CallToolResult {
	return &mcpgo.CallToolResult{
		IsError: true,
		Content: []mcpgo.Content{
			mcpgo.TextContent{
				Type: "text",
				Text: err.Error(),
			},
		},
	}
}
