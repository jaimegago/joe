package coreagent

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/llm"
)

// Refresher handles background refresh of the graph
type Refresher struct {
	services *core.Services
	llm      llm.LLMAdapter
	logger   *slog.Logger
	interval time.Duration
	stopCh   chan struct{}
	doneCh   chan struct{}
}

// NewRefresher creates a new background refresher
func NewRefresher(services *core.Services, llmAdapter llm.LLMAdapter, logger *slog.Logger) *Refresher {
	return &Refresher{
		services: services,
		llm:      llmAdapter,
		logger:   logger.With("component", "refresher"),
		interval: 5 * time.Minute, // Default refresh interval
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
}

// Start begins the background refresh loop
func (r *Refresher) Start(ctx context.Context) error {
	r.logger.Info("starting background refresh", "interval", r.interval)

	go r.refreshLoop(ctx)
	return nil
}

// Stop gracefully stops the background refresh
func (r *Refresher) Stop(ctx context.Context) error {
	r.logger.Info("stopping background refresh")

	close(r.stopCh)

	// Wait for refresh loop to finish, with timeout
	select {
	case <-r.doneCh:
		r.logger.Info("background refresh stopped")
		return nil
	case <-time.After(10 * time.Second):
		r.logger.Warn("background refresh stop timeout")
		return fmt.Errorf("timeout waiting for refresh to stop")
	}
}

// refreshLoop is the main background refresh loop
func (r *Refresher) refreshLoop(ctx context.Context) {
	defer close(r.doneCh)

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := r.refresh(ctx); err != nil {
				r.logger.Error("refresh cycle failed", "error", err)
			}
		case <-r.stopCh:
			r.logger.Info("refresh loop stopping")
			return
		case <-ctx.Done():
			r.logger.Info("refresh loop stopping due to context cancellation")
			return
		}
	}
}

// refresh performs a single refresh cycle
func (r *Refresher) refresh(ctx context.Context) error {
	r.logger.Debug("starting refresh cycle")

	// Phase 5 MVP: Basic refresh structure
	// Future implementation will:
	// 1. Load connected sources from store
	// 2. For each source, query current state
	// 3. Diff against existing graph
	// 4. Apply deterministic changes
	// 5. Queue ambiguous findings for clarification

	// Load sources
	sources, err := r.services.Store.Sources.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to load sources: %w", err)
	}

	r.logger.Debug("loaded sources for refresh", "count", len(sources))

	for _, source := range sources {
		if err := r.refreshSource(ctx, source.ID); err != nil {
			r.logger.Error("failed to refresh source", "source_id", source.ID, "error", err)
			// Continue with other sources even if one fails
		}
	}

	r.logger.Debug("completed refresh cycle")
	return nil
}

// refreshSource refreshes a single infrastructure source
func (r *Refresher) refreshSource(ctx context.Context, sourceID string) error {
	r.logger.Debug("refreshing source", "source_id", sourceID)

	// Phase 5 MVP: Placeholder for source-specific refresh logic
	// Future implementation will connect to the actual infrastructure
	// and update the knowledge graph based on current state

	return nil
}
