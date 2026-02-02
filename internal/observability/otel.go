package observability

import (
	"context"
	"fmt"
	"log"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
)

const (
	serviceName    = "joe"
	serviceVersion = "0.1.0"
)

// Config holds OpenTelemetry configuration
type Config struct {
	Enabled bool

	// Tracing
	TracesEnabled  bool
	TracesExporter string // "stdout", "otlp", "none"
	OTLPEndpoint   string // For OTLP exporter

	// Metrics
	MetricsEnabled  bool
	MetricsExporter string // "prometheus", "none"
	MetricsPort     int    // Prometheus port
}

// DefaultConfig returns default OpenTelemetry configuration
func DefaultConfig() Config {
	return Config{
		Enabled:         getEnvBool("OTEL_ENABLED", true),
		TracesEnabled:   getEnvBool("OTEL_TRACES_ENABLED", true),
		TracesExporter:  getEnv("OTEL_TRACES_EXPORTER", "stdout"),
		OTLPEndpoint:    getEnv("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4317"),
		MetricsEnabled:  getEnvBool("OTEL_METRICS_ENABLED", true),
		MetricsExporter: getEnv("OTEL_METRICS_EXPORTER", "prometheus"),
		MetricsPort:     getEnvInt("OTEL_METRICS_PORT", 9090),
	}
}

// Setup initializes OpenTelemetry with the given configuration
func Setup(ctx context.Context, cfg Config) (func(context.Context) error, error) {
	if !cfg.Enabled {
		log.Println("OpenTelemetry disabled")
		return func(context.Context) error { return nil }, nil
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
			semconv.ServiceVersionKey.String(serviceVersion),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Setup tracing
	var shutdownTraceFn func(context.Context) error
	if cfg.TracesEnabled {
		shutdownTraceFn, err = setupTracing(ctx, cfg, res)
		if err != nil {
			return nil, fmt.Errorf("failed to setup tracing: %w", err)
		}
	}

	// Setup metrics
	var shutdownMetricsFn func(context.Context) error
	if cfg.MetricsEnabled {
		shutdownMetricsFn, err = setupMetrics(ctx, cfg, res)
		if err != nil {
			return nil, fmt.Errorf("failed to setup metrics: %w", err)
		}
	}

	// Return combined shutdown function
	return func(ctx context.Context) error {
		var errs []error
		if shutdownTraceFn != nil {
			if err := shutdownTraceFn(ctx); err != nil {
				errs = append(errs, err)
			}
		}
		if shutdownMetricsFn != nil {
			if err := shutdownMetricsFn(ctx); err != nil {
				errs = append(errs, err)
			}
		}
		if len(errs) > 0 {
			return fmt.Errorf("shutdown errors: %v", errs)
		}
		return nil
	}, nil
}

func setupTracing(ctx context.Context, cfg Config, res *resource.Resource) (func(context.Context) error, error) {
	var exporter sdktrace.SpanExporter
	var err error

	switch cfg.TracesExporter {
	case "stdout":
		exporter, err = stdouttrace.New(stdouttrace.WithPrettyPrint())
	case "otlp":
		client := otlptracegrpc.NewClient(
			otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint),
			otlptracegrpc.WithInsecure(),
		)
		exporter, err = otlptrace.New(ctx, client)
	case "none":
		return func(context.Context) error { return nil }, nil
	default:
		return nil, fmt.Errorf("unknown traces exporter: %s", cfg.TracesExporter)
	}

	if err != nil {
		return nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)

	otel.SetTracerProvider(tp)

	return tp.Shutdown, nil
}

func setupMetrics(ctx context.Context, cfg Config, res *resource.Resource) (func(context.Context) error, error) {
	var reader sdkmetric.Reader
	var err error

	switch cfg.MetricsExporter {
	case "prometheus":
		reader, err = prometheus.New()
	case "none":
		return func(context.Context) error { return nil }, nil
	default:
		return nil, fmt.Errorf("unknown metrics exporter: %s", cfg.MetricsExporter)
	}

	if err != nil {
		return nil, err
	}

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(reader),
		sdkmetric.WithResource(res),
	)

	otel.SetMeterProvider(mp)

	return mp.Shutdown, nil
}

// Tracer returns a tracer for the given name
func Tracer(name string) trace.Tracer {
	return otel.Tracer(name)
}

// Meter returns a meter for the given name
func Meter(name string) metric.Meter {
	return otel.Meter(name)
}

// Helper functions
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		return value == "true" || value == "1"
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		var i int
		if _, err := fmt.Sscanf(value, "%d", &i); err == nil {
			return i
		}
	}
	return defaultValue
}

// Common attributes for LLM operations
func LLMAttributes(provider, model string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("llm.provider", provider),
		attribute.String("llm.model", model),
		attribute.String("service.name", serviceName),
	}
}
