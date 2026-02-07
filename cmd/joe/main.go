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
	"github.com/jaimegago/joe/internal/logging"
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
	currentModel, err := cfg.LLM.CurrentModel()
	if err != nil {
		fmt.Fprintf(os.Stderr, "You need to connect Joe to an LLM.\n\n%v\n\nCheck your config file's llm.current and llm.available sections.\n", err)
		os.Exit(1)
	}
	if err := config.ValidateAPIKeysWithUserMessage(currentModel); err != nil {
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
	logger, logCleanup := logging.SetupLoggerWithFile(cfg.Logging.Level, cfg.Logging.File)
	defer logCleanup()

	// Log debug mode if enabled
	if cfg.Logging.Level == "debug" {
		slog.Debug("running in debug mode")
		fmt.Println("Debug mode enabled")
	}

	// Initialize LLM adapter using factory
	baseAdapter, err := llmfactory.NewAdapter(ctx, currentModel)
	if err != nil {
		log.Fatalf("Failed to create LLM adapter: %v", err)
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
	systemPrompt := "You are Joe, an infrastructure assistant. You can use tools to help answer questions. Be concise."
	agentInstance := useragent.NewAgent(
		llmAdapter,
		executor,
		registry,
		systemPrompt,
		useragent.WithAdapterFactory(adapterFactory),
		useragent.WithCurrentModelName(cfg.LLM.Current),
	)

	// Create session with message history limit to prevent unbounded growth
	session := useragent.NewSession()
	session.MaxMessages = 100 // Limit to 100 messages

	// Create and run REPL (pass config for model management and the session)
	replInstance := repl.NewWithSession(agentInstance, cfg, session)
	if err := replInstance.Run(ctx); err != nil {
		log.Fatalf("REPL failed: %v", err)
	}

	os.Exit(0)
}
