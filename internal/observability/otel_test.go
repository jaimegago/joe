package observability

import (
	"net/http"
	"testing"
)

func TestDefaultConfig_EnvOverrides(t *testing.T) {
	t.Setenv("OTEL_ENABLED", "false")
	t.Setenv("OTEL_TRACES_ENABLED", "false")
	t.Setenv("OTEL_TRACES_EXPORTER", "none")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "example:4317")
	t.Setenv("OTEL_METRICS_ENABLED", "false")
	t.Setenv("OTEL_METRICS_EXPORTER", "none")
	t.Setenv("OTEL_METRICS_PORT", "1234")

	cfg := DefaultConfig()

	if cfg.Enabled {
		t.Fatalf("Enabled = true, want false")
	}
	if cfg.TracesEnabled {
		t.Fatalf("TracesEnabled = true, want false")
	}
	if cfg.TracesExporter != "none" {
		t.Fatalf("TracesExporter = %q, want %q", cfg.TracesExporter, "none")
	}
	if cfg.OTLPEndpoint != "example:4317" {
		t.Fatalf("OTLPEndpoint = %q, want %q", cfg.OTLPEndpoint, "example:4317")
	}
	if cfg.MetricsEnabled {
		t.Fatalf("MetricsEnabled = true, want false")
	}
	if cfg.MetricsExporter != "none" {
		t.Fatalf("MetricsExporter = %q, want %q", cfg.MetricsExporter, "none")
	}
	if cfg.MetricsPort != 1234 {
		t.Fatalf("MetricsPort = %d, want %d", cfg.MetricsPort, 1234)
	}
}

func TestResetMetricsHandler(t *testing.T) {
	metricsHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	if MetricsHandler() == nil {
		t.Fatal("expected metrics handler to be set")
	}
	ResetMetricsHandler()
	if MetricsHandler() != nil {
		t.Fatal("expected metrics handler to be nil after reset")
	}
}
