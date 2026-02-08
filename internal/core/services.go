package core

import (
	"database/sql"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/store"
)

// Services provides access to all core functionality.
// Used by both the API handlers and the Core Agent.
type Services struct {
	Config   *config.Config
	LLM      llm.LLMAdapter
	Graph    graph.GraphStore
	Store    *store.Store
	Adapters *adapters.Registry
}

// New creates a new Services instance with the given SQL store database.
// The db is used for both the SQL store and the SQLite-backed graph store.
func New(cfg *config.Config, sqlStore *store.Store, db *sql.DB, adapterRegistry *adapters.Registry) *Services {
	return &Services{
		Config:   cfg,
		Store:    sqlStore,
		Graph:    graph.NewSQLiteStore(db),
		Adapters: adapterRegistry,
	}
}

// Close cleans up resources.
func (s *Services) Close() error {
	// Store is closed by the caller (joecored main) who owns the lifecycle.
	return nil
}
