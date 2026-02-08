package tools

import (
	"github.com/jaimegago/joe/internal/client"
	coretools "github.com/jaimegago/joe/internal/tools/core"
	"github.com/jaimegago/joe/internal/tools/local/askuser"
	"github.com/jaimegago/joe/internal/tools/local/echo"
	"github.com/jaimegago/joe/internal/tools/local/gitdiff"
	"github.com/jaimegago/joe/internal/tools/local/gitstatus"
	"github.com/jaimegago/joe/internal/tools/local/readfile"
	"github.com/jaimegago/joe/internal/tools/local/runcmd"
	"github.com/jaimegago/joe/internal/tools/local/writefile"
)

// NewDefaultRegistry creates a registry with all default tools registered
// These tools are useful for the agentic loop and testing
func NewDefaultRegistry() *Registry {
	registry := NewRegistry()

	// Register basic tools
	registry.Register(echo.NewTool())
	registry.Register(askuser.NewTool())

	// Register file tools
	registry.Register(readfile.New())
	registry.Register(writefile.New())

	// Register git tools
	registry.Register(gitstatus.New())
	registry.Register(gitdiff.New())

	// Register command runner (with safe defaults)
	registry.Register(runcmd.New([]string{
		"ls", "cat", "head", "tail", "grep", "find", "wc",
		"kubectl", "helm", "argocd",
	}))

	return registry
}

// NewDefaultRegistryWithClient creates a registry with all default tools plus
// core tools that communicate with joecored via the HTTP client.
func NewDefaultRegistryWithClient(coreClient *client.Client) *Registry {
	registry := NewDefaultRegistry()

	// Register core tools (call joecored API)
	registry.Register(coretools.NewGraphQueryTool(coreClient))
	registry.Register(coretools.NewGraphRelatedTool(coreClient))
	registry.Register(coretools.NewK8sGetTool(coreClient))
	registry.Register(coretools.NewK8sLogsTool(coreClient))

	return registry
}
