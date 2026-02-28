package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/jaimegago/joe/internal/client"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/llmfactory"
	"github.com/jaimegago/joe/internal/logging"
	"github.com/jaimegago/joe/internal/observability"
	"github.com/jaimegago/joe/internal/paths"
	"github.com/jaimegago/joe/internal/repl"
	"github.com/jaimegago/joe/internal/safety"
	"github.com/jaimegago/joe/internal/tools"
	"github.com/jaimegago/joe/internal/useragent"
)

type replRunner interface {
	Run(ctx context.Context) error
}

type runDeps struct {
	loadConfig  func(path string) (*config.Config, error)
	setupOTel   func(ctx context.Context, cfg observability.Config) (func(context.Context) error, error)
	newMetrics  func() *observability.Metrics
	newAdapter  func(ctx context.Context, mc config.ModelConfig) (llm.LLMAdapter, error)
	joeDirPath  func() (string, error)
	loadPolicy  func(configDir string) (*safety.SafetyPolicy, error)
	newClient   func(baseURL string, opts ...client.ClientOption) *client.Client
	newRegistry func(coreClient *client.Client, policy *safety.SafetyPolicy) *tools.Registry
	newExecutor func(registry *tools.Registry, metrics *observability.Metrics, opts ...tools.ExecutorOption) *tools.Executor
	newRepl     func(agent *useragent.Agent, cfg *config.Config, session *useragent.Session) replRunner
}

func defaultRunDeps() runDeps {
	return runDeps{
		loadConfig:  config.Load,
		setupOTel:   observability.Setup,
		newMetrics:  observability.NewMetrics,
		newAdapter:  llmfactory.NewAdapter,
		joeDirPath:  paths.JoeDirPath,
		loadPolicy:  safety.LoadPolicy,
		newClient:   client.New,
		newRegistry: tools.NewDefaultRegistryWithClient,
		newExecutor: tools.NewExecutor,
		newRepl: func(agent *useragent.Agent, cfg *config.Config, session *useragent.Session) replRunner {
			return repl.NewWithSession(agent, cfg, session)
		},
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return runWithDeps(ctx, args, stdout, stderr, defaultRunDeps())
}

// runPanicCommand sends an emergency shutdown request to joecored.
func runPanicCommand(ctx context.Context, args []string, stdout, stderr io.Writer, deps runDeps) int {
	fs := flag.NewFlagSet("joe panic", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", paths.DefaultConfigPath(), "path to config file")
	reason := fs.String("reason", "operator triggered via CLI", "reason for the emergency shutdown")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfg, err := deps.loadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(stderr, "Error: failed to load config: %v\n", err)
		return 1
	}

	scheme := "http"
	if cfg.Server.TLSEnabled {
		scheme = "https"
	}
	joecoreURL := scheme + "://" + cfg.Server.Address
	var clientOpts []client.ClientOption
	if cfg.Server.APIKey != "" {
		clientOpts = append(clientOpts, client.WithAPIKey(cfg.Server.APIKey))
	}
	c := deps.newClient(joecoreURL, clientOpts...)

	if err := c.TriggerPanic(ctx, *reason); err != nil {
		fmt.Fprintf(stderr, "Error: failed to trigger panic: %v\n", err)
		return 1
	}

	fmt.Fprintln(stdout, "Emergency shutdown triggered. joecored will restart in safe mode.")
	fmt.Fprintln(stdout, "Use 'joe unlock --reason \"...\"' to resume normal operation.")
	return 0
}

// runUnlockCommand exits joecored's safe mode.
func runUnlockCommand(ctx context.Context, args []string, stdout, stderr io.Writer, deps runDeps) int {
	fs := flag.NewFlagSet("joe unlock", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", paths.DefaultConfigPath(), "path to config file")
	reason := fs.String("reason", "", "reason for unlocking (required)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *reason == "" {
		fmt.Fprintln(stderr, "Error: --reason is required")
		fmt.Fprintln(stderr, "Usage: joe unlock --reason \"incident resolved\"")
		return 1
	}

	cfg, err := deps.loadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(stderr, "Error: failed to load config: %v\n", err)
		return 1
	}

	scheme := "http"
	if cfg.Server.TLSEnabled {
		scheme = "https"
	}
	joecoreURL := scheme + "://" + cfg.Server.Address
	var clientOpts []client.ClientOption
	if cfg.Server.APIKey != "" {
		clientOpts = append(clientOpts, client.WithAPIKey(cfg.Server.APIKey))
	}
	c := deps.newClient(joecoreURL, clientOpts...)

	if err := c.Unlock(ctx, *reason); err != nil {
		fmt.Fprintf(stderr, "Error: failed to unlock: %v\n", err)
		return 1
	}

	fmt.Fprintln(stdout, "Safe mode lifted. Normal operation resumed.")
	return 0
}

func runWithDeps(ctx context.Context, args []string, stdout, stderr io.Writer, deps runDeps) int {
	// Dispatch subcommands before parsing REPL flags.
	if len(args) > 0 {
		switch args[0] {
		case "panic":
			return runPanicCommand(ctx, args[1:], stdout, stderr, deps)
		case "unlock":
			return runUnlockCommand(ctx, args[1:], stdout, stderr, deps)
		}
	}

	fs := flag.NewFlagSet("joe", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", paths.DefaultConfigPath(), "path to config file")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	// Initialize a basic logger before config is available
	initialLogger := logging.SetupLogger(logging.LevelInfo)
	slog.SetDefault(initialLogger)

	// Load configuration
	cfg, err := deps.loadConfig(*configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		return 1
	}

	// Initialize OpenTelemetry (default to no tracing in CLI unless explicitly enabled)
	otelCfg := observability.DefaultConfig()
	if _, ok := os.LookupEnv("OTEL_TRACES_ENABLED"); !ok {
		otelCfg.TracesEnabled = false
	}
	if _, ok := os.LookupEnv("OTEL_TRACES_EXPORTER"); !ok {
		otelCfg.TracesExporter = "none"
	}
	shutdownOTel, err := deps.setupOTel(ctx, otelCfg)
	if err != nil {
		slog.Warn("OpenTelemetry setup failed", "error", err)
	} else {
		defer func() { _ = shutdownOTel(context.Background()) }()
	}

	// Create metrics instance
	metrics := deps.newMetrics()

	// Validate LLM configuration and check API keys
	currentModel, err := cfg.LLM.CurrentModel()
	if err != nil {
		fmt.Fprintf(stderr, "You need to connect Joe to an LLM.\n\n%v\n\nCheck your config file's llm.current and llm.available sections.\n", err)
		return 1
	}
	if err := config.ValidateAPIKeysWithUserMessage(currentModel); err != nil {
		fmt.Fprintln(stderr, err.Error())
		fmt.Fprintln(stderr)
		return 1
	}

	// Connect to joecored
	scheme := "http"
	if cfg.Server.TLSEnabled {
		scheme = "https"
	}
	joecoreURL := scheme + "://" + cfg.Server.Address
	var clientOpts []client.ClientOption
	if cfg.Server.APIKey != "" {
		clientOpts = append(clientOpts, client.WithAPIKey(cfg.Server.APIKey))
	}
	if cfg.Server.TLSEnabled {
		clientOpts = append(clientOpts, client.WithTLS())
	}
	coreClient := deps.newClient(joecoreURL, clientOpts...)

	pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
	defer pingCancel()

	if err := coreClient.Ping(pingCtx); err != nil {
		fmt.Fprintf(stderr, "Error: Cannot connect to joecored at %s\n", joecoreURL)
		fmt.Fprintf(stderr, "Make sure joecored is running: joecored\n\n")
		return 1
	}

	// Set up structured logging based on config
	logger, logCleanup := logging.SetupLoggerWithFile(cfg.Logging.Level, cfg.Logging.File)
	defer logCleanup()
	slog.SetDefault(logger)

	// Log debug mode if enabled
	if cfg.Logging.Level == logging.LevelDebug {
		slog.Debug("running in debug mode")
		fmt.Fprintln(stdout, "Debug mode enabled")
	}

	// Initialize LLM adapter using factory
	baseAdapter, err := deps.newAdapter(ctx, currentModel)
	if err != nil {
		slog.Error("failed to create LLM adapter", "error", err)
		return 1
	}

	// Clean up adapter resources (important for Gemini client)
	if closer, ok := baseAdapter.(io.Closer); ok {
		defer closer.Close()
	}

	// Wrap with instrumentation
	llmAdapter := llm.NewInstrumentedAdapter(baseAdapter, logger, currentModel.Provider, currentModel.Model)

	// Log which model we're using
	slog.Info("LLM initialized",
		"provider", currentModel.Provider,
		"model", currentModel.Model,
	)
	fmt.Fprintf(stdout, "Using %s/%s\n", currentModel.Provider, currentModel.Model)

	// Load safety policy from ~/.joe/safety-policy.yaml
	// If the file doesn't exist, DefaultPolicy is used (most restrictive for T3).
	// If the file is malformed, we refuse to start.
	joeDir, err := deps.joeDirPath()
	if err != nil {
		fmt.Fprintf(stderr, "Error: cannot determine Joe config directory: %v\n", err)
		return 1
	}
	safetyPolicy, err := deps.loadPolicy(joeDir)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	slog.Info("safety policy loaded", "config_dir", joeDir)

	// Create tool registry with local tools + core tools (graph_query, graph_related)
	// Pass safety policy so tool-specific settings (e.g., allowed_directories) are enforced.
	registry := deps.newRegistry(coreClient, safetyPolicy)

	// Create REPL notifier for T3 pre-execution countdown and T2/T3 post-execution log
	replNotifier := repl.NewNotifier()

	// Create tool executor with safety policy enforcement and notifications
	executor := deps.newExecutor(registry, metrics,
		tools.WithPolicy(safetyPolicy),
		tools.WithNotifier(replNotifier),
	)

	// Create adapter factory for hot-swapping models
	adapterFactory := func(ctx context.Context, provider, model string) (llm.LLMAdapter, error) {
		// Find the model config
		var modelCfg config.ModelConfig
		found := false
		for _, mc := range cfg.LLM.Available {
			if mc.Provider == provider && mc.Model == model {
				modelCfg = mc
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("model config not found for provider=%s model=%s", provider, model)
		}

		// Validate API keys before creating adapter
		if err := config.ValidateAPIKeys(modelCfg); err != nil {
			return nil, fmt.Errorf("cannot switch to %s: %w", provider, err)
		}

		// Create the base adapter
		baseAdptr, err := llmfactory.NewAdapter(ctx, modelCfg)
		if err != nil {
			return nil, err
		}

		// Wrap with instrumentation
		return llm.NewInstrumentedAdapter(baseAdptr, logger, provider, model), nil
	}

	// Create agent with system prompt and adapter factory
	systemPrompt := `You are Joe, an infrastructure assistant. You can use tools to help answer questions. Be concise.

When you need to access infrastructure resources (Kubernetes, Git, etc.), you'll need source IDs:
- If you don't know the available sources, call list_sources first to discover them
- Then use the source_id from list_sources in subsequent tool calls like k8s_get or k8s_logs
- If there's only one source of the needed type, use that one automatically

You have access to a knowledge store via the search_knowledge tool. Use it proactively when:
- Asked about how something works, known issues, or operational patterns
- Troubleshooting — relevant runbooks or failure modes may already be documented
- Before answering from general knowledge, check if curated or synced docs are available`
	agentInstance := useragent.NewAgent(
		llmAdapter,
		executor,
		registry,
		systemPrompt,
		useragent.WithAdapterFactory(adapterFactory),
		useragent.WithCurrentModelName(cfg.LLM.Current),
	)

	// Create session with message history limit to prevent unbounded growth
	session := useragent.NewSession(metrics)
	session.MaxMessages = useragent.DefaultMaxMessages

	// Create and run REPL (pass config for model management and the session)
	replInstance := deps.newRepl(agentInstance, cfg, session)
	if err := replInstance.Run(ctx); err != nil {
		slog.Error("repl failed", "error", err)
		return 1
	}

	return 0
}

func main() {
	ctx := context.Background()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}
