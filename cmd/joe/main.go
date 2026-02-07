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
	"github.com/jaimegago/joe/internal/llm/claude"
	"github.com/jaimegago/joe/internal/llm/gemini"
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

	// Initialize LLM adapter based on config
	var baseAdapter llm.LLMAdapter

	switch cfg.LLM.Provider {
	case "claude":
		baseAdapter, err = claude.NewClient(cfg.LLM.Model)
		if err != nil {
			log.Fatalf("Failed to create Claude client: %v", err)
		}
	case "gemini":
		baseAdapter, err = gemini.NewClient(ctx, cfg.LLM.Model)
		if err != nil {
			log.Fatalf("Failed to create Gemini client: %v", err)
		}
	default:
		log.Fatalf("Unknown LLM provider: %s (supported: claude, gemini)", cfg.LLM.Provider)
	}

	// Wrap with instrumentation
	llmAdapter := llm.NewInstrumentedAdapter(baseAdapter, logger, cfg.LLM.Provider, cfg.LLM.Model)

	// Create tool registry with default tools (echo, ask_user)
	registry := tools.NewDefaultRegistry()

	// Create tool executor
	executor := tools.NewExecutor(registry)

	// Create agent with system prompt
	systemPrompt := "You are Joe, an infrastructure assistant. You can use tools to help answer questions. Be concise."
	agentInstance := useragent.NewAgent(llmAdapter, executor, registry, systemPrompt)

	// Create and run REPL
	replInstance := repl.New(agentInstance)
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
	// Check if provider is supported
	supportedProviders := []string{"claude", "gemini"}
	providerSupported := false
	for _, p := range supportedProviders {
		if cfg.LLM.Provider == p {
			providerSupported = true
			break
		}
	}

	if !providerSupported {
		return fmt.Errorf("You need to connect Joe to an LLM.\n\nCurrently supported LLMs:\n  - Claude (Anthropic)\n  - Gemini (Google)\n\nConfigured provider '%s' is not supported.", cfg.LLM.Provider)
	}

	// Check for API keys (must be set and non-empty)
	switch cfg.LLM.Provider {
	case "claude":
		apiKey := os.Getenv("ANTHROPIC_API_KEY")
		if apiKey == "" {
			return fmt.Errorf("You need to connect Joe to an LLM.\n\nClaude is configured but ANTHROPIC_API_KEY is not set or is empty.\n\nCurrently supported LLMs:\n  - Claude (Anthropic) - requires ANTHROPIC_API_KEY\n  - Gemini (Google) - requires GEMINI_API_KEY or GOOGLE_API_KEY\n\nTo use Claude:\n  export ANTHROPIC_API_KEY=your-api-key-here\n\nTo use Gemini, update your config to provider: gemini")
		}
		// Warn if model looks like it's for the wrong provider
		if len(cfg.LLM.Model) > 6 && cfg.LLM.Model[:6] == "gemini" {
			return fmt.Errorf("Configuration error: provider is set to 'claude' but model '%s' appears to be a Gemini model.\n\nValid Claude models include:\n  - claude-sonnet-4-20250514\n  - claude-opus-4-20241229\n  - claude-3-5-sonnet-20241022\n\nUpdate your config file to use a Claude model.", cfg.LLM.Model)
		}
	case "gemini":
		geminiKey := os.Getenv("GEMINI_API_KEY")
		googleKey := os.Getenv("GOOGLE_API_KEY")
		if geminiKey == "" && googleKey == "" {
			return fmt.Errorf("You need to connect Joe to an LLM.\n\nGemini is configured but neither GEMINI_API_KEY nor GOOGLE_API_KEY is set or both are empty.\n\nCurrently supported LLMs:\n  - Claude (Anthropic) - requires ANTHROPIC_API_KEY\n  - Gemini (Google) - requires GEMINI_API_KEY or GOOGLE_API_KEY\n\nTo use Gemini:\n  export GEMINI_API_KEY=your-api-key-here\n\nTo use Claude, update your config to provider: claude")
		}
		// Warn if model looks like it's for the wrong provider
		if len(cfg.LLM.Model) > 6 && cfg.LLM.Model[:6] == "claude" {
			return fmt.Errorf("Configuration error: provider is set to 'gemini' but model '%s' appears to be a Claude model.\n\nValid Gemini models include:\n  - gemini-1.5-flash (recommended - fast and stable)\n  - gemini-1.5-pro (more capable)\n  - gemini-2.0-flash-exp (experimental)\n\nUpdate your config file to use a Gemini model.", cfg.LLM.Model)
		}
	}

	return nil
}
