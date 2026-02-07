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
	// Setup logger
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// Load config
	cfg, err := config.Load("")
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Get listen address from config or default
	addr := cfg.Server.Address
	if addr == "" {
		addr = "localhost:7777"
	}

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
