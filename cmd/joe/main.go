package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"time"

	"github.com/jaimegago/joe/internal/client"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/llmfactory"
	"github.com/jaimegago/joe/internal/repl"
	"github.com/jaimegago/joe/internal/tools"
	"github.com/jaimegago/joe/internal/useragent"
)

func main() {
	// Parse command-line flags
	configPath := flag.String("config", "~/.joe/config.yaml", "path to config file")
	flag.Parse()

	ctx := context.Background()

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Validate LLM configuration and check API keys
	if err := validateLLMConfig(cfg); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		fmt.Fprintln(os.Stderr)
		os.Exit(1)
	}

	// Connect to joecored
	joecoreURL := "http://" + cfg.Server.Address
	coreClient := client.New(joecoreURL)

	pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
	defer pingCancel()

	if err := coreClient.Ping(pingCtx); err != nil {
		fmt.Fprintf(os.Stderr, "Error: Cannot connect to joecored at %s\n", joecoreURL)
		fmt.Fprintf(os.Stderr, "Make sure joecored is running: joecored\n\n")
		os.Exit(1)
	}

	// Set up structured logging based on config
	logger, logCleanup := setupLogger(cfg)
	defer logCleanup()

	// Log debug mode if enabled
	if cfg.Logging.Level == "debug" {
		slog.Debug("running in debug mode")
		fmt.Println("Debug mode enabled")
	}

	// Initialize LLM adapter using factory
	currentModel, err := cfg.LLM.CurrentModel()
	if err != nil {
		log.Fatalf("Invalid LLM config: %v", err)
	}

	baseAdapter, err := llmfactory.NewAdapter(ctx, currentModel)
	if err != nil {
		log.Fatalf("Failed to create LLM adapter: %v", err)
	}

	// Wrap with instrumentation
	llmAdapter := llm.NewInstrumentedAdapter(baseAdapter, logger, currentModel.Provider, currentModel.Model)

	// Log which model we're using
	slog.Info("LLM initialized",
		"provider", currentModel.Provider,
		"model", currentModel.Model,
	)
	fmt.Printf("Using %s/%s\n", currentModel.Provider, currentModel.Model)

	// Create tool registry with default tools (echo, ask_user)
	registry := tools.NewDefaultRegistry()

	// Create tool executor
	executor := tools.NewExecutor(registry)

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
		switch provider {
		case "claude":
			if os.Getenv("ANTHROPIC_API_KEY") == "" {
				return nil, fmt.Errorf("cannot switch to Claude: ANTHROPIC_API_KEY environment variable not set")
			}
		case "gemini":
			geminiKey := os.Getenv("GEMINI_API_KEY")
			googleKey := os.Getenv("GOOGLE_API_KEY")
			if geminiKey == "" && googleKey == "" {
				return nil, fmt.Errorf("cannot switch to Gemini: neither GEMINI_API_KEY nor GOOGLE_API_KEY environment variable is set")
			}
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
	systemPrompt := "You are Joe, an infrastructure assistant. You can use tools to help answer questions. Be concise."
	agentInstance := useragent.NewAgent(
		llmAdapter,
		executor,
		registry,
		systemPrompt,
		useragent.WithAdapterFactory(adapterFactory),
		useragent.WithCurrentModelName(cfg.LLM.Current),
	)

	// Create and run REPL (pass config for model management)
	replInstance := repl.New(agentInstance, cfg)
	if err := replInstance.Run(ctx); err != nil {
		log.Fatalf("REPL failed: %v", err)
	}

	os.Exit(0)
}

// setupLogger creates a structured logger based on config
// Returns the logger and a cleanup function to close any opened files
func setupLogger(cfg *config.Config) (*slog.Logger, func()) {
	// Determine log level
	var level slog.Level
	switch cfg.Logging.Level {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	// Create handler options
	opts := &slog.HandlerOptions{
		Level: level,
	}

	// Determine output (file or discard)
	var handler slog.Handler
	var cleanup func() = func() {} // No-op by default

	if cfg.Logging.File != "" {
		// Log to file
		file, err := os.OpenFile(cfg.Logging.File, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			log.Printf("Failed to open log file %s: %v, disabling logging", cfg.Logging.File, err)
			handler = slog.NewTextHandler(io.Discard, opts)
		} else {
			handler = slog.NewJSONHandler(file, opts)
			cleanup = func() { file.Close() }
		}
	} else {
		// No log file configured - discard logs to keep REPL clean
		handler = slog.NewTextHandler(io.Discard, opts)
	}

	return slog.New(handler), cleanup
}

// validateLLMConfig checks if LLM is properly configured with API keys
func validateLLMConfig(cfg *config.Config) error {
	mc, err := cfg.LLM.CurrentModel()
	if err != nil {
		return fmt.Errorf("You need to connect Joe to an LLM.\n\n%w\n\nCheck your config file's llm.current and llm.available sections.", err)
	}

	// Check if provider is supported
	supportedProviders := []string{"claude", "gemini"}
	providerSupported := false
	for _, p := range supportedProviders {
		if mc.Provider == p {
			providerSupported = true
			break
		}
	}

	if !providerSupported {
		return fmt.Errorf("You need to connect Joe to an LLM.\n\nCurrently supported LLMs:\n  - Claude (Anthropic)\n  - Gemini (Google)\n\nConfigured provider '%s' is not supported.", mc.Provider)
	}

	// Check for API keys (must be set and non-empty)
	switch mc.Provider {
	case "claude":
		apiKey := os.Getenv("ANTHROPIC_API_KEY")
		if apiKey == "" {
			return fmt.Errorf("You need to connect Joe to an LLM.\n\nClaude is configured but ANTHROPIC_API_KEY is not set or is empty.\n\nCurrently supported LLMs:\n  - Claude (Anthropic) - requires ANTHROPIC_API_KEY\n  - Gemini (Google) - requires GEMINI_API_KEY or GOOGLE_API_KEY\n\nTo use Claude:\n  export ANTHROPIC_API_KEY=your-api-key-here\n\nTo use Gemini, update your config to use a Gemini model")
		}
	case "gemini":
		geminiKey := os.Getenv("GEMINI_API_KEY")
		googleKey := os.Getenv("GOOGLE_API_KEY")
		if geminiKey == "" && googleKey == "" {
			return fmt.Errorf("You need to connect Joe to an LLM.\n\nGemini is configured but neither GEMINI_API_KEY nor GOOGLE_API_KEY is set or both are empty.\n\nCurrently supported LLMs:\n  - Claude (Anthropic) - requires ANTHROPIC_API_KEY\n  - Gemini (Google) - requires GEMINI_API_KEY or GOOGLE_API_KEY\n\nTo use Gemini:\n  export GEMINI_API_KEY=your-api-key-here\n\nTo use Claude, update your config to use a Claude model")
		}
	}

	return nil
}
