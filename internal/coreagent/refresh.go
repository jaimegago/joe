package coreagent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jaimegago/joe/internal/adapters"
	awsadapter "github.com/jaimegago/joe/internal/adapters/aws"
	azureadapter "github.com/jaimegago/joe/internal/adapters/azure"
	gitadapter "github.com/jaimegago/joe/internal/adapters/git"
	"github.com/jaimegago/joe/internal/adapters/k8s"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/observability"
	"github.com/jaimegago/joe/internal/store"
)

// Refresher handles background refresh of the graph
type Refresher struct {
	services       *core.Services
	llm            llm.LLMAdapter
	joeFileService *JoeFileService
	logger         *slog.Logger
	metrics        *observability.Metrics
	interval       time.Duration
	stopCh         chan struct{}
	doneCh         chan struct{}
}

// NewRefresher creates a new background refresher
func NewRefresher(services *core.Services, llmAdapter llm.LLMAdapter, logger *slog.Logger, metrics *observability.Metrics) *Refresher {
	joeFileService := NewJoeFileService(services.Store.Cache, llmAdapter, logger, metrics)
	return &Refresher{
		services:       services,
		llm:            llmAdapter,
		joeFileService: joeFileService,
		logger:         logger.With("component", "refresher"),
		metrics:        observability.EnsureMetrics(metrics),
		interval:       5 * time.Minute,
		stopCh:         make(chan struct{}),
		doneCh:         make(chan struct{}),
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
func (r *Refresher) refresh(ctx context.Context) (err error) {
	start := time.Now()
	defer func() { r.metrics.RecordRefreshCycle(ctx, time.Since(start), err) }()
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
		if err := r.refreshSource(ctx, source); err != nil {
			r.logger.Error("failed to refresh source", "source_id", source.ID, "error", err)
			// Continue with other sources even if one fails
		}
	}

	r.logger.Debug("completed refresh cycle")
	return nil
}

// refreshSource refreshes a single infrastructure source
func (r *Refresher) refreshSource(ctx context.Context, source *store.Source) (err error) {
	start := time.Now()
	defer func() {
		lastError := ""
		if err != nil {
			lastError = err.Error()
		}
		if updateErr := r.services.Store.Sources.UpdateSyncStatus(ctx, source.ID, time.Now(), lastError); updateErr != nil {
			r.logger.Warn("failed to update source sync status", "source_id", source.ID, "error", updateErr)
		}
		r.logger.Info("source refresh finished", "source_id", source.ID, "duration_ms", time.Since(start).Milliseconds(), "error", lastError)
	}()

	r.logger.Debug("refreshing source", "source_id", source.ID, "type", source.Type)

	adapter, err := r.services.Adapters.Get(source.ID)
	if err != nil {
		if errors.Is(err, adapters.ErrAdapterNotFound) {
			return fmt.Errorf("adapter not found for source %s", source.ID)
		}
		return fmt.Errorf("get adapter for source %s: %w", source.ID, err)
	}

	switch source.Type {
	case store.SourceTypeKubernetes:
		k8sAdapter, ok := adapter.(k8s.KubernetesAdapter)
		if !ok {
			return fmt.Errorf("adapter for source %s is not kubernetes", source.ID)
		}
		return r.refreshK8sSource(ctx, source, k8sAdapter)
	case store.SourceTypeGit:
		gitAdapter, ok := adapter.(gitadapter.GitAdapter)
		if !ok {
			return fmt.Errorf("adapter for source %s is not git", source.ID)
		}
		return r.refreshGitSource(ctx, source, gitAdapter)
	case store.SourceTypeAWS:
		awsAdapter, ok := adapter.(awsadapter.AWSAdapter)
		if !ok {
			return fmt.Errorf("adapter for source %s is not aws", source.ID)
		}
		return r.refreshAWSSource(ctx, source, awsAdapter)
	case store.SourceTypeAzure:
		azureAdapter, ok := adapter.(azureadapter.AzureAdapter)
		if !ok {
			return fmt.Errorf("adapter for source %s is not azure", source.ID)
		}
		return r.refreshAzureSource(ctx, source, azureAdapter)
	default:
		r.logger.Debug("skipping unsupported source type", "source_id", source.ID, "type", source.Type)
		return nil
	}
}
