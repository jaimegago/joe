package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

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

// HandleK8sGet handles the joe_k8s_get MCP tool.
func (d *Dispatcher) HandleK8sGet(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	sourceID, err := req.RequireString("source_id")
	if err != nil {
		return nil, err
	}
	resource, err := req.RequireString("resource")
	if err != nil {
		return nil, err
	}
	namespace := req.GetString("namespace", "")
	name := req.GetString("name", "")

	if name != "" {
		result, err := d.c.K8sGetResource(ctx, sourceID, resource, namespace, name)
		if err != nil {
			return errorResult(fmt.Errorf("k8s get resource failed: %w", err)), nil
		}
		return jsonResult(map[string]any{"resource": result})
	}

	results, err := d.c.K8sListResources(ctx, sourceID, resource, namespace)
	if err != nil {
		return errorResult(fmt.Errorf("k8s list resources failed: %w", err)), nil
	}
	return jsonResult(map[string]any{
		"resources": results,
		"count":     len(results),
	})
}

// HandleK8sLogs handles the joe_k8s_logs MCP tool.
func (d *Dispatcher) HandleK8sLogs(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	sourceID, err := req.RequireString("source_id")
	if err != nil {
		return nil, err
	}
	namespace, err := req.RequireString("namespace")
	if err != nil {
		return nil, err
	}
	pod, err := req.RequireString("pod")
	if err != nil {
		return nil, err
	}
	container := req.GetString("container", "")
	tail := int(req.GetFloat("tail", 100))

	logs, err := d.c.K8sGetLogs(ctx, sourceID, namespace, pod, container, tail)
	if err != nil {
		return errorResult(fmt.Errorf("k8s logs failed: %w", err)), nil
	}

	return textResult(logs), nil
}

// HandleMetricsQuery handles the joe_metrics_query MCP tool.
func (d *Dispatcher) HandleMetricsQuery(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	sourceID, err := req.RequireString("source_id")
	if err != nil {
		return nil, err
	}
	query, err := req.RequireString("query")
	if err != nil {
		return nil, err
	}

	result, err := d.c.PrometheusQuery(ctx, sourceID, query, time.Time{})
	if err != nil {
		return errorResult(fmt.Errorf("metrics query failed: %w", err)), nil
	}

	return jsonResult(result)
}

// HandleLogsSearch handles the joe_logs_search MCP tool.
func (d *Dispatcher) HandleLogsSearch(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	sourceID, err := req.RequireString("source_id")
	if err != nil {
		return nil, err
	}
	query, err := req.RequireString("query")
	if err != nil {
		return nil, err
	}
	limit := int(req.GetFloat("limit", 100))
	sinceSeconds := time.Duration(req.GetFloat("since_seconds", 3600)) * time.Second

	result, err := d.c.LokiQuery(ctx, sourceID, query, limit, sinceSeconds)
	if err != nil {
		return errorResult(fmt.Errorf("logs search failed: %w", err)), nil
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

// HandleIncidents handles the joe_incidents MCP tool.
func (d *Dispatcher) HandleIncidents(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	sourceID, err := req.RequireString("source_id")
	if err != nil {
		return nil, err
	}
	filter := req.GetString("filter", "")

	alerts, err := d.c.AlertmanagerAlerts(ctx, sourceID, filter)
	if err != nil {
		return errorResult(fmt.Errorf("incidents query failed: %w", err)), nil
	}

	return jsonResult(map[string]any{
		"alerts": alerts,
		"count":  len(alerts),
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

func textResult(text string) *mcpgo.CallToolResult {
	return &mcpgo.CallToolResult{
		Content: []mcpgo.Content{
			mcpgo.TextContent{
				Type: "text",
				Text: text,
			},
		},
	}
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
