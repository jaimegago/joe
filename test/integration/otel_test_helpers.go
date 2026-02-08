//go:build integration
// +build integration

package integration

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// OTelTestHarness provides in-memory capture of OpenTelemetry metrics and traces
// for testing instrumentation without external collectors.
type OTelTestHarness struct {
	t              *testing.T
	spanRecorder   *tracetest.SpanRecorder
	meterProvider  *sdkmetric.MeterProvider
	tracerProvider *sdktrace.TracerProvider
	metricReader   *sdkmetric.ManualReader

	// Store original providers for cleanup
	originalMeterProvider  metric.MeterProvider
	originalTracerProvider trace.TracerProvider
}

// NewOTelTestHarness creates a new test harness with in-memory collectors
func NewOTelTestHarness(t *testing.T) *OTelTestHarness {
	t.Helper()

	// Create span recorder for traces
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(spanRecorder),
	)

	// Create manual metric reader for metrics
	metricReader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(metricReader),
	)

	return &OTelTestHarness{
		t:              t,
		spanRecorder:   spanRecorder,
		meterProvider:  meterProvider,
		tracerProvider: tracerProvider,
		metricReader:   metricReader,
	}
}

// Install replaces global OTel providers with test providers
func (h *OTelTestHarness) Install() {
	h.t.Helper()

	// Store original providers
	h.originalMeterProvider = otel.GetMeterProvider()
	h.originalTracerProvider = otel.GetTracerProvider()

	// Install test providers
	otel.SetMeterProvider(h.meterProvider)
	otel.SetTracerProvider(h.tracerProvider)

	h.t.Log("Installed OTel test harness")
}

// Cleanup restores original OTel providers and cleans up resources
func (h *OTelTestHarness) Cleanup() {
	h.t.Helper()

	// Restore original providers (always restore, even if nil)
	otel.SetMeterProvider(h.originalMeterProvider)
	otel.SetTracerProvider(h.originalTracerProvider)

	// Shutdown providers
	ctx := context.Background()
	if h.meterProvider != nil {
		h.meterProvider.Shutdown(ctx)
	}
	if h.tracerProvider != nil {
		h.tracerProvider.Shutdown(ctx)
	}

	h.t.Log("Cleaned up OTel test harness")
}

// GetMetrics collects all metrics from the manual reader
func (h *OTelTestHarness) GetMetrics() metricdata.ResourceMetrics {
	h.t.Helper()

	var rm metricdata.ResourceMetrics
	err := h.metricReader.Collect(context.Background(), &rm)
	if err != nil {
		h.t.Fatalf("Failed to collect metrics: %v", err)
	}

	return rm
}

// GetSpans returns all recorded spans
func (h *OTelTestHarness) GetSpans() []sdktrace.ReadOnlySpan {
	h.t.Helper()
	return h.spanRecorder.Ended()
}

// AssertCounterIncremented verifies a counter metric was incremented with specific attributes
func (h *OTelTestHarness) AssertCounterIncremented(metricName string, expectedValue int64, attrs ...attribute.KeyValue) {
	h.t.Helper()

	rm := h.GetMetrics()

	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != metricName {
				continue
			}

			// Check if it's a counter
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				h.t.Fatalf("Metric %s is not an int64 counter", metricName)
			}

			// Find matching datapoint by attributes
			for _, dp := range sum.DataPoints {
				if attributesMatch(dp.Attributes, attrs) {
					if dp.Value == expectedValue {
						h.t.Logf("✓ Counter %s incremented to %d with matching attributes", metricName, expectedValue)
						return
					}
					h.t.Fatalf("Counter %s has value %d, expected %d (attributes: %v)",
						metricName, dp.Value, expectedValue, attrs)
				}
			}
		}
	}

	h.t.Fatalf("Counter metric %s not found with attributes %v", metricName, attrs)
}

// AssertCounterIncreased verifies a counter metric increased by at least the expected value
func (h *OTelTestHarness) AssertCounterIncreased(metricName string, minIncrease int64, attrs ...attribute.KeyValue) {
	h.t.Helper()

	rm := h.GetMetrics()

	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != metricName {
				continue
			}

			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				h.t.Fatalf("Metric %s is not an int64 counter", metricName)
			}

			for _, dp := range sum.DataPoints {
				if attributesMatch(dp.Attributes, attrs) {
					if dp.Value >= minIncrease {
						h.t.Logf("✓ Counter %s increased by %d (>= %d) with matching attributes",
							metricName, dp.Value, minIncrease)
						return
					}
					h.t.Fatalf("Counter %s increased by %d, expected at least %d (attributes: %v)",
						metricName, dp.Value, minIncrease, attrs)
				}
			}
		}
	}

	h.t.Fatalf("Counter metric %s not found with attributes %v", metricName, attrs)
}

// AssertHistogramRecorded verifies a histogram metric recorded at least one value
func (h *OTelTestHarness) AssertHistogramRecorded(metricName string, attrs ...attribute.KeyValue) {
	h.t.Helper()

	rm := h.GetMetrics()

	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != metricName {
				continue
			}

			hist, ok := m.Data.(metricdata.Histogram[float64])
			if !ok {
				h.t.Fatalf("Metric %s is not a float64 histogram", metricName)
			}

			for _, dp := range hist.DataPoints {
				if attributesMatch(dp.Attributes, attrs) {
					if dp.Count > 0 {
						h.t.Logf("✓ Histogram %s recorded %d values with matching attributes",
							metricName, dp.Count)
						return
					}
					h.t.Fatalf("Histogram %s has no recorded values (attributes: %v)",
						metricName, attrs)
				}
			}
		}
	}

	h.t.Fatalf("Histogram metric %s not found with attributes %v", metricName, attrs)
}

// AssertSpanExists verifies a span with the given name exists
func (h *OTelTestHarness) AssertSpanExists(spanName string) sdktrace.ReadOnlySpan {
	h.t.Helper()

	spans := h.GetSpans()
	for _, span := range spans {
		if span.Name() == spanName {
			h.t.Logf("✓ Span %s exists", spanName)
			return span
		}
	}

	h.t.Fatalf("Span %s not found (total spans: %d)", spanName, len(spans))
	return nil
}

// AssertSpanHasAttribute verifies a span has a specific attribute
func (h *OTelTestHarness) AssertSpanHasAttribute(span sdktrace.ReadOnlySpan, key string, expectedValue interface{}) {
	h.t.Helper()

	attrs := span.Attributes()
	for _, attr := range attrs {
		if string(attr.Key) == key {
			// Check value match
			actualValue := attr.Value.AsInterface()
			if actualValue == expectedValue {
				h.t.Logf("✓ Span %s has attribute %s=%v", span.Name(), key, expectedValue)
				return
			}
			h.t.Fatalf("Span %s has attribute %s=%v, expected %v",
				span.Name(), key, actualValue, expectedValue)
		}
	}

	h.t.Fatalf("Span %s missing attribute %s", span.Name(), key)
}

// AssertSpanStatus verifies a span has the expected status code
func (h *OTelTestHarness) AssertSpanStatus(span sdktrace.ReadOnlySpan, expectedStatus codes.Code) {
	h.t.Helper()

	status := span.Status()
	if status.Code == expectedStatus {
		h.t.Logf("✓ Span %s has status %v", span.Name(), expectedStatus)
		return
	}

	h.t.Fatalf("Span %s has status %v, expected %v (description: %s)",
		span.Name(), status.Code, expectedStatus, status.Description)
}

// AssertSpanHasError verifies a span has recorded error events
func (h *OTelTestHarness) AssertSpanHasError(span sdktrace.ReadOnlySpan) {
	h.t.Helper()

	events := span.Events()
	for _, event := range events {
		if event.Name == "exception" {
			h.t.Logf("✓ Span %s has error event", span.Name())
			return
		}
	}

	h.t.Fatalf("Span %s has no error events (total events: %d)", span.Name(), len(events))
}

// Wait allows time for async instrumentation to complete
func (h *OTelTestHarness) Wait(duration time.Duration) {
	h.t.Helper()
	time.Sleep(duration)
}

// attributesMatch checks if datapoint attributes contain all expected attributes
func attributesMatch(dpAttrs attribute.Set, expected []attribute.KeyValue) bool {
	for _, exp := range expected {
		val, exists := dpAttrs.Value(exp.Key)
		if !exists {
			return false
		}
		if val != exp.Value {
			return false
		}
	}
	return true
}
