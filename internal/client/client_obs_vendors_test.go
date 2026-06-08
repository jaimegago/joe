package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- Datadog ---

func TestDatadogMetricsQuery_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("DatadogMetricsQuery: expected GET, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"result": map[string]any{
				"query": "avg:system.cpu.user{*}",
				"from":  int64(1000),
				"to":    int64(2000),
				"series": []map[string]any{
					{"metric": "system.cpu.user", "points": [][]float64{{1000000, 45.2}}},
				},
			},
			"component_id": "dd-prod",
		})
	}))
	defer ts.Close()

	c := New(ts.URL)
	result, err := c.DatadogMetricsQuery(context.Background(), "dd-prod", "avg:system.cpu.user{*}", 1000, 2000)
	if err != nil {
		t.Fatalf("DatadogMetricsQuery() error: %v", err)
	}
	if result == nil {
		t.Fatal("DatadogMetricsQuery(): got nil result")
	}
	if len(result.Series) != 1 {
		t.Errorf("DatadogMetricsQuery(): got %d series, want 1", len(result.Series))
	}
}

func TestDatadogMetricsQuery_URLConstruction(t *testing.T) {
	var capturedURI string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURI = r.RequestURI
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"result": nil, "component_id": "dd-1"})
	}))
	defer ts.Close()

	c := New(ts.URL)
	_, _ = c.DatadogMetricsQuery(context.Background(), "dd-1", "avg:cpu{*}", 100, 200)
	assertContains(t, capturedURI, "/api/v1/datadog/dd-1/metrics")
	assertContains(t, capturedURI, "from=100")
	assertContains(t, capturedURI, "to=200")
}

func TestDatadogMetricsQuery_Error(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "internal", "message": "dd unavailable"})
	}))
	defer ts.Close()

	c := New(ts.URL)
	_, err := c.DatadogMetricsQuery(context.Background(), "dd-prod", "query", 0, 0)
	if err == nil {
		t.Fatal("DatadogMetricsQuery(): expected error for 500 response")
	}
}

func TestDatadogLogsSearch_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"result": map[string]any{
				"logs": []map[string]any{
					{"id": "log-1", "service": "payment", "message": "error occurred"},
				},
				"count": 1,
			},
			"component_id": "dd-prod",
		})
	}))
	defer ts.Close()

	c := New(ts.URL)
	result, err := c.DatadogLogsSearch(context.Background(), "dd-prod", "service:payment error", 1000, 2000, 50)
	if err != nil {
		t.Fatalf("DatadogLogsSearch() error: %v", err)
	}
	if result == nil {
		t.Fatal("DatadogLogsSearch(): got nil result")
	}
	if len(result.Logs) != 1 {
		t.Errorf("DatadogLogsSearch(): got %d logs, want 1", len(result.Logs))
	}
}

func TestDatadogLogsSearch_URLConstruction(t *testing.T) {
	var capturedURI string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURI = r.RequestURI
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"result": nil, "component_id": "dd-1"})
	}))
	defer ts.Close()

	c := New(ts.URL)
	_, _ = c.DatadogLogsSearch(context.Background(), "dd-1", "service:api", 100, 200, 25)
	assertContains(t, capturedURI, "/api/v1/datadog/dd-1/logs")
	assertContains(t, capturedURI, "from=100")
	assertContains(t, capturedURI, "to=200")
	assertContains(t, capturedURI, "limit=25")
}

func TestDatadogLogsSearch_Error(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "internal", "message": "dd logs error"})
	}))
	defer ts.Close()

	c := New(ts.URL)
	_, err := c.DatadogLogsSearch(context.Background(), "dd-prod", "query", 0, 0, 10)
	if err == nil {
		t.Fatal("DatadogLogsSearch(): expected error for 500 response")
	}
}

// --- Splunk ---

func TestSplunkSearch_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("SplunkSearch: expected GET, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"result": map[string]any{
				"events": []map[string]any{
					{"_raw": "error: timeout", "host": "web-01"},
				},
				"count": 1,
			},
			"component_id": "splunk-prod",
		})
	}))
	defer ts.Close()

	c := New(ts.URL)
	result, err := c.SplunkSearch(context.Background(), "splunk-prod", "index=main error", "-1h", "now", 100)
	if err != nil {
		t.Fatalf("SplunkSearch() error: %v", err)
	}
	if result == nil {
		t.Fatal("SplunkSearch(): got nil result")
	}
	if len(result.Events) != 1 {
		t.Errorf("SplunkSearch(): got %d events, want 1", len(result.Events))
	}
}

func TestSplunkSearch_URLConstruction(t *testing.T) {
	var capturedURI string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURI = r.RequestURI
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"result": nil, "component_id": "splunk-1"})
	}))
	defer ts.Close()

	c := New(ts.URL)
	_, _ = c.SplunkSearch(context.Background(), "splunk-1", "index=main", "-6h", "now", 50)
	assertContains(t, capturedURI, "/api/v1/splunk/splunk-1/search")
	assertContains(t, capturedURI, "earliest=-6h")
	assertContains(t, capturedURI, "latest=now")
	assertContains(t, capturedURI, "limit=50")
}

func TestSplunkSearch_NoTimeRange(t *testing.T) {
	var capturedURI string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURI = r.RequestURI
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"result": nil, "component_id": "splunk-1"})
	}))
	defer ts.Close()

	c := New(ts.URL)
	_, _ = c.SplunkSearch(context.Background(), "splunk-1", "index=main", "", "", 10)
	if strings.Contains(capturedURI, "earliest") || strings.Contains(capturedURI, "latest") {
		t.Errorf("SplunkSearch(): unexpected time range params in %q", capturedURI)
	}
}

func TestSplunkSearch_Error(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "internal", "message": "splunk down"})
	}))
	defer ts.Close()

	c := New(ts.URL)
	_, err := c.SplunkSearch(context.Background(), "splunk-prod", "search *", "", "", 10)
	if err == nil {
		t.Fatal("SplunkSearch(): expected error for 500 response")
	}
}

// --- Dynatrace ---

func TestDynatraceMetricsQuery_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("DynatraceMetricsQuery: expected GET, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"result": map[string]any{
				"resolution": "1m",
				"series": []map[string]any{
					{
						"metric_id": "builtin:host.cpu.usage",
						"values":    []map[string]any{{"timestamp": int64(1000000), "value": 65.4}},
					},
				},
			},
			"component_id": "dt-prod",
		})
	}))
	defer ts.Close()

	c := New(ts.URL)
	result, err := c.DynatraceMetricsQuery(context.Background(), "dt-prod", "builtin:host.cpu.usage", 1000, 2000)
	if err != nil {
		t.Fatalf("DynatraceMetricsQuery() error: %v", err)
	}
	if result == nil {
		t.Fatal("DynatraceMetricsQuery(): got nil result")
	}
	if len(result.Series) != 1 {
		t.Errorf("DynatraceMetricsQuery(): got %d series, want 1", len(result.Series))
	}
}

func TestDynatraceMetricsQuery_URLConstruction(t *testing.T) {
	var capturedURI string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURI = r.RequestURI
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"result": nil, "component_id": "dt-1"})
	}))
	defer ts.Close()

	c := New(ts.URL)
	_, _ = c.DynatraceMetricsQuery(context.Background(), "dt-1", "builtin:cpu", 500, 1000)
	assertContains(t, capturedURI, "/api/v1/dynatrace/dt-1/metrics")
	assertContains(t, capturedURI, "from=500")
	assertContains(t, capturedURI, "to=1000")
}

func TestDynatraceMetricsQuery_Error(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "internal", "message": "dt unavailable"})
	}))
	defer ts.Close()

	c := New(ts.URL)
	_, err := c.DynatraceMetricsQuery(context.Background(), "dt-prod", "builtin:cpu", 0, 0)
	if err == nil {
		t.Fatal("DynatraceMetricsQuery(): expected error for 500 response")
	}
}

func TestDynatraceEvents_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("DynatraceEvents: expected GET, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"result": map[string]any{
				"events": []map[string]any{
					{
						"event_id": "evt-1",
						"type":     "AVAILABILITY",
						"title":    "Host unavailable",
						"severity": "ERROR",
					},
				},
				"count": 1,
			},
			"component_id": "dt-prod",
		})
	}))
	defer ts.Close()

	c := New(ts.URL)
	result, err := c.DynatraceEvents(context.Background(), "dt-prod", 1000, 2000, 50)
	if err != nil {
		t.Fatalf("DynatraceEvents() error: %v", err)
	}
	if result == nil {
		t.Fatal("DynatraceEvents(): got nil result")
	}
	if len(result.Events) != 1 {
		t.Errorf("DynatraceEvents(): got %d events, want 1", len(result.Events))
	}
}

func TestDynatraceEvents_URLConstruction(t *testing.T) {
	var capturedURI string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURI = r.RequestURI
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"result": nil, "component_id": "dt-1"})
	}))
	defer ts.Close()

	c := New(ts.URL)
	_, _ = c.DynatraceEvents(context.Background(), "dt-1", 500, 1000, 25)
	assertContains(t, capturedURI, "/api/v1/dynatrace/dt-1/events")
	assertContains(t, capturedURI, "from=500")
	assertContains(t, capturedURI, "to=1000")
	assertContains(t, capturedURI, "limit=25")
}

func TestDynatraceEvents_Error(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "internal", "message": "dt events error"})
	}))
	defer ts.Close()

	c := New(ts.URL)
	_, err := c.DynatraceEvents(context.Background(), "dt-prod", 0, 0, 0)
	if err == nil {
		t.Fatal("DynatraceEvents(): expected error for 500 response")
	}
}

// --- New Relic ---

func TestNewRelicNRQLQuery_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("NewRelicNRQLQuery: expected GET, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"result": map[string]any{
				"results": []map[string]any{
					{"count": 1234},
				},
				"metadata": map[string]any{
					"event_types": []string{"Transaction"},
					"time_window": map[string]any{"since": "1 hour ago", "until": "now"},
				},
			},
			"component_id": "nr-prod",
		})
	}))
	defer ts.Close()

	c := New(ts.URL)
	result, err := c.NewRelicNRQLQuery(context.Background(), "nr-prod", 12345, "SELECT count(*) FROM Transaction")
	if err != nil {
		t.Fatalf("NewRelicNRQLQuery() error: %v", err)
	}
	if result == nil {
		t.Fatal("NewRelicNRQLQuery(): got nil result")
	}
	if len(result.Results) != 1 {
		t.Errorf("NewRelicNRQLQuery(): got %d results, want 1", len(result.Results))
	}
}

func TestNewRelicNRQLQuery_URLConstruction(t *testing.T) {
	var capturedURI string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURI = r.RequestURI
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"result": nil, "component_id": "nr-1"})
	}))
	defer ts.Close()

	c := New(ts.URL)
	_, _ = c.NewRelicNRQLQuery(context.Background(), "nr-1", 99999, "SELECT * FROM Log")
	assertContains(t, capturedURI, "/api/v1/newrelic/nr-1/nrql")
	assertContains(t, capturedURI, "account_id=99999")
}

func TestNewRelicNRQLQuery_ZeroAccountID(t *testing.T) {
	var capturedURI string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURI = r.RequestURI
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"result": nil, "component_id": "nr-1"})
	}))
	defer ts.Close()

	c := New(ts.URL)
	_, _ = c.NewRelicNRQLQuery(context.Background(), "nr-1", 0, "SELECT count(*) FROM Transaction")
	assertContains(t, capturedURI, "account_id=0")
}

func TestNewRelicNRQLQuery_Error(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "internal", "message": "nr unavailable"})
	}))
	defer ts.Close()

	c := New(ts.URL)
	_, err := c.NewRelicNRQLQuery(context.Background(), "nr-prod", 12345, "SELECT count(*) FROM Transaction")
	if err == nil {
		t.Fatal("NewRelicNRQLQuery(): expected error for 500 response")
	}
}
