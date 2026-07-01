package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/jaimegago/joe/internal/access"
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
	"github.com/jaimegago/joe/internal/buildinfo"
	"github.com/jaimegago/joe/internal/captaingate"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/coreagent"
	"github.com/jaimegago/joe/internal/crypto"
	"github.com/jaimegago/joe/internal/env"
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
	"github.com/jaimegago/joe/internal/promotereads"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/readposture"
	"github.com/jaimegago/joe/internal/runmodel"
	"github.com/jaimegago/joe/internal/safety"
	"github.com/jaimegago/joe/internal/search"
	"github.com/jaimegago/joe/internal/sessionarchive"
	"github.com/jaimegago/joe/internal/sessionmodel"
	"github.com/jaimegago/joe/internal/sessionsweeper"
	"github.com/jaimegago/joe/internal/skills"
	"github.com/jaimegago/joe/internal/store"
	"github.com/jaimegago/joe/internal/warnings"
	"github.com/jaimegago/joe/internal/webui"
)

type coreAgentRunner interface {
	core.CoreAgent
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

type serverDeps struct {
	// configPath is the resolved path to the config file. Empty means "use the
	// default ~/.joe/config.yaml". runServer() fills it from the --config flag /
	// JOE_CONFIG env; tests that drive runServerWithDeps directly leave it empty
	// to get the default-path behaviour.
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
	connectComponents      func(ctx context.Context, store *store.Store, registry *adapters.Registry)
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

func defaultServerDeps() serverDeps {
	return serverDeps{
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
		connectComponents:  connectSourcesDefault,
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

// noIdentityConfigMessage is the operator-actionable error shown when Joe is
// asked to boot without any usable identity configuration. It mirrors the
// missing-LLM-creds noProviderKeyMessage style: a rich, multi-line remediation
// string held in a constant rather than inlined at the log site. Joe refuses to
// start in this state because the RBAC policy engine would be nil — an ungoverned
// engine that permits every operation — so there is no safe way to run.
const noIdentityConfigMessage = `Joe has no usable identity configuration — it would run ungoverned.

Without an identity source the RBAC policy engine is not constructed, which
permits every operation with no caller principal to authorize against. Joe
refuses to start in this state.

Configure at least one of the following, then restart:
  - OIDC (human login): set ALL THREE of auth.oidc.issuer, auth.oidc.client_id
    and auth.oidc.redirect_url. A partial OIDC block (e.g. issuer only) does NOT
    count as configured and Joe will still refuse to start.
  - Service account (machine access): add at least one entry to
    server.service_accounts (name + key).`

// requireIdentityConfigured is the boot-time refuse-to-start guard (JOE-IDBOOT).
// It returns nil when Joe has a usable identity configuration — i.e. when the
// shared engine-enable predicate config.Config.RBACEnabled is true — and the
// rich noIdentityConfigMessage error otherwise. Sharing RBACEnabled with the two
// engine-construction sites is what makes engine-nil-at-runtime unreachable:
// the same predicate that would build the engine also decides whether Joe may
// boot at all. The predicate is pure-config (no IdP probe), so a complete-but-
// unreachable OIDC issuer passes — an IdP outage must not become a Joe outage.
func requireIdentityConfigured(cfg *config.Config) error {
	if cfg.RBACEnabled() {
		return nil
	}
	return errors.New(noIdentityConfigMessage)
}

// runServer is Joe's default (no-subcommand) behavior: it boots the HTTP API
// daemon on :7777. The CLI dispatcher in main.go routes here for a bare `joe`
// invocation or one carrying only server flags (e.g. `joe --config ...`).
func runServer(ctx context.Context) int {
	deps := defaultServerDeps()
	deps.configPath = resolveConfigPath(os.Args[1:])
	return runServerWithDeps(ctx, deps)
}

// resolveConfigPath determines which config file the server loads, in descending
// precedence: the --config flag, then the JOE_CONFIG environment variable, then
// "" (the caller falls back to the default ~/.joe/config.yaml). It parses a
// private FlagSet over the given args rather than the global flag.CommandLine so
// it never collides with go test's flags.
func resolveConfigPath(args []string) string {
	fs := flag.NewFlagSet("joe", flag.ContinueOnError)
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

func runServerWithDeps(ctx context.Context, deps serverDeps) int {
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

	// Compute the ui_digest once from the embedded UI bytes this binary serves,
	// so buildinfo.Get() (read by /api/v1/version, /api/v1/status, and the
	// joe_build_info gauge) reports a digest that cannot disagree with what is
	// embedded. The injected fields (version/commit/build_time) are already set
	// by the linker; only the digest is derived here.
	if uiFS, fsErr := webui.DistFS(); fsErr != nil {
		slog.Warn("could not open embedded UI for build digest", "error", fsErr)
	} else if err := buildinfo.Init(uiFS); err != nil {
		slog.Warn("could not compute embedded UI build digest", "error", err)
	}

	// Wire the append-only audit log (Identity Phase F, migration 015).
	// Every authorization decision the accessor makes and every
	// regime/captain transition writes one row here.
	auditRepo := audit.NewRepository(sqlStore.DB(), sqlStore.Driver())

	// Wire RBAC repository (uses the same SQLite DB, tables created by migration
	// 006). It is given the audit sink so every RBAC/admin mutation writes its
	// KindAdminAccess audit row in the same transaction as the mutation itself
	// (Identity Stage 1): the mutation and its audit row commit or roll back as
	// one, for every caller — not just the HTTP handler.
	rbacRepo := rbac.NewRepositoryWithAudit(sqlStore.DB(), sqlStore.Driver(), auditRepo)

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
	contextBudgetProvider := llmsettings.NewContextBudgetProvider(llmSettingsRepo, agentloop.NewStaticContextBudget(), slog.Default())
	llmSettingsSvc := llmsettings.NewMutationService(llmSettingsRepo, auditRepo)

	// A001-COREGOV CC-04: the per-component-type auto_promote_reads flag (table
	// created by migration 024). promoteReadsRepo is the live read seam the
	// policy engine consults for the agent:core dynamic admit predicate;
	// promoteReadsSvc is the SOLE write path, committing each flag change with
	// its admin_access audit row atomically (mirrors llmSettingsSvc).
	promoteReadsRepo := promotereads.NewRepository(sqlStore.DB(), sqlStore.Driver())
	promoteReadsSvc := promotereads.NewMutationService(promoteReadsRepo, auditRepo)

	// read-posture-latch: the install-wide read posture (table read_posture,
	// migration 028). readPostureRepo is the live read seam the policy engine
	// consults for the team_flat read admit (resolved per decision, no cache);
	// readPostureSvc is the SOLE write path, committing each posture flip with its
	// admin_access audit row atomically (mirrors promoteReadsSvc). The engine is
	// built WITH this resolver below (NewPolicyEngineWithGovernance), so a
	// fresh/upgraded install defaults to team_flat until an operator flips it to
	// zoned via the admin REST surface.
	readPostureRepo := readposture.NewRepository(sqlStore.DB(), sqlStore.Driver())
	readPostureSvc := readposture.NewMutationService(readPostureRepo, auditRepo)

	// Wire session-model repository (tables created by migration 009).
	// Phase 1 Change 1 — see the Phase 1 decomposition plan.
	sessionModelRepo := sessionmodel.NewRepository(sqlStore.DB(), sqlStore.Driver())

	// Wire run-model repository (tables created by migration 010).
	// Phase 1 Change 2 — see the Phase 1 decomposition plan.
	runModelRepo := runmodel.NewRepository(sqlStore.DB(), sqlStore.Driver())

	// Wire findings + warnings repositories (tables created by migration 011).
	// Phase 1 Change 3 — see the Phase 1 decomposition plan.
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

	// Resolve the write floor exactly once at boot (D-0018). Its two inputs are
	// the sticky panic state (the shared cluster_panic_state DB row from a
	// previous emergency shutdown — the SINGLE home for panic state since the
	// panic.state file was removed) and the JOE_MODE=observation env var.
	// Precedence: panic wins over observation, so a panicked Joe boots into safe
	// mode regardless of the env var. The DB row is read here, AFTER the store is
	// initialized and migrated above and BEFORE any tool executor is wired below,
	// so the sealed floor governs every write. The resolved value is sealed into
	// Services below and never re-derived from disk during the process lifetime —
	// recovery is restart, never a live transition.
	dbPanicked, dbPanicErr := clusterPanicStore.IsPanicked(ctx)
	if dbPanicErr != nil {
		slog.Warn("failed to read panic state on startup", "error", dbPanicErr)
	}
	observationMode := os.Getenv(env.Mode) == env.ModeObservation
	writeFloor := safety.ResolveWriteFloor(dbPanicked, observationMode)
	switch writeFloor.Reason() {
	case safety.FloorReasonSafeMode:
		slog.Warn("WRITE FLOOR UP (safe_mode) — previous run triggered emergency shutdown; Joe is read-only")
		if info, infoErr := clusterPanicStore.PanicInfo(ctx); infoErr == nil && info != nil {
			slog.Warn("panic state",
				"triggered_at", info.TriggeredAt,
				"trigger_source", string(info.TriggerSource),
				"trigger_reason", info.TriggerReason,
			)
		}
		slog.Warn("clear the panic state with 'joe unlock', then restart to resume writes")
	case safety.FloorReasonObservation:
		slog.Info("WRITE FLOOR UP (observation) — Joe is in read-only observation mode by configuration (JOE_MODE=observation)")
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
	encryptedSources, err := store.NewEncryptedComponentRepository(sqlStore.Components, encKey)
	if err != nil {
		slog.Error("failed to initialize encrypted source repository", "error", err)
		return 1
	}
	sqlStore.Components = encryptedSources
	slog.Info("source credential encryption enabled", "key_path", encKeyPath)

	// Initialize adapter registry and load saved components
	adapterRegistry := deps.newAdapterRegistry()
	deps.connectComponents(ctx, sqlStore, adapterRegistry)

	// Initialize core services (graph store uses same SQLite DB)
	services := deps.newServices(cfg, sqlStore, sqlStore.DB(), sqlStore.Driver(), adapterRegistry, metrics)
	// Seal the boot-resolved write floor into Services — the single process-wide
	// source the tool executors and the panic status handler read (D-0018).
	services.WriteFloor = writeFloor
	services.RBAC = rbacRepo
	services.Audit = auditRepo
	// Identity Stage 3: the admin REST surface (internal/api/admin.go) manages
	// the identity registry and admin roster. Wire the read path (the registry
	// repository, satisfied by rbacRepo) and the two orchestration seams it
	// wraps — the admin-grant provisioner and the disable/enable lifecycle —
	// here, BEFORE RegisterRoutes (registerAdminRoutes reads them at
	// registration time). authSessions is the session store the disable path
	// purges; sessionMgr below reuses it. They are wired whenever RBAC exists so
	// the admin surface (registered on the same RBAC!=nil predicate) is fully
	// backed.
	authSessions := auth.NewRepository(sqlStore.DB(), sqlStore.Driver())
	services.Principals = rbacRepo
	services.Provisioner = auth.NewProvisioner(rbacRepo)
	services.PrincipalAdmin = auth.NewPrincipalAdmin(rbacRepo, authSessions)
	services.LLMUsage = llmUsageRepo
	services.LLMSettings = llmSettingsSvc
	services.PromoteReads = promoteReadsSvc
	services.ReadPosture = readPostureSvc
	services.SessionLimitsProvider = sessionLimitsProvider
	services.CostLimitsProvider = costLimitsProvider
	services.ContextBudgetProvider = contextBudgetProvider
	services.SessionModel = sessionModelRepo
	// §12.6 archive provider (B007c): the filesystem archive backend behind the
	// provider seam, coupled with the session store into the Archiver the admin
	// archive/restore-archive routes and the sweeper's archive terminal action
	// share. The directory defaults to ~/.joe/session-archive (operator override:
	// server.session_archive_dir). A mkdir failure is fatal — an archive terminal
	// action with no writable artifact directory would silently leave sessions
	// active, so we surface it at boot rather than at first archive.
	archiveDir := cfg.Server.SessionArchiveDir
	if archiveDir == "" {
		archiveDir = filepath.Join(joeDir, paths.SessionArchiveDirName)
	}
	if err := deps.mkdirAll(archiveDir, 0o700); err != nil {
		slog.Error("failed to create session archive directory", "path", archiveDir, "error", err)
		return 1
	}
	sessionArchiver := sessionarchive.New(sessionarchive.NewFilesystemProvider(archiveDir), sessionModelRepo)
	services.SessionArchive = sessionArchiver
	slog.Info("session archive provider ready", "scheme", "fs", "dir", archiveDir)
	services.RunModel = runModelRepo
	services.Findings = findingsRepo
	services.Warnings = warningsRepo
	services.CaptainSvc = captainSvc
	// Web search is a global, boot-only capability (not a component): resolve
	// the configured SearchProvider once here from cfg.WebSearch and seal it
	// into Services, where the user-task tool registry reads it for the
	// web_search tool. NewProvider returns a nil provider when web search is
	// unconfigured (inert — the tool stays advertised and returns a
	// no-backend-configured tool-error), and an error only for a misconfigured
	// backend (unknown provider, or SearXNG without a base_url), which is fatal
	// at boot as an LLM misconfiguration is. Changing the backend requires a
	// restart — there is no runtime swap surface.
	searchProvider, err := search.NewProvider(cfg.WebSearch)
	if err != nil {
		slog.Error("failed to configure web search", "error", err)
		return 1
	}
	services.WebSearch = searchProvider
	if searchProvider != nil {
		slog.Info("web search provider ready", "provider", cfg.WebSearch.Provider)
	} else {
		slog.Info("web search not configured (web_search tool advertised, reports no backend)")
	}
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
	// SwappableAdapter and the downstream by-name consumers
	// (embedder, doc drafter, core agent) read it. The
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
	//
	// The wrap is built by services.BuildLLMChain — the SINGLE chain
	// construction site shared with the two model-swap HTTP handlers
	// (internal/api models.go / llmsettings.go), so a runtime model swap
	// installs an identically-wrapped chain and cost recording + gating
	// survive the swap. BuildLLMChain reads Repo / Limits / Audit /
	// Currency / FXRate from services (wired above at services.LLMUsage,
	// services.CostLimitsProvider, services.Audit, services.Config), so
	// the recorder identity (provider/model) comes from currentModelCfg
	// and every dependency is the same instance the gate enforces with.
	llmAdapter = services.BuildLLMChain(llmAdapter, currentModelCfg)

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
	// safe — newCoreAgent in defaultServerDeps returns *coreagent.Agent.
	if concrete, ok := coreAgent.(*coreagent.Agent); ok && services.RunModel != nil && services.SessionModel != nil {
		durable := coreagent.NewDurableExecutor(concrete.ToolExecutor(), services.RunModel)
		// WithFloor makes the denial precedence floor > incident hold by
		// construction on the autonomous path (D-0019 decision 9): the wrapper
		// checks the same boot-sealed floor the inner executor carries
		// (agent.go injects it via tools.WithWriteFloor) BEFORE the §C gate, so a
		// floored Mutate surfaces the floor reason rather than an incident-mode
		// refusal. A no-op today — the Core Agent issues only Reads — but correct
		// the day an autonomous managed-system Mutate exists.
		gated := captaingate.New(durable, services.SessionModel, services.Audit, captaingate.WithFloor(services.WriteFloor))
		concrete.SetToolExecutor(gated)
		slog.Info("core agent: §C captain-session gate + §D5 durable executor wrapper installed (gate runs upstream)")
	}

	// A001-COREGOV CC-05: floor the autonomous refresh read. Build a SECOND
	// accessor for the refresh path and thread it onto the refresher before
	// Start, so every refresh cycle resolves each component's adapter through the
	// access guard under the agent:core principal (carried on the Start ctx by
	// CC-02) at ActionRead — denying any ungranted/unpromoted component before
	// its adapter, and thus its credential, is resolved.
	//
	// CRITICAL: this accessor's engine is the promote-aware engine
	// (NewPolicyEngineWithPromote over the auto_promote_reads resolver), not a
	// bare NewPolicyEngine — otherwise agent:core reads deny-all even for promoted
	// types. It is deliberately built WITHOUT the read-posture resolver
	// (read-posture-latch-02, D-0043): the read posture is a HUMAN read-sharing
	// posture governing the human-facing transport reads only; the autonomous
	// agent:core read surface is a SEPARATE axis governed solely by
	// auto_promote_read plus grants. Wiring the posture resolver here would let the
	// default team_flat posture admit agent:core to read every component before
	// auto_promote_read is consulted, silently overriding the operator-controlled
	// promotion surface. The two axes are separated at construction — the refresh
	// engine never receives a posture resolver, so the team_flat admit is
	// structurally unreachable on this path, not merely disabled by discipline.
	// Flipping the install read posture between team_flat and zoned therefore
	// cannot change what agent:core can read.
	//
	// The transport policyEngine below is built the SAME way from the SAME
	// rbacRepo + promoteReadsRepo under the SAME cfg.RBACEnabled() predicate, plus
	// the read-posture resolver it (and only it) carries. A nil engine (RBAC
	// disabled) makes the accessor permit every decision, matching the transport
	// path. The accessor is principal-agnostic; it becomes "the agent:core
	// accessor" purely by being consumed under the CC-02 ctx.
	if concrete, ok := coreAgent.(*coreagent.Agent); ok {
		var refreshEngine *rbac.PolicyEngine
		if cfg.RBACEnabled() {
			refreshEngine = rbac.NewPolicyEngineWithPromote(rbacRepo, promoteReadsRepo)
		}
		refreshAccessor := access.New(services.Adapters, services.Graph, refreshEngine, auditRepo)
		concrete.SetRefreshAccessor(refreshAccessor)
		slog.Info("core agent: refresh adapter resolution floored through the access guard (agent:core, ActionRead; promote-aware, no read posture)")
	}

	// Mint the Core Agent's internal boot identity (agent:core) once and stamp
	// it onto the context handed to Start (A001-COREGOV CC-02). The derived
	// (WithCancel) context inside refresher.Start inherits it, so the principal
	// rides the already-plumbed boot context end-to-end to the refresh path
	// (refresh → refreshComponent → the guarded adapter resolution and the
	// ApplyGraphDelta write). agent:core is an internal boot principal, not an
	// authenticated API caller, so it is minted directly via the rbac constructor
	// — never through the service-account resolver above. Stamping once at this
	// seam is preferred over threading the principal through refresh function
	// signatures. The read is now floored through the refresh accessor wired just
	// above (CC-05) using the promote-aware engine (CC-04); the graph-write half
	// stays principal-stamped and governed upstream by this read floor.
	agentCore, principalErr := rbac.AgentCorePrincipal()
	if principalErr != nil {
		slog.Error("failed to mint core agent principal", "error", principalErr)
		return 1
	}
	if err := coreAgent.Start(rbac.WithPrincipal(ctx, agentCore)); err != nil {
		slog.Error("failed to start core agent", "error", err)
		return 1
	}
	defer func() {
		if err := coreAgent.Stop(context.Background()); err != nil {
			slog.Error("failed to stop core agent", "error", err)
		}
	}()

	slog.Info("core agent started with background refresh")

	// Start the §12.5 retention sweeper (B007b): the single automated expiration
	// driver. It applies the admin retention policy (inactivity expiry → trash
	// under trash_then_purge; trash-grace purge of trashed sessions past
	// purge_after) and drains abandoned auth_login_flows, each lifecycle effect
	// coupled to its audit row in one transaction under a boot-minted service
	// principal (svc:sweeper:sessions). It runs only when the session store is
	// wired; the principal rides the Start ctx (mirroring the Core Agent refresh)
	// and is also carried explicitly for audit attribution. The archive terminal
	// action is deferred to B007c behind an honest seam (archive-policy
	// inactivity-expired sessions are left active and logged, never falsely
	// archived). Inactivity expiry is OFF by default (regulated posture); a
	// default deployment only purges trashed sessions past grace and drains login
	// flows.
	if services.SessionModel != nil {
		sweeperPrincipal, sweeperErr := rbac.SessionSweeperPrincipal()
		if sweeperErr != nil {
			slog.Error("failed to mint session sweeper principal", "error", sweeperErr)
			return 1
		}
		sweeper := sessionsweeper.New(sessionsweeper.Config{
			DB:        sqlStore.DB(),
			Sessions:  sessionModelRepo,
			Flows:     authSessions,
			Archive:   sessionArchiver,
			Audit:     auditRepo,
			Principal: sweeperPrincipal,
			Logger:    slog.Default(),
		})
		if err := sweeper.Start(rbac.WithPrincipal(ctx, sweeperPrincipal)); err != nil {
			slog.Error("failed to start session retention sweeper", "error", err)
			return 1
		}
		defer func() {
			if err := sweeper.Stop(context.Background()); err != nil {
				slog.Error("failed to stop session retention sweeper", "error", err)
			}
		}()
		slog.Info("session retention sweeper started", "principal", string(sweeperPrincipal))
	}

	// Setup HTTP server
	mux := http.NewServeMux()

	// Register API routes
	apiServer := deps.newAPIServer(services)
	apiServer.RegisterRoutes(mux)

	// Build the service-account resolver — the single machine-authentication
	// input (Identity Phase D). It maps each configured key to its svc:<name>
	// principal. An invalid configuration (duplicate/empty keys or names) is a
	// fatal startup error, not a silently-dropped identity.
	//
	// This fatal gate runs BEFORE the refuse-to-start guard and engine build
	// below: it is load-bearing, because it is what makes raw-config
	// service-account presence (cfg.RBACEnabled) equivalent to the resolved
	// resolver's saResolver.Configured() at the engine-build site — a malformed
	// account map exits here rather than reaching the predicate.
	saResolver, saErr := auth.NewServiceAccountResolver(cfg.Server.ServiceAccounts)
	if saErr != nil {
		slog.Error("invalid service-account configuration", "error", saErr)
		return 1
	}

	// Refuse to start without a usable identity configuration (JOE-IDBOOT). The
	// guard's predicate IS the engine's own enable predicate (cfg.RBACEnabled),
	// the same one both engine-construction sites call, so refuse-to-start and
	// construct-the-engine cannot drift. It is positioned AFTER the SA-resolver
	// fatal gate and just BEFORE engine construction. Once it passes, the policy
	// engine is necessarily non-nil below: running ungoverned (nil engine,
	// all-operations-permitted) is unreachable, in the same fail-fast tier and
	// exit semantics as missing LLM credentials above.
	if err := requireIdentityConfigured(cfg); err != nil {
		slog.Error("no usable identity configuration", "error", err)
		return 1
	}

	// Build RBAC policy engine. It is enabled when EITHER a service account OR
	// OIDC is configured — both establish a real caller principal the engine
	// must evaluate. The shared cfg.RBACEnabled predicate is the same one the
	// refuse-to-start guard just enforced, so this is always true here and the
	// engine is non-nil; the call is kept at the construction site so the
	// engine-enable decision lives in exactly one predicate.
	oidcConfigured := cfg.Auth.OIDC.Configured()
	var policyEngine *rbac.PolicyEngine
	if cfg.RBACEnabled() {
		// Build the engine WITH both live governance resolvers (read-posture-latch):
		// the auto_promote_reads resolver (A001-COREGOV CC-04) so the agent:core +
		// ActionRead dynamic admit predicate is live, AND the read-posture resolver
		// so the team_flat read admit is live per decision. With a fresh/upgraded
		// install seeded team_flat, every authenticated HUMAN principal reads every
		// component until an operator flips the posture to zoned.
		//
		// The read-posture resolver is wired ONLY here, on the human-facing
		// transport engine — NOT on the agent:core refresh engine built above
		// (read-posture-latch-02, D-0043). The read posture governs human-facing
		// transport reads; the autonomous agent:core read surface is a separate
		// axis governed solely by auto_promote_read plus grants. Keeping the
		// posture resolver off the refresh engine makes that separation structural.
		policyEngine = rbac.NewPolicyEngineWithGovernance(rbacRepo, promoteReadsRepo, readPostureRepo)
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
	// authSessions (wired above for the disable path) persists sessions and
	// in-flight login flows (migration 014). The session manager
	// mints/resolves/revokes sessions and owns the cookie.
	authRepo := authSessions
	sessionMgr := auth.NewSessionManager(authRepo, cfg.Auth.SessionTTL)

	// Auth cookies are Secure by default. server.insecure_cookies drops Secure
	// for local HTTP dev only — Safari/Firefox refuse to store Secure cookies
	// delivered over plain http://, so OIDC login fails with a state mismatch
	// there (Chrome's localhost special-case hides it). Never enable in prod.
	if cfg.Server.InsecureCookies {
		sessionMgr.SetSecureCookies(false)
		slog.Warn("auth: insecure cookies enabled — session and OIDC state cookies are NOT marked Secure; for local HTTP dev only, never production")
	}

	// Register the OIDC login/callback/logout endpoints when an issuer is
	// configured. Discovery is lazy, so a missing/unreachable IdP at startup is
	// not fatal — only new logins fail (design §4).
	if oidcConfigured {
		authHandlers := auth.NewHandlers(auth.HandlerConfig{
			Provider:             auth.NewOIDCProvider(cfg.Auth.OIDC),
			Sessions:             sessionMgr,
			Repo:                 authRepo,
			RBAC:                 rbacRepo,
			Principals:           rbacRepo,
			AdminEmail:           cfg.Auth.AdminEmail,
			PostLoginRedirect:    cfg.Auth.PostLoginRedirect,
			Audit:                services.Audit,
			AllowInsecureCookies: cfg.Server.InsecureCookies,
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
		// Unreachable: the refuse-to-start guard above (requireIdentityConfigured)
		// guarantees at least one of OIDC or service accounts is configured, so
		// "authentication disabled" is no longer a state Joe can reach at runtime.
		// This arm is a defensive assertion of that invariant — NOT an operator
		// warning. If it ever fires the guard was bypassed (a programming error),
		// not an operator misconfiguration.
		slog.Error("unreachable: API authentication unconfigured despite the boot identity guard — this is a bug, not a misconfiguration")
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
	if !webui.Embedded() {
		slog.Warn("web UI not embedded in this binary — only the API under /api/v1 and a fallback page are served; build with `make build` to produce a UI-complete binary")
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
		slog.Warn("TLS disabled — connections to joe are unencrypted")
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
	slog.Info("joe stopped")

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
				// Trigger persists the panic to the single cluster_panic_state DB
				// row via the cluster store registered at boot (SetClusterStore);
				// boot reads that row to raise the safe-mode floor on next start.
				safety.Trigger(safety.PanicSourceSignal, "SIGUSR1 received")
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

func connectSourcesDefault(ctx context.Context, sqlStore *store.Store, registry *adapters.Registry) {
	k8sSources, err := sqlStore.Components.ListByType(ctx, store.ComponentTypeKubernetes)
	if err != nil {
		slog.Warn("failed to load kubernetes components", "error", err)
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

	gitComponents, err := sqlStore.Components.ListByType(ctx, store.ComponentTypeGit)
	if err != nil {
		slog.Warn("failed to load git components", "error", err)
	}
	for _, src := range gitComponents {
		adapter := gitadapter.New()
		if err := adapter.Connect(ctx, *src); err != nil {
			slog.Warn("failed to connect git source", "id", src.ID, "error", err)
			continue
		}
		registry.Register(src.ID, adapter)
		slog.Info("connected git source", "id", src.ID, "name", src.Name)
	}

	awsSources, err := sqlStore.Components.ListByType(ctx, store.ComponentTypeAWS)
	if err != nil {
		slog.Warn("failed to load aws components", "error", err)
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

	azureSources, err := sqlStore.Components.ListByType(ctx, store.ComponentTypeAzure)
	if err != nil {
		slog.Warn("failed to load azure components", "error", err)
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

	falcoSources, err := sqlStore.Components.ListByType(ctx, store.ComponentTypeFalco)
	if err != nil {
		slog.Warn("failed to load falco components", "error", err)
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
	datadogSources, err := sqlStore.Components.ListByType(ctx, store.ComponentTypeDatadog)
	if err != nil {
		slog.Warn("failed to load datadog components", "error", err)
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

	splunkComponents, err := sqlStore.Components.ListByType(ctx, store.ComponentTypeSplunk)
	if err != nil {
		slog.Warn("failed to load splunk components", "error", err)
	}
	for _, src := range splunkComponents {
		adapter := splunkadapter.New()
		if err := adapter.Connect(ctx, *src); err != nil {
			slog.Warn("failed to connect splunk source", "id", src.ID, "error", err)
			continue
		}
		registry.Register(src.ID, adapter)
		slog.Info("connected splunk source", "id", src.ID, "name", src.Name)
	}

	dynatraceSources, err := sqlStore.Components.ListByType(ctx, store.ComponentTypeDynatrace)
	if err != nil {
		slog.Warn("failed to load dynatrace components", "error", err)
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

	newrelicComponents, err := sqlStore.Components.ListByType(ctx, store.ComponentTypeNewRelic)
	if err != nil {
		slog.Warn("failed to load newrelic components", "error", err)
	}
	for _, src := range newrelicComponents {
		adapter := newrelicadapter.New()
		if err := adapter.Connect(ctx, *src); err != nil {
			slog.Warn("failed to connect newrelic source", "id", src.ID, "error", err)
			continue
		}
		registry.Register(src.ID, adapter)
		slog.Info("connected newrelic source", "id", src.ID, "name", src.Name)
	}

	// Phase 10 — GitHub and GitLab components for code review.
	githubComponents, err := sqlStore.Components.ListByType(ctx, store.ComponentTypeGitHub)
	if err != nil {
		slog.Warn("failed to load github components", "error", err)
	}
	for _, src := range githubComponents {
		adapter := githubadapter.New()
		if err := adapter.Connect(ctx, *src); err != nil {
			slog.Warn("failed to connect github source", "id", src.ID, "error", err)
			continue
		}
		registry.Register(src.ID, adapter)
		slog.Info("connected github source", "id", src.ID, "name", src.Name)
	}

	gitlabComponents, err := sqlStore.Components.ListByType(ctx, store.ComponentTypeGitLab)
	if err != nil {
		slog.Warn("failed to load gitlab components", "error", err)
	}
	for _, src := range gitlabComponents {
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
		ComponentsByType: func(ctx context.Context) (map[string]int, error) {
			components, err := services.Store.Components.List(ctx)
			if err != nil {
				return nil, err
			}
			counts := map[string]int{}
			for _, src := range components {
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
	if _, err := services.Metrics.RegisterBusinessMetrics(businessProvider); err != nil {
		return err
	}

	// joe_build_info: one constant-1 gauge per binary, build identity in labels
	// (read from the single source of build truth). Registered here in the
	// metrics-setup layer, under the same metrics-enabled gate as the business
	// gauges, per the CLAUDE.md instrumentation invariant.
	bi := buildinfo.Get()
	if _, err := services.Metrics.RegisterBuildInfo(observability.BuildInfo{
		Version:   bi.Version,
		Commit:    bi.Commit,
		BuildTime: bi.BuildTime,
		UIDigest:  bi.UIDigest,
	}); err != nil {
		return err
	}
	return nil
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
		slog.Info("joe starting", "addr", server.Addr, "tls", certFile != "")
		fmt.Printf("joe listening on %s\n", server.Addr)
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
