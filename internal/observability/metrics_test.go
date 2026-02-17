package observability

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestEnsureMetrics(t *testing.T) {
	if EnsureMetrics(nil) == nil {
		t.Fatal("EnsureMetrics(nil) returned nil")
	}
	m := NewMetrics()
	if EnsureMetrics(m) != m {
		t.Fatal("EnsureMetrics should return provided instance")
	}
}

func TestSafeHelpers_NilInstruments(t *testing.T) {
	ctx := context.Background()
	safeAddCounter(ctx, nil, 1)
	safeAddUpDownCounter(ctx, nil, 1)
	safeRecordHistogram(ctx, nil, 1)
}

func TestMetrics_Recorders(t *testing.T) {
	m := NewMetrics()
	ctx := context.Background()

	m.RecordToolExecution(ctx, "read_file", 10*time.Millisecond, nil)
	m.RecordToolExecution(ctx, "read_file", 10*time.Millisecond, errors.New("boom"))
	m.RecordToolBatch(ctx, 3, 0, 12*time.Millisecond)
	m.RecordToolBatch(ctx, 3, 1, 12*time.Millisecond)

	m.RecordAdapterCall(ctx, "k8s", "list", 5*time.Millisecond, nil)
	m.RecordAdapterCall(ctx, "k8s", "list", 5*time.Millisecond, errors.New("boom"))

	m.RecordGraphOperation(ctx, "query", 8*time.Millisecond, nil)
	m.RecordGraphOperation(ctx, "query", 8*time.Millisecond, errors.New("boom"))

	m.RecordDBOperation(ctx, "insert", 3*time.Millisecond, nil)
	m.RecordDBOperation(ctx, "insert", 3*time.Millisecond, errors.New("boom"))

	m.RecordCacheLookup(ctx, "facts", true, 2*time.Millisecond, nil)
	m.RecordCacheLookup(ctx, "facts", false, 2*time.Millisecond, nil)
	m.RecordCacheLookup(ctx, "facts", false, 2*time.Millisecond, errors.New("boom"))

	m.RecordRefreshCycle(ctx, 1*time.Second, nil)
	m.RecordRefreshCycle(ctx, 1*time.Second, errors.New("boom"))
	m.RecordDiscoveryInput(ctx, 500*time.Millisecond, nil)
	m.RecordDiscoveryInput(ctx, 500*time.Millisecond, errors.New("boom"))

	m.RecordAgentRun(ctx, 100*time.Millisecond, nil)
	m.RecordAgentRun(ctx, 100*time.Millisecond, errors.New("boom"))

	m.RecordSessionStart()
	m.RecordSessionMessage(ctx, "user")
	m.RecordSessionTokens(ctx, 123)
	m.RecordSessionEnd()
}

func TestRegisterBusinessMetrics(t *testing.T) {
	m := NewMetrics()

	unregister, err := m.RegisterBusinessMetrics(BusinessMetricsProvider{
		SourcesByType: func(ctx context.Context) (map[string]int, error) {
			return map[string]int{"k8s": 2}, nil
		},
		GraphSummary: func(ctx context.Context) (GraphMetricsSummary, error) {
			return GraphMetricsSummary{NodeCount: 3, EdgeCount: 4, NodesByType: map[string]int{"service": 1}}, nil
		},
		AdapterCount: func() int { return 5 },
	})
	if err != nil {
		t.Fatalf("RegisterBusinessMetrics error: %v", err)
	}
	if unregister == nil {
		t.Fatal("expected unregister func")
	}
	if err := unregister(); err != nil {
		t.Fatalf("unregister error: %v", err)
	}
}

func TestSetupAndHelpers_Branches(t *testing.T) {
	t.Run("enabled with none exporters", func(t *testing.T) {
		ResetMetricsHandler()
		shutdown, err := Setup(context.Background(), Config{Enabled: true, TracesEnabled: true, TracesExporter: "none", MetricsEnabled: true, MetricsExporter: "none"})
		if err != nil {
			t.Fatalf("Setup error: %v", err)
		}
		if shutdown == nil {
			t.Fatal("expected shutdown")
		}
		if err := shutdown(context.Background()); err != nil {
			t.Fatalf("shutdown error: %v", err)
		}
	})

	t.Run("unknown traces exporter", func(t *testing.T) {
		_, err := Setup(context.Background(), Config{Enabled: true, TracesEnabled: true, TracesExporter: "bad", MetricsEnabled: false})
		if err == nil {
			t.Fatal("expected error for unknown traces exporter")
		}
	})

	t.Run("unknown metrics exporter", func(t *testing.T) {
		_, err := Setup(context.Background(), Config{Enabled: true, TracesEnabled: false, MetricsEnabled: true, MetricsExporter: "bad"})
		if err == nil {
			t.Fatal("expected error for unknown metrics exporter")
		}
	})

	t.Run("env helpers and llm attrs", func(t *testing.T) {
		t.Setenv("OBS_BOOL", "1")
		t.Setenv("OBS_INT", "not-int")
		if got := getEnv("OBS_NONE", "default"); got != "default" {
			t.Fatalf("getEnv got %q", got)
		}
		if !getEnvBool("OBS_BOOL", false) {
			t.Fatal("expected true bool")
		}
		if got := getEnvInt("OBS_INT", 42); got != 42 {
			t.Fatalf("getEnvInt got %d want 42", got)
		}
		attrs := LLMAttributes("claude", "sonnet")
		if len(attrs) != 3 {
			t.Fatalf("unexpected attrs len: %d", len(attrs))
		}
		if Tracer("x") == nil || Meter("x") == nil {
			t.Fatal("expected tracer and meter")
		}
	})
}
