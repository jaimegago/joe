package core_test

import (
	"context"
	"errors"
	"testing"

	datadogadapter "github.com/jaimegago/joe/internal/adapters/observability/datadog"
	dynatraceadapter "github.com/jaimegago/joe/internal/adapters/observability/dynatrace"
	newrelicadapter "github.com/jaimegago/joe/internal/adapters/observability/newrelic"
	splunkadapter "github.com/jaimegago/joe/internal/adapters/observability/splunk"
	"github.com/jaimegago/joe/internal/tools/core"
)

// ---- DatadogQueryTool ----

type fakeDatadogClient struct {
	metricsFunc func(ctx context.Context, sourceID, query string, from, to int64) (*datadogadapter.MetricsResult, error)
	logsFunc    func(ctx context.Context, sourceID, query string, from, to int64, limit int) (*datadogadapter.LogsResult, error)
}

func (f *fakeDatadogClient) DatadogMetricsQuery(ctx context.Context, sourceID, query string, from, to int64) (*datadogadapter.MetricsResult, error) {
	return f.metricsFunc(ctx, sourceID, query, from, to)
}

func (f *fakeDatadogClient) DatadogLogsSearch(ctx context.Context, sourceID, query string, from, to int64, limit int) (*datadogadapter.LogsResult, error) {
	return f.logsFunc(ctx, sourceID, query, from, to, limit)
}

func TestDatadogQueryTool(t *testing.T) {
	fake := &fakeDatadogClient{
		metricsFunc: func(_ context.Context, sourceID, query string, _, _ int64) (*datadogadapter.MetricsResult, error) {
			if sourceID == "dd-prod" {
				return &datadogadapter.MetricsResult{Query: query, Series: []datadogadapter.MetricSeries{}}, nil
			}
			return nil, errors.New("source not found")
		},
		logsFunc: func(_ context.Context, sourceID, query string, _, _ int64, _ int) (*datadogadapter.LogsResult, error) {
			if sourceID == "dd-prod" {
				return &datadogadapter.LogsResult{Logs: []datadogadapter.LogEntry{{Message: "error"}}, Count: 1}, nil
			}
			return nil, errors.New("source not found")
		},
	}
	tool := core.NewDatadogQueryTool(fake)

	t.Run("name and metadata", func(t *testing.T) {
		if tool.Name() != "datadog_query" {
			t.Errorf("Name() = %q, want datadog_query", tool.Name())
		}
		if tool.Description() == "" {
			t.Error("Description() should not be empty")
		}
		params := tool.Parameters()
		if params.Type != "object" {
			t.Errorf("Parameters().Type = %q, want object", params.Type)
		}
		if _, ok := params.Properties["source_id"]; !ok {
			t.Error("Parameters() missing source_id")
		}
		if _, ok := params.Properties["query"]; !ok {
			t.Error("Parameters() missing query")
		}
	})

	t.Run("missing source_id", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{"query": "avg:system.cpu.user{*}"})
		if err == nil {
			t.Error("expected error for missing source_id")
		}
	})

	t.Run("missing query", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{"source_id": "dd-prod"})
		if err == nil {
			t.Error("expected error for missing query")
		}
	})

	t.Run("metrics success", func(t *testing.T) {
		res, err := tool.Execute(context.Background(), map[string]any{
			"source_id": "dd-prod",
			"query":     "avg:system.cpu.user{*}",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m := res.(map[string]any)
		if m["source_id"] != "dd-prod" {
			t.Errorf("source_id = %v, want dd-prod", m["source_id"])
		}
	})

	t.Run("metrics client error", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{
			"source_id": "bad-source",
			"query":     "avg:system.cpu.user{*}",
		})
		if err == nil {
			t.Error("expected error from client")
		}
	})

	t.Run("logs success", func(t *testing.T) {
		res, err := tool.Execute(context.Background(), map[string]any{
			"source_id": "dd-prod",
			"query":     "service:payment status:error",
			"action":    "logs",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m := res.(map[string]any)
		if m["count"].(int) != 1 {
			t.Errorf("count = %v, want 1", m["count"])
		}
	})

	t.Run("logs with limit", func(t *testing.T) {
		res, err := tool.Execute(context.Background(), map[string]any{
			"source_id": "dd-prod",
			"query":     "service:payment",
			"action":    "logs",
			"limit":     float64(50),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res == nil {
			t.Error("expected non-nil result")
		}
	})

	t.Run("logs client error", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{
			"source_id": "bad-source",
			"query":     "service:payment",
			"action":    "logs",
		})
		if err == nil {
			t.Error("expected error from client")
		}
	})

	t.Run("metrics with custom time range", func(t *testing.T) {
		res, err := tool.Execute(context.Background(), map[string]any{
			"source_id": "dd-prod",
			"query":     "avg:system.cpu.user{*}",
			"from":      float64(1700000000),
			"to":        float64(1700003600),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res == nil {
			t.Error("expected non-nil result")
		}
	})
}

// ---- SplunkQueryTool ----

type fakeSplunkClient struct {
	fn func(ctx context.Context, sourceID, query, earliest, latest string, limit int) (*splunkadapter.SearchResult, error)
}

func (f *fakeSplunkClient) SplunkSearch(ctx context.Context, sourceID, query, earliest, latest string, limit int) (*splunkadapter.SearchResult, error) {
	return f.fn(ctx, sourceID, query, earliest, latest, limit)
}

func TestSplunkQueryTool(t *testing.T) {
	fake := &fakeSplunkClient{
		fn: func(_ context.Context, sourceID, query, _, _ string, _ int) (*splunkadapter.SearchResult, error) {
			if sourceID == "splunk-1" {
				return &splunkadapter.SearchResult{Events: []splunkadapter.SearchEvent{{Raw: "error log"}}, Count: 1}, nil
			}
			return nil, errors.New("source not found")
		},
	}
	tool := core.NewSplunkQueryTool(fake)

	t.Run("name and metadata", func(t *testing.T) {
		if tool.Name() != "splunk_query" {
			t.Errorf("Name() = %q, want splunk_query", tool.Name())
		}
		if tool.Description() == "" {
			t.Error("Description() should not be empty")
		}
		params := tool.Parameters()
		if params.Type != "object" {
			t.Errorf("Parameters().Type = %q, want object", params.Type)
		}
		if _, ok := params.Properties["source_id"]; !ok {
			t.Error("Parameters() missing source_id")
		}
	})

	t.Run("missing source_id", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{"query": "index=main error"})
		if err == nil {
			t.Error("expected error for missing source_id")
		}
	})

	t.Run("missing query", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{"source_id": "splunk-1"})
		if err == nil {
			t.Error("expected error for missing query")
		}
	})

	t.Run("success with defaults", func(t *testing.T) {
		res, err := tool.Execute(context.Background(), map[string]any{
			"source_id": "splunk-1",
			"query":     "index=main error",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m := res.(map[string]any)
		if m["count"].(int) != 1 {
			t.Errorf("count = %v, want 1", m["count"])
		}
		if m["source_id"] != "splunk-1" {
			t.Errorf("source_id = %v, want splunk-1", m["source_id"])
		}
	})

	t.Run("success with custom params", func(t *testing.T) {
		res, err := tool.Execute(context.Background(), map[string]any{
			"source_id": "splunk-1",
			"query":     "index=main error",
			"earliest":  "-24h",
			"latest":    "now",
			"limit":     float64(50),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res == nil {
			t.Error("expected non-nil result")
		}
	})

	t.Run("client error", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{
			"source_id": "bad-source",
			"query":     "index=main error",
		})
		if err == nil {
			t.Error("expected error from client")
		}
	})
}

// ---- DynatraceQueryTool ----

type fakeDynatraceClient struct {
	metricsFunc func(ctx context.Context, sourceID, query string, from, to int64) (*dynatraceadapter.MetricsResult, error)
	eventsFunc  func(ctx context.Context, sourceID string, from, to int64, limit int) (*dynatraceadapter.EventsResult, error)
}

func (f *fakeDynatraceClient) DynatraceMetricsQuery(ctx context.Context, sourceID, query string, from, to int64) (*dynatraceadapter.MetricsResult, error) {
	return f.metricsFunc(ctx, sourceID, query, from, to)
}

func (f *fakeDynatraceClient) DynatraceEvents(ctx context.Context, sourceID string, from, to int64, limit int) (*dynatraceadapter.EventsResult, error) {
	return f.eventsFunc(ctx, sourceID, from, to, limit)
}

func TestDynatraceQueryTool(t *testing.T) {
	fake := &fakeDynatraceClient{
		metricsFunc: func(_ context.Context, sourceID, query string, _, _ int64) (*dynatraceadapter.MetricsResult, error) {
			if sourceID == "dt-1" {
				return &dynatraceadapter.MetricsResult{Series: []dynatraceadapter.MetricSeries{}}, nil
			}
			return nil, errors.New("source not found")
		},
		eventsFunc: func(_ context.Context, sourceID string, _, _ int64, _ int) (*dynatraceadapter.EventsResult, error) {
			if sourceID == "dt-1" {
				return &dynatraceadapter.EventsResult{Events: []dynatraceadapter.DynatraceEvent{{Title: "Deploy"}}, Count: 1}, nil
			}
			return nil, errors.New("source not found")
		},
	}
	tool := core.NewDynatraceQueryTool(fake)

	t.Run("name and metadata", func(t *testing.T) {
		if tool.Name() != "dynatrace_query" {
			t.Errorf("Name() = %q, want dynatrace_query", tool.Name())
		}
		if tool.Description() == "" {
			t.Error("Description() should not be empty")
		}
		params := tool.Parameters()
		if params.Type != "object" {
			t.Errorf("Parameters().Type = %q, want object", params.Type)
		}
		if _, ok := params.Properties["source_id"]; !ok {
			t.Error("Parameters() missing source_id")
		}
	})

	t.Run("missing source_id", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{})
		if err == nil {
			t.Error("expected error for missing source_id")
		}
	})

	t.Run("metrics missing query", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{
			"source_id": "dt-1",
			"action":    "metrics",
		})
		if err == nil {
			t.Error("expected error for missing query in metrics action")
		}
	})

	t.Run("metrics success", func(t *testing.T) {
		res, err := tool.Execute(context.Background(), map[string]any{
			"source_id": "dt-1",
			"query":     "builtin:host.cpu.usage:avg",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m := res.(map[string]any)
		if m["source_id"] != "dt-1" {
			t.Errorf("source_id = %v, want dt-1", m["source_id"])
		}
	})

	t.Run("metrics client error", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{
			"source_id": "bad",
			"query":     "builtin:host.cpu.usage:avg",
		})
		if err == nil {
			t.Error("expected error from client")
		}
	})

	t.Run("events success", func(t *testing.T) {
		res, err := tool.Execute(context.Background(), map[string]any{
			"source_id": "dt-1",
			"action":    "events",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m := res.(map[string]any)
		if m["count"].(int) != 1 {
			t.Errorf("count = %v, want 1", m["count"])
		}
	})

	t.Run("events with custom params", func(t *testing.T) {
		res, err := tool.Execute(context.Background(), map[string]any{
			"source_id": "dt-1",
			"action":    "events",
			"limit":     float64(20),
			"from":      float64(1700000000000),
			"to":        float64(1700003600000),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res == nil {
			t.Error("expected non-nil result")
		}
	})

	t.Run("events client error", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{
			"source_id": "bad",
			"action":    "events",
		})
		if err == nil {
			t.Error("expected error from client")
		}
	})
}

// ---- NewRelicQueryTool ----

type fakeNewRelicClient struct {
	fn func(ctx context.Context, sourceID string, accountID int, query string) (*newrelicadapter.NRQLResult, error)
}

func (f *fakeNewRelicClient) NewRelicNRQLQuery(ctx context.Context, sourceID string, accountID int, query string) (*newrelicadapter.NRQLResult, error) {
	return f.fn(ctx, sourceID, accountID, query)
}

func TestNewRelicQueryTool(t *testing.T) {
	fake := &fakeNewRelicClient{
		fn: func(_ context.Context, sourceID string, _ int, query string) (*newrelicadapter.NRQLResult, error) {
			if sourceID == "nr-1" {
				return &newrelicadapter.NRQLResult{
					Results:  []map[string]any{{"count": 42}},
					Metadata: newrelicadapter.NRQLMetadata{},
				}, nil
			}
			return nil, errors.New("source not found")
		},
	}
	tool := core.NewNewRelicQueryTool(fake)

	t.Run("name and metadata", func(t *testing.T) {
		if tool.Name() != "newrelic_query" {
			t.Errorf("Name() = %q, want newrelic_query", tool.Name())
		}
		if tool.Description() == "" {
			t.Error("Description() should not be empty")
		}
		params := tool.Parameters()
		if params.Type != "object" {
			t.Errorf("Parameters().Type = %q, want object", params.Type)
		}
		if _, ok := params.Properties["source_id"]; !ok {
			t.Error("Parameters() missing source_id")
		}
	})

	t.Run("missing source_id", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{"query": "SELECT count(*) FROM Transaction"})
		if err == nil {
			t.Error("expected error for missing source_id")
		}
	})

	t.Run("missing query", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{"source_id": "nr-1"})
		if err == nil {
			t.Error("expected error for missing query")
		}
	})

	t.Run("success with default account_id", func(t *testing.T) {
		res, err := tool.Execute(context.Background(), map[string]any{
			"source_id": "nr-1",
			"query":     "SELECT count(*) FROM Transaction SINCE 1 hour ago",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m := res.(map[string]any)
		if m["count"].(int) != 1 {
			t.Errorf("count = %v, want 1", m["count"])
		}
		if m["source_id"] != "nr-1" {
			t.Errorf("source_id = %v, want nr-1", m["source_id"])
		}
	})

	t.Run("success with explicit account_id", func(t *testing.T) {
		res, err := tool.Execute(context.Background(), map[string]any{
			"source_id":  "nr-1",
			"query":      "SELECT count(*) FROM Transaction",
			"account_id": float64(12345),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res == nil {
			t.Error("expected non-nil result")
		}
	})

	t.Run("client error", func(t *testing.T) {
		_, err := tool.Execute(context.Background(), map[string]any{
			"source_id": "bad-source",
			"query":     "SELECT count(*) FROM Transaction",
		})
		if err == nil {
			t.Error("expected error from client")
		}
	})
}
