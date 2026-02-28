// joe-mcp is a Model Context Protocol server that exposes Joe's
// infrastructure intelligence to MCP clients (Claude Code, Cursor, Codex).
//
// It reads joecored connection details from environment variables:
//
//	JOE_SERVER  — joecored base URL (default: http://localhost:7777)
//	JOE_API_KEY — Bearer token for joecored API auth (optional)
//
// Start it as an MCP server by adding it to your MCP client config:
//
//	{
//	  "mcpServers": {
//	    "joe": {
//	      "command": "joe-mcp",
//	      "env": { "JOE_SERVER": "http://localhost:7777", "JOE_API_KEY": "<token>" }
//	    }
//	  }
//	}
package main

import (
	"fmt"
	"log"
	"os"

	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/jaimegago/joe/internal/client"
	"github.com/jaimegago/joe/internal/mcp"
)

func main() {
	serverURL := os.Getenv("JOE_SERVER")
	if serverURL == "" {
		serverURL = "http://localhost:7777"
	}
	apiKey := os.Getenv("JOE_API_KEY")

	var opts []client.ClientOption
	if apiKey != "" {
		opts = append(opts, client.WithAPIKey(apiKey))
	}

	coreClient := client.New(serverURL, opts...)

	s := mcp.NewServer(coreClient)

	fmt.Fprintf(os.Stderr, "joe-mcp: connecting to joecored at %s\n", serverURL)

	if err := mcpserver.ServeStdio(s); err != nil {
		log.Fatalf("joe-mcp: server error: %v", err)
	}
}
