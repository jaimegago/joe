package core

import (
	"context"
	"database/sql"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/findings"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/knowledge"
	"github.com/jaimegago/joe/internal/knowledge/drafts"
	"github.com/jaimegago/joe/internal/knowledge/drift"
	"github.com/jaimegago/joe/internal/knowledge/proposals"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/observability"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/review"
	"github.com/jaimegago/joe/internal/runmodel"
	"github.com/jaimegago/joe/internal/sessionmodel"
	"github.com/jaimegago/joe/internal/skills"
	"github.com/jaimegago/joe/internal/store"
	"github.com/jaimegago/joe/internal/warnings"
)

// CoreAgent interface for control operations
type CoreAgent interface {
	ProcessOnboarding(ctx context.Context, input string) error
	TriggerRefresh(ctx context.Context) error
	TriggerRefreshSource(ctx context.Context, sourceID string) error
}

// Services provides access to all core functionality.
// Used by both the API handlers and the Core Agent.
type Services struct {
	Config         *config.Config
	LLM            llm.LLMAdapter
	Graph          graph.GraphStore
	Store          *store.Store
	Agent          CoreAgent // Core Agent instance for control endpoints
	Adapters       *adapters.Registry
	Metrics        *observability.Metrics
	Clarifications *ClarificationService
	Knowledge      *knowledge.Service
	Proposals      *proposals.Service
	DocDrafter     *drafts.Generator
	DriftDet       *drift.Detector
	RBAC           rbac.Repository         // nil when RBAC is not configured
	SessionModel   sessionmodel.Repository // nil until wired in cmd/joe-core/main.go
	RunModel       runmodel.Repository     // nil until wired in cmd/joe-core/main.go
	Findings       findings.Repository     // nil until wired in cmd/joe-core/main.go
	Warnings       warnings.Repository     // nil until wired in cmd/joe-core/main.go
	Review         *review.Service         // nil when code review is not configured
	ReviewAgent    *review.ReviewAgent     // nil when review agent is not configured
	Skills         *skills.AtomicRouter    // never nil after wiring; Snapshot() may return nil
	SkillsWatcher  *skills.Watcher         // nil when hot reload is disabled or failed to start
	// SkillsManager owns ~/.joe/skills/ and the lockfile. Used by the
	// admin API (POST /api/v1/skills/approve, GET /api/v1/skills). It is
	// nil only when joecored started without ever resolving its joe-dir,
	// which is an error case the API handlers report as 503.
	SkillsManager *skills.Manager
}

// New creates a new Services instance with the given SQL store database.
// driver is the database driver name (store.DriverSQLite or store.DriverPostgres).
func New(cfg *config.Config, sqlStore *store.Store, db *sql.DB, driver string, adapterRegistry *adapters.Registry, metrics *observability.Metrics) *Services {
	metrics = observability.EnsureMetrics(metrics)
	graphStore := graph.NewSQLStore(db, driver, metrics)
	// Knowledge service starts without an embedder; one is attached later via
	// services.Knowledge = knowledge.NewService(repo, embedder) once the LLM
	// adapter is wired in cmd/joecored/main.go.
	knowledgeSvc := knowledge.NewService(sqlStore.Knowledge, nil)
	proposalRepo := proposals.NewRepository(db, driver)
	proposalSvc := proposals.NewService(proposalRepo)
	driftDet := drift.New(knowledgeSvc)
	reviewRepo := review.NewRepository(db, driver)
	reviewSvc := review.NewService(reviewRepo)
	return &Services{
		Config:         cfg,
		Store:          sqlStore,
		Graph:          graphStore,
		Adapters:       adapterRegistry,
		Metrics:        metrics,
		Clarifications: NewClarificationService(graphStore, sqlStore),
		Knowledge:      knowledgeSvc,
		Proposals:      proposalSvc,
		DriftDet:       driftDet,
		Review:         reviewSvc,
		// DocDrafter and ReviewAgent are wired later in cmd/joecored/main.go.
	}
}

// Close cleans up resources.
func (s *Services) Close() error {
	// Store is closed by the caller (joecored main) who owns the lifecycle.
	return nil
}
