package core

import (
	"context"
	"database/sql"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/auth"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/findings"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/knowledge"
	"github.com/jaimegago/joe/internal/knowledge/drafts"
	"github.com/jaimegago/joe/internal/knowledge/drift"
	"github.com/jaimegago/joe/internal/knowledge/proposals"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/llmsettings"
	"github.com/jaimegago/joe/internal/llmusage"
	"github.com/jaimegago/joe/internal/observability"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/runmodel"
	"github.com/jaimegago/joe/internal/safety"
	"github.com/jaimegago/joe/internal/sessionmodel"
	"github.com/jaimegago/joe/internal/skills"
	"github.com/jaimegago/joe/internal/store"
	"github.com/jaimegago/joe/internal/warnings"
)

// CoreAgent interface for control operations
type CoreAgent interface {
	ProcessOnboarding(ctx context.Context, input string) error
	TriggerRefresh(ctx context.Context) error
	TriggerRefreshComponent(ctx context.Context, sourceID string) error
}

// Services provides access to all core functionality.
// Used by both the API handlers and the Core Agent.
type Services struct {
	Config *config.Config
	LLM    llm.LLMAdapter
	Graph  graph.GraphStore
	Store  *store.Store
	Agent  CoreAgent // Core Agent instance for control endpoints
	// WriteFloor is the boot-resolved, runtime-immutable write floor (D-0018).
	// Resolved exactly once in cmd/joe/server.go from the panic state (sticky)
	// and the JOE_MODE=observation env var, then stored here as the single
	// process-wide source. Read by the tool executors — which deny Mutate when
	// it is up — and by the panic status handler. The zero value is "down".
	// There is no setter that lowers it; recovery is restart.
	WriteFloor     safety.WriteFloor
	Adapters       *adapters.Registry
	Metrics        *observability.Metrics
	Clarifications *ClarificationService
	Knowledge      *knowledge.Service
	Proposals      *proposals.Service
	DocDrafter     *drafts.Generator
	DriftDet       *drift.Detector
	RBAC           rbac.Repository // nil when RBAC is not configured
	// Principals is the authoritative identity registry (migration 021),
	// satisfied by the same *rbac.SQLRepository wired into RBAC. Read path for
	// the admin Users page (GET /api/v1/admin/principals). nil when RBAC is not
	// configured.
	Principals rbac.PrincipalRepository
	// Provisioner is the admin-grant seam (auth.Provisioner.GrantAdmin): grant
	// admin authority AND clean up the principal's redundant policy grants in
	// one call. The admin-add REST handler (POST /api/v1/admin/admins) wraps it
	// so the grant-plus-cleanup invariant is not re-implemented. nil when RBAC
	// is not configured.
	Provisioner *auth.Provisioner
	// PrincipalAdmin orchestrates principal disable/enable across the identity
	// registry and the session store (auth.PrincipalAdmin): Disable sets status
	// then revokes live sessions; Enable restores status. The disable/enable
	// REST handlers wrap it. nil when RBAC is not configured.
	PrincipalAdmin *auth.PrincipalAdmin
	// RBACEnabled mirrors the predicate the policy engine is built
	// from in cmd/joe/server.go: true exactly when a real caller
	// principal can be established (a service account OR OIDC is
	// configured). It is the SAME predicate the accessor uses to
	// decide whether to short-circuit its rbac-disabled allow-path and
	// the same condition that makes rbac.PolicyEngine non-nil. Set
	// once at the engine-build site (Stream G phase G5) so HTTP
	// handlers — the current-user endpoint and the admin gate — can
	// answer "is enforcement active right now?" without re-deriving
	// the predicate from config. A nil PolicyEngine and RBACEnabled
	// false are equivalent statements about the same configuration.
	RBACEnabled bool
	// OIDCEnabled mirrors cfg.Auth.OIDC.Configured() — true exactly when
	// an OIDC issuer is configured and the login/callback/logout
	// endpoints are registered (Stream H2). Set once at the same
	// main.go build site that computes oidcConfigured, so the
	// current-user handler can tell the Web UI whether to offer the OIDC
	// login button without re-deriving the predicate from config. There
	// is deliberately no second auth-config endpoint: /me already
	// reports the per-caller auth facts and this rides alongside them.
	OIDCEnabled bool
	// Audit is the append-only audit trail (Identity Phase F,
	// docs/joe-identity-design.md §2.6). Wired by cmd/joe/server.go after
	// the store migrations run. Consumers: the guarded accessor
	// (internal/access) writes one row per authorization decision; the
	// regime and captain handlers (internal/api) write durable rows for
	// declare/resolve/attach/transfer so incident history survives resolve
	// (bug #3). Insert-only by interface; UPDATE/DELETE are also blocked at
	// the database via migration 015 triggers.
	Audit audit.Repository
	// LLMUsage records one row per LLM Chat call (Stream G phase G2,
	// migration 017). Wired by cmd/joe/server.go alongside the other
	// store-backed repositories; nil in unit-test harnesses that don't
	// need the recorder. Insert-only by interface — usage rows are an
	// observability log, and the recorder is fail-open against this
	// surface.
	LLMUsage llmusage.Repository
	// LLMSettings is the storage-backed settings service (Stream G
	// phase G4). Owns reads and atomic writes against the three
	// settings tables (llm_settings, llm_cost_limits,
	// llm_runaway_limits) created in migration 017. Every mutation
	// commits with its audit row through audit.Repository.InsertTx —
	// either both rows land or neither does. nil in unit-test
	// harnesses that don't exercise model switching or limit edits.
	LLMSettings *llmsettings.MutationService
	// SessionLimitsProvider is the storage-backed agentloop.SessionLimits
	// the per-task agent construction reads from. Built once at startup
	// and shared across tasks so a per-task agent construction does not
	// need to know how the limit is sourced — same interface, swapped
	// implementation.
	SessionLimitsProvider *llmsettings.SessionLimitsProvider
	// CostLimitsProvider is the SAME storage-backed llmusage.CostLimits
	// instance the recorder's cost-window gate enforces with (wired into
	// llmusage.NewRecorderAdapter as Limits in cmd/joe/server.go). The
	// settings GET handler reads it to surface the effective enforced
	// value — backstop-substituted for an unset window — so the displayed
	// number is sourced from the same object the gate decides with and
	// cannot drift from it. nil in unit-test harnesses that don't
	// exercise the settings GET endpoint.
	CostLimitsProvider *llmsettings.CostLimitsProvider
	// ContextBudgetProvider is the storage-backed agentloop.ContextBudget
	// the per-task agent construction reads the context-budget fraction
	// from. Built once at startup and shared across tasks (re-reads the DB
	// per call), so a fraction changed through the settings API takes effect
	// on the next message without a restart. nil in unit-test harnesses that
	// don't exercise context budgeting (buildTaskRun then falls back to the
	// static backstop fraction).
	ContextBudgetProvider *llmsettings.ContextBudgetProvider
	SessionModel          sessionmodel.Repository      // nil until wired in cmd/joe/server.go
	RunModel              runmodel.Repository          // nil until wired in cmd/joe/server.go
	Findings              findings.Repository          // nil until wired in cmd/joe/server.go
	Warnings              warnings.Repository          // nil until wired in cmd/joe/server.go
	CaptainSvc            *sessionmodel.CaptainService // nil until wired in cmd/joe/server.go
	Skills                *skills.AtomicRouter         // never nil after wiring; Snapshot() may return nil
	SkillsWatcher         *skills.Watcher              // nil when hot reload is disabled or failed to start
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
		// DocDrafter is wired later in cmd/joecored/main.go.
	}
}

// Close cleans up resources.
func (s *Services) Close() error {
	// Store is closed by the caller (joecored main) who owns the lifecycle.
	return nil
}
