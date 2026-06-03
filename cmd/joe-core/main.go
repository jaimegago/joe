package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/jaimegago/joe/internal/adapters"
	awsadapter "github.com/jaimegago/joe/internal/adapters/aws"
	azureadapter "github.com/jaimegago/joe/internal/adapters/azure"
	gitadapter "github.com/jaimegago/joe/internal/adapters/git"
	githubadapter "github.com/jaimegago/joe/internal/adapters/github"
	gitlabadapter "github.com/jaimegago/joe/internal/adapters/gitlab"
	"github.com/jaimegago/joe/internal/adapters/k8s"
	datadogadapter "github.com/jaimegago/joe/internal/adapters/observability/datadog"
	dynatraceadapter "github.com/jaimegago/joe/internal/adapters/observability/dynatrace"
	newrelicadapter "github.com/jaimegago/joe/internal/adapters/observability/newrelic"
	splunkadapter "github.com/jaimegago/joe/internal/adapters/observability/splunk"
	falcoadapter "github.com/jaimegago/joe/internal/adapters/security/falco"
	"github.com/jaimegago/joe/internal/agentloop"
	"github.com/jaimegago/joe/internal/api"
	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/auth"
	"github.com/jaimegago/joe/internal/captaingate"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/coreagent"
	"github.com/jaimegago/joe/internal/crypto"
	"github.com/jaimegago/joe/internal/findings"
	"github.com/jaimegago/joe/internal/knowledge"
	"github.com/jaimegago/joe/internal/knowledge/drafts"
	"github.com/jaimegago/joe/internal/knowledge/embeddings"
	knowledgesync "github.com/jaimegago/joe/internal/knowledge/sync"
	"github.com/jaimegago/joe/internal/knowledge/sync/confluence"
	"github.com/jaimegago/joe/internal/knowledge/sync/notion"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/llmfactory"
	"github.com/jaimegago/joe/internal/llmsettings"
	"github.com/jaimegago/joe/internal/llmusage"
	"github.com/jaimegago/joe/internal/logging"
	"github.com/jaimegago/joe/internal/observability"
	"github.com/jaimegago/joe/internal/paths"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/review"
	"github.com/jaimegago/joe/internal/runmodel"
	"github.com/jaimegago/joe/internal/safety"
	"github.com/jaimegago/joe/internal/sessionmodel"
	"github.com/jaimegago/joe/internal/skills"
	"github.com/jaimegago/joe/internal/store"
	"github.com/jaimegago/joe/internal/warnings"
	"github.com/jaimegago/joe/internal/webui"
)

// version is set at build time via ldflags:
//
//	go build -ldflags "-X main.version=1.2.3" ./cmd/joe-core
var version string

type coreAgentRunner interface {
	core.CoreAgent
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// adapterRegistryOps wraps the adapter registry to implement review.GitHubOps and review.GitLabOps.
// It looks up the appropriate adapter by sourceID and delegates the call.
type adapterRegistryOps struct {
	registry *adapters.Registry
}

func (o adapterRegistryOps) GitHubGetPR(ctx context.Context, sourceID, owner, repo string, number int) (*githubadapter.PRInfo, error) {
	a, err := o.registry.Get(sourceID)
	if err != nil {
		return nil, fmt.Errorf("github adapter %q: %w", sourceID, err)
	}
	gh, ok := a.(githubadapter.GitHubAdapter)
	if !ok {
		return nil, fmt.Errorf("source %q is not a GitHub adapter", sourceID)
	}
	return gh.GetPR(ctx, owner, repo, number)
}

func (o adapterRegistryOps) GitHubGetPRDiff(ctx context.Context, sourceID, owner, repo string, number int) (string, error) {
	a, err := o.registry.Get(sourceID)
	if err != nil {
		return "", fmt.Errorf("github adapter %q: %w", sourceID, err)
	}
	gh, ok := a.(githubadapter.GitHubAdapter)
	if !ok {
		return "", fmt.Errorf("source %q is not a GitHub adapter", sourceID)
	}
	return gh.GetPRDiff(ctx, owner, repo, number)
}

func (o adapterRegistryOps) GitHubPostComment(ctx context.Context, sourceID, owner, repo string, number int, body string) error {
	a, err := o.registry.Get(sourceID)
	if err != nil {
		return fmt.Errorf("github adapter %q: %w", sourceID, err)
	}
	gh, ok := a.(githubadapter.GitHubAdapter)
	if !ok {
		return fmt.Errorf("source %q is not a GitHub adapter", sourceID)
	}
	return gh.PostComment(ctx, owner, repo, number, body)
}

func (o adapterRegistryOps) GitLabGetMR(ctx context.Context, sourceID, projectID string, iid int) (*gitlabadapter.MRInfo, error) {
	a, err := o.registry.Get(sourceID)
	if err != nil {
		return nil, fmt.Errorf("gitlab adapter %q: %w", sourceID, err)
	}
	gl, ok := a.(gitlabadapter.GitLabAdapter)
	if !ok {
		return nil, fmt.Errorf("source %q is not a GitLab adapter", sourceID)
	}
	return gl.GetMR(ctx, projectID, iid)
}

func (o adapterRegistryOps) GitLabGetMRDiff(ctx context.Context, sourceID, projectID string, iid int) (string, error) {
	a, err := o.registry.Get(sourceID)
	if err != nil {
		return "", fmt.Errorf("gitlab adapter %q: %w", sourceID, err)
	}
	gl, ok := a.(gitlabadapter.GitLabAdapter)
	if !ok {
		return "", fmt.Errorf("source %q is not a GitLab adapter", sourceID)
	}
	return gl.GetMRDiff(ctx, projectID, iid)
}

func (o adapterRegistryOps) GitLabPostNote(ctx context.Context, sourceID, projectID string, iid int, body string) error {
	a, err := o.registry.Get(sourceID)
	if err != nil {
		return fmt.Errorf("gitlab adapter %q: %w", sourceID, err)
	}
	gl, ok := a.(gitlabadapter.GitLabAdapter)
	if !ok {
		return fmt.Errorf("source %q is not a GitLab adapter", sourceID)
	}
	return gl.PostNote(ctx, projectID, iid, body)
}

type runDeps struct {
	// configPath is the resolved path to the config file. Empty means "use the
	// default ~/.joe/config.yaml". run() fills it from the --config flag /
	// JOE_CONFIG env; tests that drive runWithDeps directly leave it empty to
	// get the default-path behaviour.
	configPath             string
	loadConfig             func(path string) (*config.Config, error)
	setupOTel              func(ctx context.Context, cfg observability.Config) (func(context.Context) error, error)
	defaultOTelConfig      func() observability.Config
	newMetrics             func() *observability.Metrics
	joeDirPath             func() (string, error)
	mkdirAll               func(path string, perm os.FileMode) error
	databasePath           func() (string, error)
	newStore               func(cfg store.DatabaseConfig, metrics *observability.Metrics) (*store.Store, error)
	migrateStore           func(store *store.Store) error
	closeStore             func(store *store.Store) error
	newAdapterRegistry     func() *adapters.Registry
	connectSources         func(ctx context.Context, store *store.Store, registry *adapters.Registry)
	newServices            func(cfg *config.Config, store *store.Store, db *sql.DB, driver string, registry *adapters.Registry, metrics *observability.Metrics) *core.Services
	registerBusinessMetric func(services *core.Services) error
	newLLMAdapter          func(ctx context.Context, mc config.ModelConfig) (llm.LLMAdapter, error)
	newCoreAgent           func(services *core.Services, llmAdapter llm.LLMAdapter, metrics *observability.Metrics) coreAgentRunner
	newAPIServer           func(services *core.Services) *api.Server
	startServer            func(server *http.Server, certFile, keyFile string) <-chan error
	shutdownServer         func(ctx context.Context, server *http.Server) error
	startMetricsServer     func(server *http.Server) error
	shutdownMetricsServer  func(ctx context.Context, server *http.Server) error
	waitForShutdown        func(ctx context.Context) <-chan struct{}
}

func defaultRunDeps() runDeps {
	return runDeps{
		loadConfig:        config.Load,
		setupOTel:         observability.Setup,
		defaultOTelConfig: observability.DefaultConfig,
		newMetrics:        observability.NewMetrics,
		joeDirPath:        paths.JoeDirPath,
		mkdirAll:          os.MkdirAll,
		databasePath:      paths.DatabasePath,
		newStore:          store.New,
		migrateStore: func(store *store.Store) error {
			return store.Migrate()
		},
		closeStore: func(store *store.Store) error {
			return store.Close()
		},
		newAdapterRegistry: adapters.NewRegistry,
		connectSources:     connectSourcesDefault,
		newServices:        core.New,
		registerBusinessMetric: func(services *core.Services) error {
			return registerBusinessMetricsDefault(services)
		},
		newLLMAdapter: llmfactory.NewAdapter,
		newCoreAgent: func(services *core.Services, llmAdapter llm.LLMAdapter, metrics *observability.Metrics) coreAgentRunner {
			return coreagent.New(services, llmAdapter, metrics)
		},
		newAPIServer:          api.New,
		startServer:           defaultStartServer,
		shutdownServer:        func(ctx context.Context, server *http.Server) error { return server.Shutdown(ctx) },
		startMetricsServer:    defaultStartMetricsServer,
		shutdownMetricsServer: func(ctx context.Context, server *http.Server) error { return server.Shutdown(ctx) },
		waitForShutdown:       defaultWaitForShutdown,
	}
}

func run(ctx context.Context) int {
	deps := defaultRunDeps()
	deps.configPath = resolveConfigPath(os.Args[1:])
	return runWithDeps(ctx, deps)
}

// resolveConfigPath determines which config file joe-core loads, in descending
// precedence: the --config flag, then the JOE_CONFIG environment variable, then
// "" (the caller falls back to the default ~/.joe/config.yaml). It parses a
// private FlagSet over the given args rather than the global flag.CommandLine so
// it never collides with go test's flags.
func resolveConfigPath(args []string) string {
	fs := flag.NewFlagSet("joe-core", flag.ContinueOnError)
	var configFlag string
	fs.StringVar(&configFlag, "config", "",
		"path to the config file (overrides JOE_CONFIG and the default ~/.joe/config.yaml)")
	// A parse error (unknown flag, -h) is non-fatal: fall through to env/default
	// so the daemon still boots. fs already wrote any usage text to stderr.
	_ = fs.Parse(args)

	if configFlag != "" {
		return configFlag
	}
	if env := os.Getenv("JOE_CONFIG"); env != "" {
		return env
	}
	return ""
}

func runWithDeps(ctx context.Context, deps runDeps) int {
	// Setup initial logger at info level
	initialLogger := logging.SetupLogger("info")
	slog.SetDefault(initialLogger)

	// Load config from the resolved path (--config / JOE_CONFIG), falling back to
	// the default ~/.joe/config.yaml. A missing file is not fatal — config.Load
	// uses hardcoded defaults in that case.
	configPath := deps.configPath
	if configPath == "" {
		configPath = paths.DefaultConfigPath()
	}
	cfg, err := deps.loadConfig(configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err, "path", configPath)
		return 1
	}

	// Reconfigure logger based on config level
	logger := logging.SetupLogger(cfg.Logging.Level)
	slog.SetDefault(logger)

	// Log debug mode if enabled
	if cfg.Logging.Level == logging.LevelDebug {
		slog.Debug("running in debug mode")
	}

	// Auto-select the LLM provider from whichever key is present when the user
	// expressed no explicit preference, so a stranger with exactly one provider
	// key can start with zero config. An explicit choice always wins.
	if err := cfg.AutoSelectProvider(); err != nil {
		slog.Error("no usable LLM provider", "error", err)
		return 1
	}

	// Log configuration
	currentModel, modelErr := cfg.LLM.CurrentModel()
	modelInfo := "none"
	if modelErr == nil {
		modelInfo = fmt.Sprintf("%s/%s", currentModel.Provider, currentModel.Model)
	}
	slog.Info("configuration loaded",
		"server.address", cfg.Server.Address,
		"refresh.interval_minutes", cfg.Refresh.IntervalMinutes,
		"logging.level", cfg.Logging.Level,
		"llm.model", modelInfo,
	)

	// Initialize OpenTelemetry
	otelCfg := deps.defaultOTelConfig()
	shutdownOTel, err := deps.setupOTel(ctx, otelCfg)
	if err != nil {
		slog.Warn("OpenTelemetry setup failed", "error", err)
	} else {
		defer func() { _ = shutdownOTel(context.Background()) }()
	}

	// Create metrics instance
	metrics := deps.newMetrics()

	// Initialize store
	joeDir, err := deps.joeDirPath()
	if err != nil {
		slog.Error("failed to get joe directory path", "error", err)
		return 1
	}

	if err := deps.mkdirAll(joeDir, 0755); err != nil {
		slog.Error("failed to create joe directory", "error", err)
		return 1
	}

	dbPath, err := deps.databasePath()
	if err != nil {
		slog.Error("failed to get database path", "error", err)
		return 1
	}

	dbCfg := store.DatabaseConfig{
		Driver: store.DriverSQLite,
		DSN:    dbPath,
	}
	if cfg.Database.Driver != "" {
		dbCfg.Driver = cfg.Database.Driver
	}
	if cfg.Database.DSN != "" {
		dbCfg.DSN = cfg.Database.DSN
	}
	sqlStore, err := deps.newStore(dbCfg, metrics)
	if err != nil {
		slog.Error("failed to open database", "error", err)
		return 1
	}
	defer func() {
		_ = deps.closeStore(sqlStore)
	}()

	if err := deps.migrateStore(sqlStore); err != nil {
		slog.Error("failed to run migrations", "error", err)
		return 1
	}
	slog.Info("database ready", "path", dbCfg.DSN)

	// Wire RBAC repository (uses the same SQLite DB, tables created by migration 006).
	rbacRepo := rbac.NewRepository(sqlStore.DB(), sqlStore.Driver())

	// Wire the append-only audit log (Identity Phase F, migration 015).
	// Every authorization decision the accessor makes and every
	// regime/captain transition writes one row here.
	auditRepo := audit.NewRepository(sqlStore.DB(), sqlStore.Driver())

	// Wire the per-call LLM usage repository (Stream G phase G2,
	// migration 017). The recorder wrapper installed around the raw
	// llm adapter below uses this repo to write one row per Chat call.
	// Insert-only by interface — there is no UPDATE/DELETE path.
	llmUsageRepo := llmusage.NewRepository(sqlStore.DB(), sqlStore.Driver())

	// Wire the LLM settings repository, the storage-backed limit
	// providers, and the mutation service (Stream G phase G4,
	// migration 017). Layered:
	//
	//   - llmSettingsRepo: reads + transactional UPDATEs on the three
	//     settings tables.
	//   - storage-backed CostLimits / SessionLimits providers: drop-
	//     in replacements for the static providers at the enforcement
	//     check sites (recorder cost-window gate, agentloop runaway
	//     gate). Unset (zero) values fall back to the conservative
	//     hardcoded backstop — a freshly migrated system stays
	//     protected.
	//   - llmSettingsSvc: the SOLE write path. Every mutation commits
	//     atomically with one llm_settings_mutation audit row through
	//     audit.Repository.InsertTx.
	llmSettingsRepo := llmsettings.NewRepository(sqlStore.DB(), sqlStore.Driver())
	costLimitsProvider := llmsettings.NewCostLimitsProvider(llmSettingsRepo, llmusage.NewStaticCostLimits(), slog.Default())
	sessionLimitsProvider := llmsettings.NewSessionLimitsProvider(llmSettingsRepo, agentloop.NewStaticSessionLimits(), slog.Default())
	llmSettingsSvc := llmsettings.NewMutationService(llmSettingsRepo, auditRepo)

	// Wire session-model repository (tables created by migration 009).
	// Phase 1 Change 1 — see docs/PHASE-1-DECOMPOSITION.md.
	sessionModelRepo := sessionmodel.NewRepository(sqlStore.DB(), sqlStore.Driver())

	// Wire run-model repository (tables created by migration 010).
	// Phase 1 Change 2 — see docs/PHASE-1-DECOMPOSITION.md.
	runModelRepo := runmodel.NewRepository(sqlStore.DB(), sqlStore.Driver())

	// Wire findings + warnings repositories (tables created by migration 011).
	// Phase 1 Change 3 — see docs/PHASE-1-DECOMPOSITION.md.
	findingsRepo := findings.NewRepository(sqlStore.DB(), sqlStore.Driver())
	warningsRepo := warnings.NewRepository(sqlStore.DB(), sqlStore.Driver())

	// Wire captain state-machine service (Phase 1 Change 6).
	// Reachability threshold of 90s matches the CaptainService default;
	// future config-driven override goes here.
	captainSvc := sessionmodel.NewCaptainService(sessionModelRepo, runModelRepo, 90)

	// Register the DB-backed cluster panic store so that safety.Trigger /
	// safety.Unlock propagate across all joecored instances sharing this DB.
	clusterPanicStore := sqlStore.PanicStore()
	safety.SetClusterStore(clusterPanicStore)

	// Boot in safe mode if either the local panic.state file or the shared
	// cluster_panic_state row indicates a previous emergency shutdown.
	panicState, err := safety.ReadPanicState(joeDir)
	if err != nil {
		slog.Warn("failed to read panic state file on startup", "error", err)
	}
	dbPanicked, dbPanicErr := clusterPanicStore.IsPanicked(ctx)
	if dbPanicErr != nil {
		slog.Warn("failed to read cluster panic state on startup", "error", dbPanicErr)
	}
	if panicState != nil || dbPanicked {
		safety.ActivateSafeMode()
		slog.Warn("SAFE MODE ACTIVE — previous run triggered emergency shutdown")
		if panicState != nil {
			slog.Warn("panic state (file)",
				"triggered_at", panicState.TriggeredAt,
				"trigger_source", string(panicState.TriggerSource),
				"trigger_reason", panicState.TriggerReason,
			)
		}
		slog.Warn("use 'joe unlock --reason \"...\"' to exit safe mode and resume normal operation")
	}

	// Load or create encryption key for source configs.
	encKeyPath, err := paths.EncryptionKeyPath()
	if err != nil {
		slog.Error("failed to get encryption key path", "error", err)
		return 1
	}
	encKey, err := crypto.LoadOrCreateKey(encKeyPath)
	if err != nil {
		slog.Error("failed to load encryption key", "error", err)
		return 1
	}
	encryptedSources, err := store.NewEncryptedSourceRepository(sqlStore.Sources, encKey)
	if err != nil {
		slog.Error("failed to initialize encrypted source repository", "error", err)
		return 1
	}
	sqlStore.Sources = encryptedSources
	slog.Info("source credential encryption enabled", "key_path", encKeyPath)

	// Initialize adapter registry and load saved sources
	adapterRegistry := deps.newAdapterRegistry()
	deps.connectSources(ctx, sqlStore, adapterRegistry)

	// Initialize core services (graph store uses same SQLite DB)
	services := deps.newServices(cfg, sqlStore, sqlStore.DB(), sqlStore.Driver(), adapterRegistry, metrics)
	services.RBAC = rbacRepo
	services.Audit = auditRepo
	services.LLMUsage = llmUsageRepo
	services.LLMSettings = llmSettingsSvc
	services.SessionLimitsProvider = sessionLimitsProvider
	services.CostLimitsProvider = costLimitsProvider
	services.SessionModel = sessionModelRepo
	services.RunModel = runModelRepo
	services.Findings = findingsRepo
	services.Warnings = warningsRepo
	services.CaptainSvc = captainSvc
	defer services.Close()
	slog.Info("core services ready", "graph_store", "sqlite", "adapters", len(adapterRegistry.List()))

	// Load skills from ~/.joe/skills/ (Agent Skills format). Missing directory
	// is fine — it just means no skills are installed. Parse failures are
	// logged but never fatal: a bad skill must not block startup.
	skillsDir := filepath.Join(joeDir, "skills")
	skillRegistry, err := skills.LoadDir(skillsDir)
	if err != nil {
		slog.Warn("failed to load skills", "dir", skillsDir, "error", err)
		skillRegistry = skills.NewRegistry()
	}
	services.Skills = skills.NewAtomicRouter(skills.NewRouter(skillRegistry))
	slog.Info("skills loaded", "dir", skillsDir, "count", skillRegistry.Len())

	// Load skills policy (~/.joe/skills-policy.yaml) and wire the install
	// manager so the API can serve list/approve. A missing file falls back
	// to DefaultPolicy (deny-by-default). A malformed file is fatal here —
	// silently dropping policy would let a corrupted file invert the
	// trust model.
	skillsPolicy, err := skills.LoadPolicy(joeDir)
	if err != nil {
		slog.Error("failed to load skills policy", "error", err)
		return 1
	}
	services.SkillsManager = skills.NewManager(skillsDir, nil).
		WithTrustedSources(cfg.Skills.TrustedSources).
		WithPolicy(skillsPolicy)
	slog.Info("skills policy loaded", "trusted_sources", len(skillsPolicy.TrustedSources), "auto_approve_trusted", skillsPolicy.AutoApprove.TrustedSources)

	// Start the filesystem watcher unless the operator explicitly disabled
	// hot reload. A failed watcher init is logged but never fatal — the
	// registry we just loaded stays usable, just frozen until restart.
	if !cfg.Skills.HotReloadDisabled {
		watcher, werr := skills.NewWatcher(skillsDir, services.Skills)
		if werr != nil {
			slog.Warn("skills hot reload disabled (watcher init failed)", "error", werr)
		} else {
			services.SkillsWatcher = watcher
			watcherCtx, cancelWatcher := context.WithCancel(ctx)
			defer cancelWatcher()
			go func() {
				if err := watcher.Run(watcherCtx); err != nil {
					slog.Warn("skills watcher exited with error", "error", err)
				}
			}()
			slog.Info("skills hot reload enabled", "dir", skillsDir, "debounce_ms", skills.DefaultDebounce.Milliseconds())
		}
	} else {
		slog.Info("skills hot reload disabled by config", "dir", skillsDir)
	}

	// Register business metrics gauges
	if err := deps.registerBusinessMetric(services); err != nil {
		slog.Warn("failed to register business metrics", "error", err)
	}

	// Get listen address from config (defaults to localhost:7777)
	addr := cfg.Server.Address

	// Stream G phase G4: active-model startup precedence is encoded
	// in llmsettings.ResolveActiveModelOnStartup so the policy lives
	// in one tested place. The persisted active model in llm_settings
	// wins ONLY if it still resolves against the currently configured
	// available models; otherwise configuration wins loudly. We never
	// FAIL startup on a stale stored model.
	availableKeys := make(map[string]bool, len(cfg.LLM.Available))
	for k := range cfg.LLM.Available {
		availableKeys[k] = true
	}
	activeModelKey := llmsettings.ResolveActiveModelOnStartup(ctx, llmSettingsRepo, cfg.LLM.Current, availableKeys, slog.Default())

	// Initialize LLM adapter for Core Agent against the resolved
	// active model.
	currentModelCfg, ok := cfg.LLM.Available[activeModelKey]
	if !ok {
		slog.Error("resolved active model not found in available models", "model", activeModelKey)
		return 1
	}

	llmAdapter, err := deps.newLLMAdapter(ctx, currentModelCfg)
	if err != nil {
		slog.Error("failed to initialize LLM adapter for core agent", "error", err)
		return 1
	}

	// Stream G phase G2: wrap the raw adapter in the usage recorder
	// EXACTLY ONCE, at the SINGLE construction site, BEFORE the
	// SwappableAdapter and the four downstream by-name consumers
	// (embedder, doc drafter, review agent, core agent) read it. The
	// wrapped value is assigned back to the same handle so every
	// downstream consumer below receives the recording wrapper through
	// the identical llm.LLMAdapter interface — no consumer has a path to
	// the raw, unrecorded adapter by name. The structural guard
	// TestPhaseG2_LLMAdapterConstructorWrappedOnce asserts this raw
	// constructor is called exactly once in this main file. Provider,
	// model, currency, and the USD-to-configured FX rate are sourced
	// from the active ModelConfig at this site (the concrete provider
	// clients do not expose provider/model identity, so the wiring site
	// is the single source of truth). Recording is fail-open by design;
	// see internal/llmusage/recorder.go's package doc.
	// Stream G phase G3b → G4: the recorder is also the gate. The
	// CostLimits provider passed in is now the storage-backed
	// implementation that reads per-window thresholds from the
	// llm_cost_limits table; the gate site in
	// RecorderAdapter.Chat / .gate is untouched, satisfying the
	// "swap behind the interface" invariant. An unset stored
	// threshold falls back to the conservative hardcoded backstop
	// (llmusage.StaticCostLimits) inside the provider, so a fresh
	// system stays protected. services.Audit is threaded through so a
	// gate refusal — or a gate-read failure — writes its
	// KindLLMLimitTriggered row to the same append-only sink the
	// accessor, captaingate, and runaway gate use.
	llmAdapter = llmusage.NewRecorderAdapter(llmusage.Config{
		Inner:    llmAdapter,
		Repo:     llmUsageRepo,
		Provider: currentModelCfg.Provider,
		Model:    currentModelCfg.Model,
		Currency: cfg.LLM.Currency,
		FXRate:   cfg.LLM.USDToConfiguredRate,
		Limits:   costLimitsProvider,
		Audit:    auditRepo,
	})

	// Phase 2: services.LLM is the single LLM contact point for the agentic
	// loop and the Web UI chat handler. Wrap it in a SwappableAdapter so the
	// /model HTTP API can hot-swap the active model at runtime without a
	// restart. The raw adapter is retained below for the knowledge embedder
	// and background services — embeddings must stay on a stable model and
	// must not follow interactive chat-model swaps.
	services.LLM = llm.NewSwappableAdapter(llmAdapter, activeModelKey)

	// Wire the LLM embedder into the Knowledge Service now that the adapter is ready.
	embModelName := cfg.Knowledge.EmbeddingModel
	if embModelName == "" {
		embModelName = cfg.LLM.Current
	}
	embedder := embeddings.New(llmAdapter, embModelName)
	services.Knowledge = knowledge.NewService(sqlStore.Knowledge, embedder)
	services.DocDrafter = drafts.New(services.Knowledge, services.Proposals, llmAdapter)
	slog.Info("knowledge store ready", "embedding_model", embModelName)

	// Wire up the Review Agent (Phase 10).
	// The agent uses the adapter registry for GitHub/GitLab ops and the
	// knowledge/graph stores for context enrichment.
	if services.Review != nil {
		reviewAgent := review.NewReviewAgent(
			adapterRegistryOps{adapterRegistry},
			adapterRegistryOps{adapterRegistry},
			services.Knowledge,
			services.Graph,
			llmAdapter,
			services.Review,
		)
		services.ReviewAgent = reviewAgent
		slog.Info("review agent ready")
	}

	// Start knowledge sync coordinator when sync is enabled.
	if cfg.Knowledge.SyncEnabled {
		syncers := map[string]knowledgesync.Syncer{
			"confluence": confluence.New(),
			"notion":     notion.New(),
		}
		syncCoordinator := knowledgesync.NewCoordinator(services.Knowledge, syncers)
		syncCoordinator.Start(ctx)
		defer syncCoordinator.Stop()
		slog.Info("knowledge sync coordinator started")
	}

	// Initialize and start Core Agent BEFORE setting up API routes
	coreAgent := deps.newCoreAgent(services, llmAdapter, metrics)
	services.Agent = coreAgent // Wire Core Agent to services for API handlers

	// Identity Phase G + Phase 1 Change 9: wrap the Core Agent's tool
	// executor with the COMPOSED chain
	//   captaingate.Wrapper → DurableExecutor → inner *tools.Executor
	// so that on every T2/T3 tool call the §C captain-session gate
	// runs UPSTREAM of the §D5 idempotency layer (a refused mutation
	// is never persisted as an issued intent — nothing happened to
	// record). The exact same wrapper is installed around the user
	// task loop in api.New / api/tasks.go, so the gate logic lives in
	// EXACTLY ONE place across both agentic paths (the static guard
	// TestPhaseG_SingleSharedCaptainGateImplementation asserts there
	// is no second copy in coreagent or anywhere else). Type-assert is
	// safe — newCoreAgent in defaultRunDeps returns *coreagent.Agent.
	if concrete, ok := coreAgent.(*coreagent.Agent); ok && services.RunModel != nil && services.SessionModel != nil {
		durable := coreagent.NewDurableExecutor(concrete.ToolExecutor(), services.RunModel)
		gated := captaingate.New(durable, services.SessionModel, services.Audit)
		concrete.SetToolExecutor(gated)
		slog.Info("core agent: §C captain-session gate + §D5 durable executor wrapper installed (gate runs upstream)")
	}

	if err := coreAgent.Start(ctx); err != nil {
		slog.Error("failed to start core agent", "error", err)
		return 1
	}
	defer func() {
		if err := coreAgent.Stop(context.Background()); err != nil {
			slog.Error("failed to stop core agent", "error", err)
		}
	}()

	slog.Info("core agent started with background refresh")

	// Setup HTTP server
	mux := http.NewServeMux()

	// Register API routes
	apiServer := deps.newAPIServer(services)
	if version != "" {
		apiServer.SetVersion(version)
	}
	apiServer.RegisterRoutes(mux)

	// Build the service-account resolver — the single machine-authentication
	// input (Identity Phase D). It maps each configured key to its svc:<name>
	// principal. An invalid configuration (duplicate/empty keys or names) is a
	// fatal startup error, not a silently-dropped identity.
	saResolver, saErr := auth.NewServiceAccountResolver(cfg.Server.ServiceAccounts)
	if saErr != nil {
		slog.Error("invalid service-account configuration", "error", saErr)
		return 1
	}

	// Build RBAC policy engine. It is enabled when EITHER a service account OR
	// OIDC is configured — both establish a real caller principal the engine
	// must evaluate. When neither is set (local/dev), enforcement is skipped so
	// single-user setups are not blocked by empty policy tables.
	oidcConfigured := cfg.Auth.OIDC.Configured()
	var policyEngine *rbac.PolicyEngine
	if saResolver.Configured() || oidcConfigured {
		policyEngine = rbac.NewPolicyEngine(rbacRepo)
	}
	// Stream G phase G5: surface the RBAC-enabled signal to handlers.
	// Set once here at the engine-build site so the accessor's
	// rbac-disabled short-circuit predicate, the policy engine
	// nil-ness, and services.RBACEnabled are the SAME statement
	// about the same configuration. Handlers (current-user, the admin
	// gate) consult this field rather than re-deriving the predicate.
	services.RBACEnabled = policyEngine != nil
	// Stream H2: surface OIDC-configured to the current-user handler so the
	// Web UI knows whether to offer the OIDC login button. Same build site,
	// same single-source predicate (oidcConfigured) the auth endpoints are
	// registered from below — no second auth-config endpoint.
	services.OIDCEnabled = oidcConfigured

	// Identity Phase C: server-side sessions + the OIDC login flow.
	// authRepo persists sessions and in-flight login flows (migration 014).
	// The session manager mints/resolves/revokes sessions and owns the cookie.
	authRepo := auth.NewRepository(sqlStore.DB(), sqlStore.Driver())
	sessionMgr := auth.NewSessionManager(authRepo, cfg.Auth.SessionTTL)

	// Register the OIDC login/callback/logout endpoints when an issuer is
	// configured. Discovery is lazy, so a missing/unreachable IdP at startup is
	// not fatal — only new logins fail (design §4).
	if oidcConfigured {
		authHandlers := auth.NewHandlers(auth.HandlerConfig{
			Provider:          auth.NewOIDCProvider(cfg.Auth.OIDC),
			Sessions:          sessionMgr,
			Repo:              authRepo,
			RBAC:              rbacRepo,
			AdminEmail:        cfg.Auth.AdminEmail,
			PostLoginRedirect: cfg.Auth.PostLoginRedirect,
			Audit:             services.Audit,
		})
		authHandlers.RegisterRoutes(mux, "/api/v1")
		slog.Info("OIDC login enabled", "issuer", cfg.Auth.OIDC.Issuer, "admin_email", cfg.Auth.AdminEmail != "")
	}

	// Build middleware chain: CORS → rate limit → metrics → edge auth → session headers → RBAC → request size limit → mux
	// CORS must be outermost so OPTIONS preflight requests are answered before auth runs.
	//
	// Identity Phase C/D: the edge-auth middleware replaces the prior
	// BearerAuth + IdentityMiddleware pair. It resolves the caller principal
	// from a session cookie (humans) or a service-account bearer key (machines),
	// sets it in context via rbac.WithPrincipal (the Phase B mechanism), and
	// rejects unauthenticated requests on protected paths with 401 — exactly as
	// before. The source-keyed EnforcementMiddleware below it stays
	// authoritative on the HTTP path (its demotion is Phase E).
	handler := api.Chain(
		mux,
		api.CORS(),
		api.RateLimit(cfg.Server.RateLimitRPS, cfg.Server.RateLimitBurst),
		func(h http.Handler) http.Handler {
			return observability.HTTPMetricsMiddleware(h, metrics)
		},
		auth.EdgeAuth(auth.EdgeConfig{
			Sessions:         sessionMgr,
			ServiceAccounts:  saResolver,
			OIDCConfigured:   oidcConfigured,
			Audit:            services.Audit,
			AuditDedupWindow: cfg.Auth.SessionTTL,
		}),
		// Phase 1 Change 9: thread session/run/idempotency-key
		// request headers into context AFTER identity is resolved
		// and BEFORE source-keyed RBAC enforcement runs.
		api.SessionMiddleware,
		rbac.EnforcementMiddleware(policyEngine),
		api.MaxRequestBody(api.DefaultMaxRequestBytes),
	)

	switch {
	case oidcConfigured && saResolver.Configured():
		slog.Info("API authentication enabled (OIDC login + service-account keys)")
	case oidcConfigured:
		slog.Info("API authentication enabled (OIDC login)")
	case saResolver.Configured():
		slog.Info("API authentication enabled (service-account keys)")
	default:
		slog.Warn("API authentication disabled — set auth.oidc.issuer for human login or server.service_accounts for machine access")
	}
	if cfg.Server.RateLimitRPS > 0 {
		slog.Info("API rate limiting enabled", "rps", cfg.Server.RateLimitRPS, "burst", cfg.Server.RateLimitBurst)
	}

	// Mount the embedded web UI outside the middleware chain. Requests under
	// /api/v1 are delegated to the wrapped chain above unchanged (including its
	// JSON 404s for unknown API paths); every other path is served same-origin
	// from the embedded SPA with no edge auth, so the logged-out login UI loads
	// without any credential. This is the H2 OIDC same-origin prerequisite.
	rootHandler, err := webui.Mount(handler)
	if err != nil {
		slog.Error("failed to initialize embedded web UI handler", "error", err)
		return 1
	}

	server := &http.Server{
		Addr:         addr,
		Handler:      rootHandler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	// Start metrics server if enabled
	if otelCfg.MetricsEnabled && otelCfg.MetricsExporter == "prometheus" {
		metricsHandler := observability.MetricsHandler()
		if metricsHandler != nil {
			metricsAddr := fmt.Sprintf(":%d", otelCfg.MetricsPort)
			metricsMux := http.NewServeMux()
			metricsMux.Handle("/metrics", metricsHandler)
			metricsServer := &http.Server{
				Addr:         metricsAddr,
				Handler:      metricsMux,
				ReadTimeout:  10 * time.Second,
				WriteTimeout: 10 * time.Second,
			}
			if err := deps.startMetricsServer(metricsServer); err != nil {
				slog.Warn("metrics server failed to start", "error", err)
			} else {
				defer func() {
					_ = deps.shutdownMetricsServer(context.Background(), metricsServer)
				}()
			}
		}
	}

	if cfg.Server.TLSConfigured() {
		slog.Info("TLS enabled", "cert", cfg.Server.TLSCertFile, "key", cfg.Server.TLSKeyFile)
	} else {
		slog.Warn("TLS disabled — connections to joe-core are unencrypted")
	}

	errCh := deps.startServer(server, cfg.Server.TLSCertFile, cfg.Server.TLSKeyFile)
	done := deps.waitForShutdown(ctx)

	select {
	case err := <-errCh:
		if err != nil {
			slog.Error("server error", "error", err)
			return 1
		}
	case <-done:
	}

	slog.Info("shutting down...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := deps.shutdownServer(shutdownCtx, server); err != nil {
		slog.Error("shutdown error", "error", err)
	}
	slog.Info("joe-core stopped")

	return 0
}

func defaultWaitForShutdown(ctx context.Context) <-chan struct{} {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM, syscall.SIGUSR1)

	done := make(chan struct{})
	go func() {
		select {
		case sig := <-quit:
			if sig == syscall.SIGUSR1 {
				joeDir, err := paths.JoeDirPath()
				if err != nil {
					slog.Error("SIGUSR1 panic: failed to resolve joe dir", "error", err)
				} else {
					safety.Trigger(safety.PanicSourceSignal, "SIGUSR1 received")
					state := safety.PanicState{
						TriggeredAt:   timeNow(),
						TriggerSource: safety.PanicSourceSignal,
						TriggerReason: "SIGUSR1 received",
					}
					if err := safety.WritePanicState(joeDir, state); err != nil {
						slog.Error("SIGUSR1 panic: failed to write panic.state", "error", err)
					}
				}
				slog.Error("SIGUSR1: emergency shutdown triggered — exiting with code 2")
				os.Exit(2)
			}
			close(done)
		case <-ctx.Done():
			close(done)
		}
	}()
	return done
}

// timeNow is a package-level variable so tests can stub it.
var timeNow = func() time.Time { return time.Now().UTC() }

func connectSourcesDefault(ctx context.Context, sqlStore *store.Store, registry *adapters.Registry) {
	k8sSources, err := sqlStore.Sources.ListByType(ctx, store.SourceTypeKubernetes)
	if err != nil {
		slog.Warn("failed to load kubernetes sources", "error", err)
	}
	for _, src := range k8sSources {
		adapter := k8s.New()
		if err := adapter.Connect(ctx, *src); err != nil {
			slog.Warn("failed to connect k8s source", "id", src.ID, "error", err)
			continue
		}
		registry.Register(src.ID, adapter)
		slog.Info("connected k8s source", "id", src.ID, "name", src.Name)
	}

	gitSources, err := sqlStore.Sources.ListByType(ctx, store.SourceTypeGit)
	if err != nil {
		slog.Warn("failed to load git sources", "error", err)
	}
	for _, src := range gitSources {
		adapter := gitadapter.New()
		if err := adapter.Connect(ctx, *src); err != nil {
			slog.Warn("failed to connect git source", "id", src.ID, "error", err)
			continue
		}
		registry.Register(src.ID, adapter)
		slog.Info("connected git source", "id", src.ID, "name", src.Name)
	}

	awsSources, err := sqlStore.Sources.ListByType(ctx, store.SourceTypeAWS)
	if err != nil {
		slog.Warn("failed to load aws sources", "error", err)
	}
	for _, src := range awsSources {
		adapter := awsadapter.New()
		if err := adapter.Connect(ctx, *src); err != nil {
			slog.Warn("failed to connect aws source", "id", src.ID, "error", err)
			continue
		}
		registry.Register(src.ID, adapter)
		slog.Info("connected aws source", "id", src.ID, "name", src.Name)
	}

	azureSources, err := sqlStore.Sources.ListByType(ctx, store.SourceTypeAzure)
	if err != nil {
		slog.Warn("failed to load azure sources", "error", err)
	}
	for _, src := range azureSources {
		adapter := azureadapter.New()
		if err := adapter.Connect(ctx, *src); err != nil {
			slog.Warn("failed to connect azure source", "id", src.ID, "error", err)
			continue
		}
		registry.Register(src.ID, adapter)
		slog.Info("connected azure source", "id", src.ID, "name", src.Name)
	}

	falcoSources, err := sqlStore.Sources.ListByType(ctx, store.SourceTypeFalco)
	if err != nil {
		slog.Warn("failed to load falco sources", "error", err)
	}
	for _, src := range falcoSources {
		adapter := falcoadapter.New()
		if err := adapter.Connect(ctx, *src); err != nil {
			slog.Warn("failed to connect falco source", "id", src.ID, "error", err)
			continue
		}
		registry.Register(src.ID, adapter)
		slog.Info("connected falco source", "id", src.ID, "name", src.Name)
	}

	// Phase 6, Step 12 — proprietary observability vendors.
	datadogSources, err := sqlStore.Sources.ListByType(ctx, store.SourceTypeDatadog)
	if err != nil {
		slog.Warn("failed to load datadog sources", "error", err)
	}
	for _, src := range datadogSources {
		adapter := datadogadapter.New()
		if err := adapter.Connect(ctx, *src); err != nil {
			slog.Warn("failed to connect datadog source", "id", src.ID, "error", err)
			continue
		}
		registry.Register(src.ID, adapter)
		slog.Info("connected datadog source", "id", src.ID, "name", src.Name)
	}

	splunkSources, err := sqlStore.Sources.ListByType(ctx, store.SourceTypeSplunk)
	if err != nil {
		slog.Warn("failed to load splunk sources", "error", err)
	}
	for _, src := range splunkSources {
		adapter := splunkadapter.New()
		if err := adapter.Connect(ctx, *src); err != nil {
			slog.Warn("failed to connect splunk source", "id", src.ID, "error", err)
			continue
		}
		registry.Register(src.ID, adapter)
		slog.Info("connected splunk source", "id", src.ID, "name", src.Name)
	}

	dynatraceSources, err := sqlStore.Sources.ListByType(ctx, store.SourceTypeDynatrace)
	if err != nil {
		slog.Warn("failed to load dynatrace sources", "error", err)
	}
	for _, src := range dynatraceSources {
		adapter := dynatraceadapter.New()
		if err := adapter.Connect(ctx, *src); err != nil {
			slog.Warn("failed to connect dynatrace source", "id", src.ID, "error", err)
			continue
		}
		registry.Register(src.ID, adapter)
		slog.Info("connected dynatrace source", "id", src.ID, "name", src.Name)
	}

	newrelicSources, err := sqlStore.Sources.ListByType(ctx, store.SourceTypeNewRelic)
	if err != nil {
		slog.Warn("failed to load newrelic sources", "error", err)
	}
	for _, src := range newrelicSources {
		adapter := newrelicadapter.New()
		if err := adapter.Connect(ctx, *src); err != nil {
			slog.Warn("failed to connect newrelic source", "id", src.ID, "error", err)
			continue
		}
		registry.Register(src.ID, adapter)
		slog.Info("connected newrelic source", "id", src.ID, "name", src.Name)
	}

	// Phase 10 — GitHub and GitLab sources for code review.
	githubSources, err := sqlStore.Sources.ListByType(ctx, store.SourceTypeGitHub)
	if err != nil {
		slog.Warn("failed to load github sources", "error", err)
	}
	for _, src := range githubSources {
		adapter := githubadapter.New()
		if err := adapter.Connect(ctx, *src); err != nil {
			slog.Warn("failed to connect github source", "id", src.ID, "error", err)
			continue
		}
		registry.Register(src.ID, adapter)
		slog.Info("connected github source", "id", src.ID, "name", src.Name)
	}

	gitlabSources, err := sqlStore.Sources.ListByType(ctx, store.SourceTypeGitLab)
	if err != nil {
		slog.Warn("failed to load gitlab sources", "error", err)
	}
	for _, src := range gitlabSources {
		adapter := gitlabadapter.New()
		if err := adapter.Connect(ctx, *src); err != nil {
			slog.Warn("failed to connect gitlab source", "id", src.ID, "error", err)
			continue
		}
		registry.Register(src.ID, adapter)
		slog.Info("connected gitlab source", "id", src.ID, "name", src.Name)
	}
}

func registerBusinessMetricsDefault(services *core.Services) error {
	businessProvider := observability.BusinessMetricsProvider{
		SourcesByType: func(ctx context.Context) (map[string]int, error) {
			sources, err := services.Store.Sources.List(ctx)
			if err != nil {
				return nil, err
			}
			counts := map[string]int{}
			for _, src := range sources {
				counts[src.Type]++
			}
			return counts, nil
		},
		GraphSummary: func(ctx context.Context) (observability.GraphMetricsSummary, error) {
			summary, err := services.Graph.Summary(ctx)
			if err != nil {
				return observability.GraphMetricsSummary{}, err
			}
			return observability.GraphMetricsSummary{
				NodeCount:   summary.NodeCount,
				EdgeCount:   summary.EdgeCount,
				NodesByType: summary.NodesByType,
			}, nil
		},
		AdapterCount: func() int {
			return len(services.Adapters.List())
		},
	}
	_, err := services.Metrics.RegisterBusinessMetrics(businessProvider)
	return err
}

func defaultStartMetricsServer(server *http.Server) error {
	go func() {
		slog.Info("metrics server listening", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("metrics server error", "error", err)
		}
	}()
	return nil
}

func defaultStartServer(server *http.Server, certFile, keyFile string) <-chan error {
	errCh := make(chan error, 1)
	go func() {
		slog.Info("joe-core starting", "addr", server.Addr, "tls", certFile != "")
		fmt.Printf("joe-core listening on %s\n", server.Addr)
		var err error
		if certFile != "" && keyFile != "" {
			err = server.ListenAndServeTLS(certFile, keyFile)
		} else {
			err = server.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()
	return errCh
}

func main() {
	ctx := context.Background()
	os.Exit(run(ctx))
}
