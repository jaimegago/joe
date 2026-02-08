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

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/adapters/k8s"
	"github.com/jaimegago/joe/internal/api"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/logging"
	"github.com/jaimegago/joe/internal/paths"
	"github.com/jaimegago/joe/internal/store"
)

func main() {
	// Setup initial logger at info level
	initialLogger := logging.SetupLogger("info")
	slog.SetDefault(initialLogger)

	// Load config (defaults to ~/.joe/config.yaml if exists, otherwise uses hardcoded defaults)
	cfg, err := config.Load(paths.DefaultConfigPath())
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
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

	// Initialize store
	joeDir, err := paths.JoeDirPath()
	if err != nil {
		slog.Error("failed to get joe directory path", "error", err)
		os.Exit(1)
	}

	if err := os.MkdirAll(joeDir, 0755); err != nil {
		slog.Error("failed to create joe directory", "error", err)
		os.Exit(1)
	}

	dbPath, err := paths.DatabasePath()
	if err != nil {
		slog.Error("failed to get database path", "error", err)
		os.Exit(1)
	}

	sqlStore, err := store.New(dbPath + paths.DatabaseFlags)
	if err != nil {
		slog.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer sqlStore.Close()

	if err := sqlStore.Migrate(); err != nil {
		slog.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}
	slog.Info("database ready", "path", dbPath)

	// Initialize adapter registry and load saved sources
	adapterRegistry := adapters.NewRegistry()

	ctx := context.Background()
	k8sSources, err := sqlStore.Sources.ListByType(ctx, "kubernetes")
	if err != nil {
		slog.Warn("failed to load kubernetes sources", "error", err)
	}
	for _, src := range k8sSources {
		adapter := k8s.New()
		if err := adapter.Connect(*src); err != nil {
			slog.Warn("failed to connect k8s source", "id", src.ID, "error", err)
			continue
		}
		adapterRegistry.Register(src.ID, adapter)
		slog.Info("connected k8s source", "id", src.ID, "name", src.Name)
	}

	// Initialize core services (graph store uses same SQLite DB)
	services := core.New(cfg, sqlStore, sqlStore.DB(), adapterRegistry)
	defer services.Close()
	slog.Info("core services ready", "graph_store", "sqlite", "adapters", len(adapterRegistry.List()))

	// Get listen address from config (defaults to localhost:7777)
	addr := cfg.Server.Address

	// Setup HTTP server
	mux := http.NewServeMux()

	// Register API routes
	apiServer := api.New(services)
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
