package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jaimegago/joe/internal/api"
	"github.com/jaimegago/joe/internal/config"
)

func main() {
	// Setup initial logger at info level
	initialLogger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(initialLogger)

	// Load config (defaults to ~/.joe/config.yaml if exists, otherwise uses hardcoded defaults)
	configPath := "~/.joe/config.yaml"
	cfg, err := config.Load(configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Reconfigure logger based on config level
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

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	}))
	slog.SetDefault(logger)

	// Log debug mode if enabled
	if cfg.Logging.Level == "debug" {
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

	// Get listen address from config (defaults to localhost:7777)
	addr := cfg.Server.Address

	// Setup HTTP server
	mux := http.NewServeMux()

	// Register API routes
	apiServer := api.New()
	apiServer.RegisterRoutes(mux)

	server := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	// Start server in goroutine
	go func() {
		slog.Info("joecored starting", "addr", addr)
		fmt.Printf("joecored listening on %s\n", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	// TODO: Start Core Agent background refresh here
	slog.Info("core agent ready (background refresh not yet implemented)")

	// Wait for shutdown signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		slog.Error("shutdown error", "error", err)
	}
	slog.Info("joecored stopped")
}
