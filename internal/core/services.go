package core

import (
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/store"
)

// Services provides access to all core functionality
// Used by both the API handlers and the Core Agent
type Services struct {
	Config *config.Config
	LLM    llm.LLMAdapter
	Graph  graph.GraphStore
	Store  store.Store
}

// New creates a new Services instance
// For now this is a placeholder - we'll wire up real implementations in later phases
func New(cfg *config.Config) (*Services, error) {
	return &Services{
		Config: cfg,
	}, nil
}

// Close cleans up resources
func (s *Services) Close() error {
	// TODO: Close LLM, Graph, Store connections
	return nil
}
