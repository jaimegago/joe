package tools

import (
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
