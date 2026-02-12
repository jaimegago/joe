package coreagent

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/observability"
	"github.com/jaimegago/joe/internal/store"
)

// Engine handles onboarding and .joe/ file processing
type Engine struct {
	services *core.Services
	llm      llm.LLMAdapter
	logger   *slog.Logger
	metrics  *observability.Metrics
}

// NewEngine creates a new discovery engine
func NewEngine(services *core.Services, llmAdapter llm.LLMAdapter, logger *slog.Logger, metrics *observability.Metrics) *Engine {
	return &Engine{
		services: services,
		llm:      llmAdapter,
		logger:   logger.With("component", "discovery"),
		metrics:  observability.EnsureMetrics(metrics),
	}
}

// ProcessInput handles onboarding input from users
func (e *Engine) ProcessInput(ctx context.Context, input string) (err error) {
	start := time.Now()
	defer func() { e.metrics.RecordDiscoveryInput(ctx, time.Since(start), err) }()

	e.logger.Info("processing onboarding input", "input_length", len(input))

	// Phase 5 MVP: Store the input as an onboarding fact
	// Future phases will add LLM interpretation and graph discovery

	fact := &store.OnboardingFact{
		FactType: "user_input",
		Subject:  "onboarding",
		Content:  input,
		Source:   "discovery-engine",
	}

	err = e.services.Store.Facts.Create(ctx, fact)
	if err != nil {
		e.logger.Error("failed to store onboarding input", "error", err)
		return fmt.Errorf("failed to store onboarding input: %w", err)
	}

	e.logger.Info("stored onboarding input as fact", "fact_id", fact.ID)
	return nil
}

// DiscoverInfrastructure performs automated infrastructure discovery
func (e *Engine) DiscoverInfrastructure(ctx context.Context, sourceID int64) error {
	e.logger.Info("discovering infrastructure", "source_id", sourceID)

	// Phase 5 MVP: Placeholder for future LLM-powered discovery
	// Future implementation will:
	// 1. Connect to infrastructure sources
	// 2. Query for resources
	// 3. Use LLM to interpret and categorize findings
	// 4. Update the knowledge graph

	e.logger.Info("infrastructure discovery completed", "source_id", sourceID)
	return nil
}
