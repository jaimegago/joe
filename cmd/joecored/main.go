package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jaimegago/joe/internal/adapters"
	awsadapter "github.com/jaimegago/joe/internal/adapters/aws"
	azureadapter "github.com/jaimegago/joe/internal/adapters/azure"
	gitadapter "github.com/jaimegago/joe/internal/adapters/git"
	"github.com/jaimegago/joe/internal/adapters/k8s"
	datadogadapter "github.com/jaimegago/joe/internal/adapters/observability/datadog"
	dynatraceadapter "github.com/jaimegago/joe/internal/adapters/observability/dynatrace"
	newrelicadapter "github.com/jaimegago/joe/internal/adapters/observability/newrelic"
	splunkadapter "github.com/jaimegago/joe/internal/adapters/observability/splunk"
	falcoadapter "github.com/jaimegago/joe/internal/adapters/security/falco"
	"github.com/jaimegago/joe/internal/api"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/coreagent"
	"github.com/jaimegago/joe/internal/crypto"
	"github.com/jaimegago/joe/internal/knowledge"
	"github.com/jaimegago/joe/internal/knowledge/embeddings"
	knowledgesync "github.com/jaimegago/joe/internal/knowledge/sync"
	"github.com/jaimegago/joe/internal/knowledge/sync/confluence"
	"github.com/jaimegago/joe/internal/knowledge/sync/notion"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/llmfactory"
	"github.com/jaimegago/joe/internal/logging"
	"github.com/jaimegago/joe/internal/observability"
	"github.com/jaimegago/joe/internal/paths"
	"github.com/jaimegago/joe/internal/store"
)

type coreAgentRunner interface {
	core.CoreAgent
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

type runDeps struct {
	loadConfig             func(path string) (*config.Config, error)
	setupOTel              func(ctx context.Context, cfg observability.Config) (func(context.Context) error, error)
	defaultOTelConfig      func() observability.Config
	newMetrics             func() *observability.Metrics
	joeDirPath             func() (string, error)
	mkdirAll               func(path string, perm os.FileMode) error
	databasePath           func() (string, error)
	newStore               func(path string, metrics *observability.Metrics) (*store.Store, error)
	migrateStore           func(store *store.Store) error
	closeStore             func(store *store.Store) error
	newAdapterRegistry     func() *adapters.Registry
	connectSources         func(ctx context.Context, store *store.Store, registry *adapters.Registry)
	newServices            func(cfg *config.Config, store *store.Store, db *sql.DB, registry *adapters.Registry, metrics *observability.Metrics) *core.Services
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
	return runWithDeps(ctx, defaultRunDeps())
}

func runWithDeps(ctx context.Context, deps runDeps) int {
	// Setup initial logger at info level
	initialLogger := logging.SetupLogger("info")
	slog.SetDefault(initialLogger)

	// Load config (defaults to ~/.joe/config.yaml if exists, otherwise uses hardcoded defaults)
	cfg, err := deps.loadConfig(paths.DefaultConfigPath())
	if err != nil {
		slog.Error("failed to load config", "error", err)
		return 1
	}

	// Reconfigure logger based on config level
	logger := logging.SetupLogger(cfg.Logging.Level)
	slog.SetDefault(logger)

	// Log debug mode if enabled
	if cfg.Logging.Level == logging.LevelDebug {
		slog.Debug("running in debug mode")
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

	sqlStore, err := deps.newStore(dbPath+paths.DatabaseFlags, metrics)
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
	slog.Info("database ready", "path", dbPath)

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
	services := deps.newServices(cfg, sqlStore, sqlStore.DB(), adapterRegistry, metrics)
	defer services.Close()
	slog.Info("core services ready", "graph_store", "sqlite", "adapters", len(adapterRegistry.List()))

	// Register business metrics gauges
	if err := deps.registerBusinessMetric(services); err != nil {
		slog.Warn("failed to register business metrics", "error", err)
	}

	// Get listen address from config (defaults to localhost:7777)
	addr := cfg.Server.Address

	// Initialize LLM adapter for Core Agent
	currentModelCfg, err := cfg.LLM.CurrentModel()
	if err != nil {
		slog.Error("failed to get current model config for core agent", "error", err)
		return 1
	}

	llmAdapter, err := deps.newLLMAdapter(ctx, currentModelCfg)
	if err != nil {
		slog.Error("failed to initialize LLM adapter for core agent", "error", err)
		return 1
	}

	// Wire the LLM embedder into the Knowledge Service now that the adapter is ready.
	embModelName := cfg.Knowledge.EmbeddingModel
	if embModelName == "" {
		embModelName = cfg.LLM.Current
	}
	embedder := embeddings.New(llmAdapter, embModelName)
	services.Knowledge = knowledge.NewService(sqlStore.Knowledge, embedder)
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
	apiServer.RegisterRoutes(mux)

	// Build middleware chain: rate limit → metrics → auth → request size limit → mux
	handler := api.Chain(
		mux,
		api.RateLimit(cfg.Server.RateLimitRPS, cfg.Server.RateLimitBurst),
		func(h http.Handler) http.Handler {
			return observability.HTTPMetricsMiddleware(h, metrics)
		},
		api.BearerAuth(cfg.Server.APIKey),
		api.MaxRequestBody(api.DefaultMaxRequestBytes),
	)

	if cfg.Server.APIKey != "" {
		slog.Info("API authentication enabled")
	} else {
		slog.Warn("API authentication disabled — set server.api_key in config or JOE_API_KEY env var")
	}
	if cfg.Server.RateLimitRPS > 0 {
		slog.Info("API rate limiting enabled", "rps", cfg.Server.RateLimitRPS, "burst", cfg.Server.RateLimitBurst)
	}

	server := &http.Server{
		Addr:         addr,
		Handler:      handler,
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
		slog.Warn("TLS disabled — connections to joecored are unencrypted")
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
	slog.Info("joecored stopped")

	return 0
}

func defaultWaitForShutdown(ctx context.Context) <-chan struct{} {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	done := make(chan struct{})
	go func() {
		select {
		case <-quit:
			close(done)
		case <-ctx.Done():
			close(done)
		}
	}()
	return done
}

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
		slog.Info("joecored starting", "addr", server.Addr, "tls", certFile != "")
		fmt.Printf("joecored listening on %s\n", server.Addr)
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
