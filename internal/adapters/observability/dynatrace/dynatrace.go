package dynatrace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/store"
)

const (
	statusNotConnected = "Not connected to Dynatrace"
	statusConnectedFmt = "Connected to Dynatrace at %s"
)

var ErrNotConnected = errors.New("adapter not connected to Dynatrace")

// DataPoint is a single timestamped metric value.
type DataPoint struct {
	Timestamp int64   `json:"timestamp"` // Unix ms
	Value     float64 `json:"value"`
}

// MetricSeries is one time-series returned from a Dynatrace metrics query.
type MetricSeries struct {
	MetricID   string            `json:"metric_id"`
	Dimensions map[string]string `json:"dimensions,omitempty"`
	Values     []DataPoint       `json:"values"`
}

// MetricsResult holds the result of a Dynatrace metrics query.
type MetricsResult struct {
	Resolution string         `json:"resolution"`
	Series     []MetricSeries `json:"series"`
}

// DynatraceEvent is a single Dynatrace event.
type DynatraceEvent struct {
	EventID    string            `json:"event_id"`
	Type       string            `json:"type"`
	Title      string            `json:"title"`
	Severity   string            `json:"severity"`
	StartTime  int64             `json:"start_time"` // Unix ms
	EndTime    int64             `json:"end_time"`   // Unix ms
	EntityID   string            `json:"entity_id"`
	EntityName string            `json:"entity_name"`
	Properties map[string]string `json:"properties,omitempty"`
}

// EventsResult holds the result of a Dynatrace events query.
type EventsResult struct {
	Events []DynatraceEvent `json:"events"`
	Count  int              `json:"count"`
}

// DynatraceAdapter extends the base Adapter with Dynatrace-specific operations.
type DynatraceAdapter interface {
	adapters.Adapter
	// MetricsQuery executes a Dynatrace metrics selector query.
	// from and to are Unix timestamps in milliseconds.
	MetricsQuery(ctx context.Context, query string, from, to int64) (*MetricsResult, error)
	// Events returns Dynatrace events in the given time range.
	// from and to are Unix timestamps in milliseconds.
	Events(ctx context.Context, from, to int64, limit int) (*EventsResult, error)
}

// httpDoer abstracts net/http.Client for testing.
type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Adapter is the concrete Dynatrace adapter.
type Adapter struct {
	mu        sync.RWMutex
	config    Config
	client    httpDoer
	connected bool
}

// New creates a new Dynatrace adapter (not yet connected).
func New() *Adapter {
	return &Adapter{
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// NewWithClient creates an adapter with a custom HTTP client (for testing).
func NewWithClient(client httpDoer) *Adapter {
	return &Adapter{
		client:    client,
		connected: true,
	}
}

// Connect establishes and verifies connectivity to Dynatrace.
func (a *Adapter) Connect(ctx context.Context, source store.Source) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	var configMap map[string]any
	if len(source.Config) > 0 {
		if err := json.Unmarshal(source.Config, &configMap); err != nil {
			return fmt.Errorf("parse source config JSON: %w", err)
		}
	} else {
		configMap = make(map[string]any)
	}

	cfg, err := ParseConfig(configMap)
	if err != nil {
		return fmt.Errorf("parse source config: %w", err)
	}
	a.config = cfg

	// Verify connectivity via metrics metadata endpoint.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		cfg.URL+"/api/v2/metrics?pageSize=1", nil)
	if err != nil {
		return fmt.Errorf("build health-check request: %w", err)
	}
	a.addHeaders(req, cfg)

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("connect to Dynatrace at %s: %w", cfg.URL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("dynatrace health check failed (status %d): %s", resp.StatusCode, string(body))
	}

	a.connected = true
	return nil
}

// Disconnect closes the connection.
func (a *Adapter) Disconnect() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.connected = false
	return nil
}

// Status returns the current connection status.
func (a *Adapter) Status() adapters.Status {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.connected {
		return adapters.Status{
			Connected: true,
			Message:   fmt.Sprintf(statusConnectedFmt, a.config.URL),
		}
	}
	return adapters.Status{Connected: false, Message: statusNotConnected}
}

// MetricsQuery executes a Dynatrace metrics selector query.
// from and to are Unix timestamps in milliseconds.
func (a *Adapter) MetricsQuery(ctx context.Context, query string, from, to int64) (*MetricsResult, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	params := url.Values{}
	params.Set("metricSelector", query)
	if from > 0 {
		params.Set("from", fmt.Sprintf("%d", from))
	}
	if to > 0 {
		params.Set("to", fmt.Sprintf("%d", to))
	}

	u := a.config.URL + "/api/v2/metrics/query?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build metrics query request: %w", err)
	}
	a.addHeaders(req, a.config)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("metrics query request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read metrics response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("metrics query failed (status %d): %s", resp.StatusCode, string(body))
	}

	var raw struct {
		Resolution string `json:"resolution"`
		Result     []struct {
			MetricID string `json:"metricId"`
			Data     []struct {
				DimensionMap map[string]string `json:"dimensionMap"`
				Timestamps   []int64           `json:"timestamps"`
				Values       []float64         `json:"values"`
			} `json:"data"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse metrics response: %w", err)
	}

	result := &MetricsResult{Resolution: raw.Resolution}
	for _, r := range raw.Result {
		for _, d := range r.Data {
			series := MetricSeries{
				MetricID:   r.MetricID,
				Dimensions: d.DimensionMap,
			}
			for i, ts := range d.Timestamps {
				val := float64(0)
				if i < len(d.Values) {
					val = d.Values[i]
				}
				series.Values = append(series.Values, DataPoint{
					Timestamp: ts,
					Value:     val,
				})
			}
			result.Series = append(result.Series, series)
		}
	}
	return result, nil
}

// Events returns Dynatrace events in the given time range.
// from and to are Unix timestamps in milliseconds.
func (a *Adapter) Events(ctx context.Context, from, to int64, limit int) (*EventsResult, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	if limit <= 0 {
		limit = 50
	}

	params := url.Values{}
	params.Set("pageSize", fmt.Sprintf("%d", limit))
	if from > 0 {
		params.Set("from", fmt.Sprintf("%d", from))
	}
	if to > 0 {
		params.Set("to", fmt.Sprintf("%d", to))
	}

	u := a.config.URL + "/api/v2/events?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build events request: %w", err)
	}
	a.addHeaders(req, a.config)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("events request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read events response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("events query failed (status %d): %s", resp.StatusCode, string(body))
	}

	var raw struct {
		TotalCount int `json:"totalCount"`
		Events     []struct {
			EventID   string `json:"eventId"`
			EventType string `json:"eventType"`
			Title     string `json:"title"`
			Severity  string `json:"severity"`
			StartTime int64  `json:"startTime"`
			EndTime   int64  `json:"endTime"`
			EntityID  struct {
				EntityID struct {
					ID string `json:"id"`
				} `json:"entityId"`
				Name string `json:"name"`
			} `json:"entityId"`
			Properties []struct {
				Key   string `json:"key"`
				Value string `json:"value"`
			} `json:"properties"`
		} `json:"events"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse events response: %w", err)
	}

	result := &EventsResult{}
	for _, e := range raw.Events {
		evt := DynatraceEvent{
			EventID:    e.EventID,
			Type:       e.EventType,
			Title:      e.Title,
			Severity:   e.Severity,
			StartTime:  e.StartTime,
			EndTime:    e.EndTime,
			EntityID:   e.EntityID.EntityID.ID,
			EntityName: e.EntityID.Name,
		}
		if len(e.Properties) > 0 {
			evt.Properties = make(map[string]string)
			for _, p := range e.Properties {
				evt.Properties[p.Key] = p.Value
			}
		}
		result.Events = append(result.Events, evt)
	}
	result.Count = len(result.Events)
	return result, nil
}

// addHeaders sets Dynatrace authentication headers.
func (a *Adapter) addHeaders(req *http.Request, cfg Config) {
	req.Header.Set("Authorization", "Api-Token "+cfg.Token)
	req.Header.Set("Accept", "application/json")
}

func (a *Adapter) checkConnected() error {
	if !a.connected {
		return ErrNotConnected
	}
	return nil
}
