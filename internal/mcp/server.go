// Package mcp implements a Model Context Protocol server that exposes
// Joe's infrastructure intelligence to MCP clients such as Claude Code,
// Cursor, and Codex.
package mcp

import (
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/jaimegago/joe/internal/client"
)

const (
	serverName    = "joe"
	serverVersion = "0.1.0"
)

// NewServer creates an MCP server with all Joe tools registered.
// The returned server is ready to be started with ServeStdio.
func NewServer(c *client.Client) *mcpserver.MCPServer {
	s := mcpserver.NewMCPServer(
		serverName,
		serverVersion,
		mcpserver.WithToolCapabilities(false),
	)

	d := NewDispatcher(c)

	for _, tool := range toolDefs() {
		switch tool.Name {
		case "joe_graph_query":
			s.AddTool(tool, d.HandleGraphQuery)
		case "joe_graph_related":
			s.AddTool(tool, d.HandleGraphRelated)
		case "joe_k8s":
			s.AddTool(tool, d.HandleK8s)
		case "joe_metrics":
			s.AddTool(tool, d.HandleMetrics)
		case "joe_logs":
			s.AddTool(tool, d.HandleLogs)
		case "joe_traces":
			s.AddTool(tool, d.HandleTraces)
		case "joe_alerts":
			s.AddTool(tool, d.HandleAlerts)
		case "joe_knowledge_search":
			s.AddTool(tool, d.HandleKnowledgeSearch)
		}
	}

	return s
}
