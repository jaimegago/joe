package observability

import (
	"context"
	"fmt"
	"time"

	"github.com/jaimegago/joe/internal/llm"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const (
	instrumentationName = "joe/llm"
)

// LLMMiddleware wraps an LLM adapter with OpenTelemetry instrumentation
type LLMMiddleware struct {
	adapter  llm.LLMAdapter
	provider string
	model    string

	// OpenTelemetry primitives
	tracer trace.Tracer
	meter  metric.Meter

	// Metrics
	callCounter       metric.Int64Counter
	errorCounter      metric.Int64Counter
	durationHistogram metric.Float64Histogram
	tokenCounter      metric.Int64Counter
}

// NewLLMMiddleware creates a new instrumented LLM middleware
func NewLLMMiddleware(adapter llm.LLMAdapter, provider, model string) (*LLMMiddleware, error) {
	tracer := Tracer(instrumentationName)
	meter := Meter(instrumentationName)

	// Create metrics
	callCounter, err := meter.Int64Counter(
		"llm.calls",
		metric.WithDescription("Number of LLM API calls"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create call counter: %w", err)
	}

	errorCounter, err := meter.Int64Counter(
		"llm.errors",
		metric.WithDescription("Number of LLM API errors"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create error counter: %w", err)
	}

	durationHistogram, err := meter.Float64Histogram(
		"llm.duration",
		metric.WithDescription("LLM API call duration"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create duration histogram: %w", err)
	}

	tokenCounter, err := meter.Int64Counter(
		"llm.tokens",
		metric.WithDescription("LLM token usage"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create token counter: %w", err)
	}

	return &LLMMiddleware{
		adapter:           adapter,
		provider:          provider,
		model:             model,
		tracer:            tracer,
		meter:             meter,
		callCounter:       callCounter,
		errorCounter:      errorCounter,
		durationHistogram: durationHistogram,
		tokenCounter:      tokenCounter,
	}, nil
}

// Chat implements llm.LLMAdapter with full OpenTelemetry instrumentation
func (m *LLMMiddleware) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	// Start span
	ctx, span := m.tracer.Start(ctx, "llm.chat",
		trace.WithAttributes(
			attribute.String("llm.provider", m.provider),
			attribute.String("llm.model", m.model),
			attribute.Int("llm.messages.count", len(req.Messages)),
			attribute.Int("llm.tools.count", len(req.Tools)),
		),
	)
	defer span.End()

	start := time.Now()

	// Common attributes for metrics
	attrs := metric.WithAttributes(
		attribute.String("provider", m.provider),
		attribute.String("model", m.model),
	)

	// Increment call counter
	m.callCounter.Add(ctx, 1, attrs)

	// Make the actual API call
	resp, err := m.adapter.Chat(ctx, req)
	duration := time.Since(start)

	// Record duration
	m.durationHistogram.Record(ctx, float64(duration.Milliseconds()), attrs)

	if err != nil {
		// Record error
		m.errorCounter.Add(ctx, 1, attrs)
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		return nil, err
	}

	// Record token usage
	m.tokenCounter.Add(ctx, int64(resp.Usage.InputTokens),
		metric.WithAttributes(
			attribute.String("provider", m.provider),
			attribute.String("model", m.model),
			attribute.String("token_type", "input"),
		),
	)
	m.tokenCounter.Add(ctx, int64(resp.Usage.OutputTokens),
		metric.WithAttributes(
			attribute.String("provider", m.provider),
			attribute.String("model", m.model),
			attribute.String("token_type", "output"),
		),
	)

	// Add response attributes to span
	span.SetAttributes(
		attribute.Int("llm.tokens.input", resp.Usage.InputTokens),
		attribute.Int("llm.tokens.output", resp.Usage.OutputTokens),
		attribute.Int("llm.tokens.total", resp.Usage.TotalTokens),
		attribute.Int("llm.tool_calls.count", len(resp.ToolCalls)),
		attribute.Int64("llm.duration_ms", duration.Milliseconds()),
	)

	span.SetStatus(codes.Ok, "")
	return resp, nil
}

// ChatStream implements llm.LLMAdapter with OpenTelemetry instrumentation
func (m *LLMMiddleware) ChatStream(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	// Start span
	ctx, span := m.tracer.Start(ctx, "llm.chat_stream",
		trace.WithAttributes(
			attribute.String("llm.provider", m.provider),
			attribute.String("llm.model", m.model),
			attribute.Int("llm.messages.count", len(req.Messages)),
			attribute.Int("llm.tools.count", len(req.Tools)),
		),
	)
	defer span.End()

	start := time.Now()

	attrs := metric.WithAttributes(
		attribute.String("provider", m.provider),
		attribute.String("model", m.model),
		attribute.String("operation", "stream"),
	)

	m.callCounter.Add(ctx, 1, attrs)

	stream, err := m.adapter.ChatStream(ctx, req)
	duration := time.Since(start)

	m.durationHistogram.Record(ctx, float64(duration.Milliseconds()), attrs)

	if err != nil {
		m.errorCounter.Add(ctx, 1, attrs)
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		return nil, err
	}

	span.SetStatus(codes.Ok, "")
	return stream, nil
}

// Embed implements llm.LLMAdapter with OpenTelemetry instrumentation
func (m *LLMMiddleware) Embed(ctx context.Context, text string) ([]float32, error) {
	// Start span
	ctx, span := m.tracer.Start(ctx, "llm.embed",
		trace.WithAttributes(
			attribute.String("llm.provider", m.provider),
			attribute.String("llm.model", m.model),
			attribute.Int("llm.text.length", len(text)),
		),
	)
	defer span.End()

	start := time.Now()

	attrs := metric.WithAttributes(
		attribute.String("provider", m.provider),
		attribute.String("model", m.model),
		attribute.String("operation", "embed"),
	)

	m.callCounter.Add(ctx, 1, attrs)

	embedding, err := m.adapter.Embed(ctx, text)
	duration := time.Since(start)

	m.durationHistogram.Record(ctx, float64(duration.Milliseconds()), attrs)

	if err != nil {
		m.errorCounter.Add(ctx, 1, attrs)
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		return nil, err
	}

	span.SetAttributes(
		attribute.Int("llm.embedding.dimensions", len(embedding)),
		attribute.Int64("llm.duration_ms", duration.Milliseconds()),
	)

	span.SetStatus(codes.Ok, "")
	return embedding, nil
}
