package store

import (
	"context"
	"time"
)

// Store is the interface for SQL storage (SQLite)
type Store interface {
	// Sources
	AddSource(ctx context.Context, source Source) error
	GetSource(ctx context.Context, id string) (*Source, error)
	ListSources(ctx context.Context) ([]Source, error)
	UpdateSource(ctx context.Context, source Source) error
	DeleteSource(ctx context.Context, id string) error

	// Sessions
	CreateSession(ctx context.Context, session Session) error
	GetSession(ctx context.Context, id string) (*Session, error)
	UpdateSession(ctx context.Context, session Session) error

	// Cache
	GetJoeFileCache(ctx context.Context, repoID, hash string) (*JoeFileCache, error)
	SetJoeFileCache(ctx context.Context, cache JoeFileCache) error

	// Close the store
	Close() error
}

// Source represents an infrastructure source
type Source struct {
	ID                string
	Type              string
	URL               string
	Name              string
	Environment       string
	Categories        []string
	ConnectionDetails map[string]any
	Status            string
	LastConnected     *time.Time
	DiscoveredFrom    string
	DiscoveryContext  string
	Metadata          map[string]any
	CreatedAt         time.Time
}

// Session represents a conversation session
type Session struct {
	ID         string
	StartedAt  time.Time
	EndedAt    *time.Time
	Summary    string
	Issue      string
	RootCause  string
	Resolution string
	Components []string
	Tags       []string
	Embedding  []float32
}

// JoeFileCache stores cached interpretations of .joe/ files
type JoeFileCache struct {
	RepoID     string
	JoeDirHash string
	ToolCalls  []CachedToolCall
	CachedAt   time.Time
	LLMModel   string
}

// CachedToolCall represents a cached tool call from .joe/ file interpretation
type CachedToolCall struct {
	Tool string
	Args map[string]any
}
