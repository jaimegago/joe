package tools

import (
	"github.com/jaimegago/joe/internal/tools/local/askuser"
	"github.com/jaimegago/joe/internal/tools/local/echo"
)

// NewDefaultRegistry creates a registry with all default tools registered
// These tools are useful for the agentic loop and testing
func NewDefaultRegistry() *Registry {
	registry := NewRegistry()

	// Register basic tools
	registry.Register(echo.NewTool())
	registry.Register(askuser.NewTool())

	return registry
}
