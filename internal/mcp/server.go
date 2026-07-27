// Package mcp implements a Model Context Protocol server that exposes
// Joe's infrastructure intelligence to MCP clients such as Claude Code,
// Cursor, and Codex.
package mcp

import (
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/jaimegago/joe/internal/buildinfo"
	"github.com/jaimegago/joe/internal/client"
)

const serverName = "joe"

// NewServer creates an MCP server with all Joe tools registered.
// The returned server is ready to be started with ServeStdio.
//
// The version reported in the MCP handshake is this binary's build identity,
// read from internal/buildinfo (the single source of build truth), so it cannot
// drift from the built artifact. It is unrelated to the MCP protocol version,
// which the mcp-go library states on its own.
func NewServer(c *client.Client) *mcpserver.MCPServer {
	s := mcpserver.NewMCPServer(
		serverName,
		buildinfo.Get().Version,
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
		}
	}

	return s
}
