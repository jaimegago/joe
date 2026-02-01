package joe

import (
	"context"

	"github.com/jaimegago/joe/internal/agent"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/session"
	"github.com/jaimegago/joe/internal/store"
	"github.com/jaimegago/joe/internal/tools"
)

// Joe is the core struct that orchestrates everything
type Joe struct {
	config     *config.Config
	llm        llm.LLMAdapter
	graph      graph.GraphStore
	store      store.Store
	executor   *tools.Executor
	registry   *tools.Registry
	agent      *agent.Agent
	sessionMgr *session.Manager
}

// New creates a new Joe instance
func New(cfg *config.Config, llmAdapter llm.LLMAdapter, graphStore graph.GraphStore, sqlStore store.Store) *Joe {
	registry := tools.NewRegistry()
	executor := tools.NewExecutor(registry)
	sessionMgr := session.NewManager()
	agentInstance := agent.NewAgent(llmAdapter, executor, registry)

	return &Joe{
		config:     cfg,
		llm:        llmAdapter,
		graph:      graphStore,
		store:      sqlStore,
		executor:   executor,
		registry:   registry,
		agent:      agentInstance,
		sessionMgr: sessionMgr,
	}
}

// Chat handles a chat message and returns a streaming response
func (j *Joe) Chat(ctx context.Context, sessionID, message string) (<-chan string, error) {
	return j.agent.Run(ctx, sessionID, message)
}

// Init runs the onboarding flow
func (j *Joe) Init(ctx context.Context) error {
	// TODO: Implement onboarding
	return nil
}

// Status returns the current status
func (j *Joe) Status() Status {
	// TODO: Implement status
	return Status{}
}

// Sources returns all registered sources
func (j *Joe) Sources() ([]store.Source, error) {
	return j.store.ListSources(context.Background())
}

// Refresh triggers a manual refresh
func (j *Joe) Refresh(ctx context.Context) error {
	// TODO: Implement refresh
	return nil
}

// Start starts background services
func (j *Joe) Start(ctx context.Context) error {
	// TODO: Implement background refresh goroutine
	return nil
}

// Stop gracefully shuts down Joe
func (j *Joe) Stop() error {
	// TODO: Implement graceful shutdown
	return nil
}

// Status represents Joe's current status
type Status struct {
	GraphNodes       int
	GraphEdges       int
	ConnectedSources int
	LastRefresh      string
}
