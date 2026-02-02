package llm

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const meterName = "github.com/jaimegago/joe/internal/llm"

// APIErrorDetails interface for errors that carry API error details
type APIErrorDetails interface {
	error
	APICode() int
	APIMessage() string
}

// InstrumentedAdapter wraps an LLMAdapter with instrumentation
// Tracks API calls, token usage, latency, and errors using OpenTelemetry metrics
type InstrumentedAdapter struct {
	adapter  LLMAdapter
	logger   *slog.Logger
	provider string
	model    string

	// In-memory counters (atomic for thread safety, used for GetStats)
	totalCalls        atomic.Int64
	totalErrors       atomic.Int64
	totalInputTokens  atomic.Int64
	totalOutputTokens atomic.Int64

	// OTel metrics
	requestCounter     metric.Int64Counter
	errorCounter       metric.Int64Counter
	inputTokenCounter  metric.Int64Counter
	outputTokenCounter metric.Int64Counter
	latencyHistogram   metric.Float64Histogram
}

// NewInstrumentedAdapter wraps an LLM adapter with instrumentation
func NewInstrumentedAdapter(adapter LLMAdapter, logger *slog.Logger, provider, model string) *InstrumentedAdapter {
	if logger == nil {
		logger = slog.Default()
	}

	meter := otel.Meter(meterName)

	// Create OTel metrics - log warnings on failure but continue
	// Metrics will be nil if creation fails, handled in recording methods
	requestCounter, err := meter.Int64Counter("llm.requests",
		metric.WithDescription("Total number of LLM API requests"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		logger.Warn("failed to create llm.requests metric", "error", err)
	}

	errorCounter, err := meter.Int64Counter("llm.errors",
		metric.WithDescription("Total number of LLM API errors"),
		metric.WithUnit("{error}"),
	)
	if err != nil {
		logger.Warn("failed to create llm.errors metric", "error", err)
	}

	inputTokenCounter, err := meter.Int64Counter("llm.tokens.input",
		metric.WithDescription("Total input tokens consumed"),
		metric.WithUnit("{token}"),
	)
	if err != nil {
		logger.Warn("failed to create llm.tokens.input metric", "error", err)
	}

	outputTokenCounter, err := meter.Int64Counter("llm.tokens.output",
		metric.WithDescription("Total output tokens consumed"),
		metric.WithUnit("{token}"),
	)
	if err != nil {
		logger.Warn("failed to create llm.tokens.output metric", "error", err)
	}

	latencyHistogram, err := meter.Float64Histogram("llm.request.duration",
		metric.WithDescription("LLM request latency"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		logger.Warn("failed to create llm.request.duration metric", "error", err)
	}

	return &InstrumentedAdapter{
		adapter:            adapter,
		logger:             logger,
		provider:           provider,
		model:              model,
		requestCounter:     requestCounter,
		errorCounter:       errorCounter,
		inputTokenCounter:  inputTokenCounter,
		outputTokenCounter: outputTokenCounter,
		latencyHistogram:   latencyHistogram,
	}
}

// safeAddCounter safely adds to a counter, handling nil metrics
func safeAddCounter(ctx context.Context, counter metric.Int64Counter, value int64, attrs ...attribute.KeyValue) {
	if counter != nil {
		counter.Add(ctx, value, metric.WithAttributes(attrs...))
	}
}

// safeRecordHistogram safely records to a histogram, handling nil metrics
func safeRecordHistogram(ctx context.Context, hist metric.Float64Histogram, value float64, attrs ...attribute.KeyValue) {
	if hist != nil {
		hist.Record(ctx, value, metric.WithAttributes(attrs...))
	}
}

// Chat implements LLMAdapter with instrumentation
func (i *InstrumentedAdapter) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	start := time.Now()
	i.totalCalls.Add(1)

	// Common attributes for all metrics
	attrs := []attribute.KeyValue{
		attribute.String("llm.provider", i.provider),
		attribute.String("llm.model", i.model),
		attribute.String("operation", "chat"),
	}

	// Record OTel request metric
	safeAddCounter(ctx, i.requestCounter, 1, attrs...)

	// Make the actual API call
	resp, err := i.adapter.Chat(ctx, req)
	duration := time.Since(start)

	// Record OTel latency
	latencyAttrs := append(attrs, attribute.Bool("error", err != nil))
	safeRecordHistogram(ctx, i.latencyHistogram, float64(duration.Milliseconds()), latencyAttrs...)

	if err != nil {
		i.totalErrors.Add(1)

		// Record OTel error metric and log
		var apiErr APIErrorDetails
		if errors.As(err, &apiErr) {
			errorAttrs := append(attrs, attribute.Int("api_error_code", apiErr.APICode()))
			safeAddCounter(ctx, i.errorCounter, 1, errorAttrs...)
			i.logger.Error("llm_error",
				"error", err,
				"provider", i.provider,
				"model", i.model,
				"api_error_code", apiErr.APICode(),
				"api_error_msg", apiErr.APIMessage(),
				"duration_ms", duration.Milliseconds(),
			)
		} else {
			safeAddCounter(ctx, i.errorCounter, 1, attrs...)
			i.logger.Error("llm_error",
				"error", err,
				"provider", i.provider,
				"model", i.model,
				"duration_ms", duration.Milliseconds(),
			)
		}
		return nil, err
	}

	// Track token usage (both in-memory and OTel)
	i.totalInputTokens.Add(int64(resp.Usage.InputTokens))
	i.totalOutputTokens.Add(int64(resp.Usage.OutputTokens))

	safeAddCounter(ctx, i.inputTokenCounter, int64(resp.Usage.InputTokens), attrs...)
	safeAddCounter(ctx, i.outputTokenCounter, int64(resp.Usage.OutputTokens), attrs...)

	return resp, nil
}

// ChatStream implements LLMAdapter with instrumentation
func (i *InstrumentedAdapter) ChatStream(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error) {
	start := time.Now()
	i.totalCalls.Add(1)

	attrs := []attribute.KeyValue{
		attribute.String("llm.provider", i.provider),
		attribute.String("llm.model", i.model),
		attribute.String("operation", "chat_stream"),
	}

	safeAddCounter(ctx, i.requestCounter, 1, attrs...)

	stream, err := i.adapter.ChatStream(ctx, req)
	duration := time.Since(start)

	latencyAttrs := append(attrs, attribute.Bool("error", err != nil))
	safeRecordHistogram(ctx, i.latencyHistogram, float64(duration.Milliseconds()), latencyAttrs...)

	if err != nil {
		i.totalErrors.Add(1)
		safeAddCounter(ctx, i.errorCounter, 1, attrs...)
		i.logger.Error("llm_stream_error",
			"error", err,
			"provider", i.provider,
			"model", i.model,
			"duration_ms", duration.Milliseconds(),
		)
		return nil, err
	}

	return stream, nil
}

// Embed implements LLMAdapter with instrumentation
func (i *InstrumentedAdapter) Embed(ctx context.Context, text string) ([]float32, error) {
	start := time.Now()
	i.totalCalls.Add(1)

	attrs := []attribute.KeyValue{
		attribute.String("llm.provider", i.provider),
		attribute.String("llm.model", i.model),
		attribute.String("operation", "embed"),
	}

	safeAddCounter(ctx, i.requestCounter, 1, attrs...)

	embedding, err := i.adapter.Embed(ctx, text)
	duration := time.Since(start)

	latencyAttrs := append(attrs, attribute.Bool("error", err != nil))
	safeRecordHistogram(ctx, i.latencyHistogram, float64(duration.Milliseconds()), latencyAttrs...)

	if err != nil {
		i.totalErrors.Add(1)
		safeAddCounter(ctx, i.errorCounter, 1, attrs...)
		i.logger.Error("llm_embed_error",
			"error", err,
			"provider", i.provider,
			"model", i.model,
			"duration_ms", duration.Milliseconds(),
		)
		return nil, err
	}

	return embedding, nil
}

// Stats holds instrumentation statistics
type Stats struct {
	TotalCalls        int64
	TotalErrors       int64
	TotalInputTokens  int64
	TotalOutputTokens int64
	TotalTokens       int64
}

// GetStats returns the current instrumentation statistics
func (i *InstrumentedAdapter) GetStats() Stats {
	input := i.totalInputTokens.Load()
	output := i.totalOutputTokens.Load()

	return Stats{
		TotalCalls:        i.totalCalls.Load(),
		TotalErrors:       i.totalErrors.Load(),
		TotalInputTokens:  input,
		TotalOutputTokens: output,
		TotalTokens:       input + output,
	}
}
