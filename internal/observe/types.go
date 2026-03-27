package observe

import "time"

// ObservabilityResult is the normalized response for metrics, logs, and traces queries.
type ObservabilityResult struct {
	Source      string      `json:"source"`
	SourceID    string      `json:"source_id"`
	NativeQuery string      `json:"native_query"`
	Data        []DataPoint `json:"data"`
	RawResult   any         `json:"raw_result,omitempty"`
}

// DataPoint represents a single time-series data point.
type DataPoint struct {
	Timestamp time.Time         `json:"timestamp"`
	Labels    map[string]string `json:"labels"`
	Value     float64           `json:"value"`
}

// AlertsResult is the normalized response for alerts queries.
type AlertsResult struct {
	Source   string  `json:"source"`
	SourceID string  `json:"source_id"`
	Alerts   []Alert `json:"alerts"`
	Count    int     `json:"count"`
}

// Alert represents a normalized alert regardless of the backing system.
type Alert struct {
	Name    string            `json:"name"`
	State   string            `json:"state"`
	Labels  map[string]string `json:"labels,omitempty"`
	Summary string            `json:"summary,omitempty"`
}

// K8sResult is the normalized response for k8s queries.
type K8sResult struct {
	Source      string `json:"source"`
	SourceID    string `json:"source_id"`
	NativeQuery string `json:"native_query"`
	Data        any    `json:"data"`
}
