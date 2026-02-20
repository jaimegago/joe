package coreagent

import (
	"context"
	"fmt"
	"time"

	datadogadapter "github.com/jaimegago/joe/internal/adapters/observability/datadog"
	dynatraceadapter "github.com/jaimegago/joe/internal/adapters/observability/dynatrace"
	jaegeradapter "github.com/jaimegago/joe/internal/adapters/observability/jaeger"
	lokiadapter "github.com/jaimegago/joe/internal/adapters/observability/loki"
	newrelicadapter "github.com/jaimegago/joe/internal/adapters/observability/newrelic"
	prometheusadapter "github.com/jaimegago/joe/internal/adapters/observability/prometheus"
	splunkadapter "github.com/jaimegago/joe/internal/adapters/observability/splunk"
	tempoadapter "github.com/jaimegago/joe/internal/adapters/observability/tempo"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/store"
)

// refreshPrometheusSource refreshes a Prometheus/Mimir source.
// It creates a graph node for the source and attempts to discover metrics_in
// edges by matching Prometheus scrape targets to existing service nodes.
func (r *Refresher) refreshPrometheusSource(ctx context.Context, source *store.Source, adapter prometheusadapter.PrometheusAdapter) error {
	r.logger.Info("refreshing prometheus source", "source_id", source.ID, "type", source.Type)

	now := time.Now()
	nodeID := obsNodeID(source.ID, source.Type)

	desiredNodes := []graph.Node{
		{
			ID:       nodeID,
			Type:     source.Type + "_source",
			SourceID: source.ID,
			Metadata: map[string]any{
				"source_id":   source.ID,
				"source_type": source.Type,
				"name":        source.Name,
			},
			LastSeen: now,
		},
	}

	desiredEdges := make([]graph.Edge, 0)

	// Discover targets and create metrics_in edges to matching service nodes.
	targets, err := adapter.Targets(ctx)
	if err != nil {
		r.logger.Warn("failed to list prometheus targets (skipping edge discovery)", "source_id", source.ID, "error", err)
	} else {
		edges, edgeErr := r.buildMetricsInEdges(ctx, source, nodeID, targets, now)
		if edgeErr != nil {
			r.logger.Warn("failed to build metrics_in edges", "source_id", source.ID, "error", edgeErr)
		} else {
			desiredEdges = append(desiredEdges, edges...)
		}
	}

	existingNodes, existingEdges, err := LoadGraphStateForSource(ctx, r.services.Graph, source.ID)
	if err != nil {
		return fmt.Errorf("load graph state for prometheus source %s: %w", source.ID, err)
	}

	delta := BuildGraphDelta(existingNodes, existingEdges, desiredNodes, desiredEdges)
	if err := ApplyGraphDelta(ctx, r.services.Graph, delta); err != nil {
		return fmt.Errorf("apply graph delta for prometheus source %s: %w", source.ID, err)
	}

	r.logger.Info("prometheus refresh completed",
		"source_id", source.ID,
		"nodes", len(desiredNodes),
		"edges", len(desiredEdges),
	)
	return nil
}

// buildMetricsInEdges attempts to match Prometheus targets to existing service
// nodes using the "job" label, creating metrics_in edges.
func (r *Refresher) buildMetricsInEdges(ctx context.Context, source *store.Source, promNodeID string, targets []prometheusadapter.Target, now time.Time) ([]graph.Edge, error) {
	var edges []graph.Edge

	for _, target := range targets {
		if target.State != "active" {
			continue
		}

		jobName := target.Labels["job"]
		if jobName == "" {
			continue
		}

		// Query the graph for service nodes whose name matches the job label.
		matchingNodes, err := r.services.Graph.Query(ctx, jobName)
		if err != nil {
			r.logger.Debug("graph query failed for target job", "job", jobName, "error", err)
			continue
		}

		for _, svcNode := range matchingNodes {
			if svcNode.Type != "service" && svcNode.Type != "deployment" {
				continue
			}
			edges = append(edges, graph.Edge{
				From:       svcNode.ID,
				To:         promNodeID,
				Relation:   graph.RelationMetricsIn,
				Confidence: graph.Inferred,
				Source:     "prometheus_targets",
				SourceID:   source.ID,
				Context:    "job=" + jobName,
				CreatedAt:  now,
			})
		}
	}

	return edges, nil
}

// refreshLokiSource refreshes a Loki source.
// Creates a graph node for the source. Edge discovery is done via .joe/ files.
func (r *Refresher) refreshLokiSource(ctx context.Context, source *store.Source, _ lokiadapter.LokiAdapter) error {
	r.logger.Info("refreshing loki source", "source_id", source.ID)

	now := time.Now()
	nodeID := obsNodeID(source.ID, source.Type)

	desiredNodes := []graph.Node{
		{
			ID:       nodeID,
			Type:     "loki_source",
			SourceID: source.ID,
			Metadata: map[string]any{
				"source_id":   source.ID,
				"source_type": source.Type,
				"name":        source.Name,
			},
			LastSeen: now,
		},
	}

	existingNodes, existingEdges, err := LoadGraphStateForSource(ctx, r.services.Graph, source.ID)
	if err != nil {
		return fmt.Errorf("load graph state for loki source %s: %w", source.ID, err)
	}

	delta := BuildGraphDelta(existingNodes, existingEdges, desiredNodes, nil)
	if err := ApplyGraphDelta(ctx, r.services.Graph, delta); err != nil {
		return fmt.Errorf("apply graph delta for loki source %s: %w", source.ID, err)
	}

	_ = existingEdges // suppress unused warning; edges preserved by delta logic
	r.logger.Info("loki refresh completed", "source_id", source.ID)
	return nil
}

// refreshTempoSource refreshes a Tempo source.
func (r *Refresher) refreshTempoSource(ctx context.Context, source *store.Source, _ tempoadapter.TempoAdapter) error {
	r.logger.Info("refreshing tempo source", "source_id", source.ID)

	now := time.Now()
	nodeID := obsNodeID(source.ID, source.Type)

	desiredNodes := []graph.Node{
		{
			ID:       nodeID,
			Type:     "tempo_source",
			SourceID: source.ID,
			Metadata: map[string]any{
				"source_id":   source.ID,
				"source_type": source.Type,
				"name":        source.Name,
			},
			LastSeen: now,
		},
	}

	existingNodes, existingEdges, err := LoadGraphStateForSource(ctx, r.services.Graph, source.ID)
	if err != nil {
		return fmt.Errorf("load graph state for tempo source %s: %w", source.ID, err)
	}

	delta := BuildGraphDelta(existingNodes, existingEdges, desiredNodes, nil)
	if err := ApplyGraphDelta(ctx, r.services.Graph, delta); err != nil {
		return fmt.Errorf("apply graph delta for tempo source %s: %w", source.ID, err)
	}

	_ = existingEdges
	r.logger.Info("tempo refresh completed", "source_id", source.ID)
	return nil
}

// refreshJaegerSource refreshes a Jaeger source and creates traces_in edges
// for any services Jaeger has seen.
func (r *Refresher) refreshJaegerSource(ctx context.Context, source *store.Source, adapter jaegeradapter.JaegerAdapter) error {
	r.logger.Info("refreshing jaeger source", "source_id", source.ID)

	now := time.Now()
	nodeID := obsNodeID(source.ID, source.Type)

	desiredNodes := []graph.Node{
		{
			ID:       nodeID,
			Type:     "jaeger_source",
			SourceID: source.ID,
			Metadata: map[string]any{
				"source_id":   source.ID,
				"source_type": source.Type,
				"name":        source.Name,
			},
			LastSeen: now,
		},
	}

	desiredEdges := make([]graph.Edge, 0)

	// Discover services and create traces_in edges.
	services, err := adapter.ListServices(ctx)
	if err != nil {
		r.logger.Warn("failed to list jaeger services (skipping edge discovery)", "source_id", source.ID, "error", err)
	} else {
		for _, svcName := range services {
			matchingNodes, err := r.services.Graph.Query(ctx, svcName)
			if err != nil {
				continue
			}
			for _, svcNode := range matchingNodes {
				if svcNode.Type != "service" && svcNode.Type != "deployment" {
					continue
				}
				desiredEdges = append(desiredEdges, graph.Edge{
					From:       svcNode.ID,
					To:         nodeID,
					Relation:   graph.RelationTracesIn,
					Confidence: graph.Inferred,
					Source:     "jaeger_services",
					SourceID:   source.ID,
					Context:    "service=" + svcName,
					CreatedAt:  now,
				})
			}
		}
	}

	existingNodes, existingEdges, err := LoadGraphStateForSource(ctx, r.services.Graph, source.ID)
	if err != nil {
		return fmt.Errorf("load graph state for jaeger source %s: %w", source.ID, err)
	}

	delta := BuildGraphDelta(existingNodes, existingEdges, desiredNodes, desiredEdges)
	if err := ApplyGraphDelta(ctx, r.services.Graph, delta); err != nil {
		return fmt.Errorf("apply graph delta for jaeger source %s: %w", source.ID, err)
	}

	r.logger.Info("jaeger refresh completed",
		"source_id", source.ID,
		"nodes", len(desiredNodes),
		"edges", len(desiredEdges),
	)
	return nil
}

// obsNodeID builds a stable graph node ID for an observability source.
func obsNodeID(sourceID, sourceType string) string {
	return fmt.Sprintf("obs/%s/%s", sourceType, sourceID)
}

// refreshDatadogSource refreshes a Datadog source and discovers metrics_in and logs_in
// edges by querying the Datadog API for active hosts and recent log events.
func (r *Refresher) refreshDatadogSource(ctx context.Context, source *store.Source, adapter datadogadapter.DatadogAdapter) error {
	r.logger.Info("refreshing datadog source", "source_id", source.ID)

	now := time.Now()
	nodeID := obsNodeID(source.ID, source.Type)

	desiredNodes := []graph.Node{
		{
			ID:       nodeID,
			Type:     "datadog_source",
			SourceID: source.ID,
			Metadata: map[string]any{
				"source_id":   source.ID,
				"source_type": source.Type,
				"name":        source.Name,
			},
			LastSeen: now,
		},
	}

	desiredEdges := make([]graph.Edge, 0)

	// Discover metrics_in edges from active Datadog hosts.
	metricServices, err := adapter.ListActiveServices(ctx)
	if err != nil {
		r.logger.Warn("failed to list datadog metric services (skipping metrics_in discovery)", "source_id", source.ID, "error", err)
	} else {
		edges, edgeErr := r.buildDDMetricsInEdges(ctx, source, nodeID, metricServices, now)
		if edgeErr != nil {
			r.logger.Warn("failed to build datadog metrics_in edges", "source_id", source.ID, "error", edgeErr)
		} else {
			desiredEdges = append(desiredEdges, edges...)
		}
	}

	// Discover logs_in edges from recent Datadog log events.
	logServices, err := adapter.ListLogServices(ctx)
	if err != nil {
		r.logger.Warn("failed to list datadog log services (skipping logs_in discovery)", "source_id", source.ID, "error", err)
	} else {
		edges, edgeErr := r.buildDDLogsInEdges(ctx, source, nodeID, logServices, now)
		if edgeErr != nil {
			r.logger.Warn("failed to build datadog logs_in edges", "source_id", source.ID, "error", edgeErr)
		} else {
			desiredEdges = append(desiredEdges, edges...)
		}
	}

	existingNodes, existingEdges, err := LoadGraphStateForSource(ctx, r.services.Graph, source.ID)
	if err != nil {
		return fmt.Errorf("load graph state for datadog source %s: %w", source.ID, err)
	}

	delta := BuildGraphDelta(existingNodes, existingEdges, desiredNodes, desiredEdges)
	if err := ApplyGraphDelta(ctx, r.services.Graph, delta); err != nil {
		return fmt.Errorf("apply graph delta for datadog source %s: %w", source.ID, err)
	}

	r.logger.Info("datadog refresh completed",
		"source_id", source.ID,
		"nodes", len(desiredNodes),
		"edges", len(desiredEdges),
	)
	return nil
}

// buildDDMetricsInEdges creates metrics_in edges by matching Datadog metric service names
// (from active hosts) to existing service/deployment nodes in the graph.
func (r *Refresher) buildDDMetricsInEdges(ctx context.Context, source *store.Source, ddNodeID string, services []string, now time.Time) ([]graph.Edge, error) {
	var edges []graph.Edge
	for _, svcName := range services {
		matchingNodes, err := r.services.Graph.Query(ctx, svcName)
		if err != nil {
			r.logger.Debug("graph query failed for datadog metric service", "service", svcName, "error", err)
			continue
		}
		for _, svcNode := range matchingNodes {
			if svcNode.Type != "service" && svcNode.Type != "deployment" {
				continue
			}
			edges = append(edges, graph.Edge{
				From:       svcNode.ID,
				To:         ddNodeID,
				Relation:   graph.RelationMetricsIn,
				Confidence: graph.Inferred,
				Source:     "datadog_hosts",
				SourceID:   source.ID,
				Context:    "service=" + svcName,
				CreatedAt:  now,
			})
		}
	}
	return edges, nil
}

// buildDDLogsInEdges creates logs_in edges by matching Datadog log service names
// (from recent log events) to existing service/deployment nodes in the graph.
func (r *Refresher) buildDDLogsInEdges(ctx context.Context, source *store.Source, ddNodeID string, services []string, now time.Time) ([]graph.Edge, error) {
	var edges []graph.Edge
	for _, svcName := range services {
		matchingNodes, err := r.services.Graph.Query(ctx, svcName)
		if err != nil {
			r.logger.Debug("graph query failed for datadog log service", "service", svcName, "error", err)
			continue
		}
		for _, svcNode := range matchingNodes {
			if svcNode.Type != "service" && svcNode.Type != "deployment" {
				continue
			}
			edges = append(edges, graph.Edge{
				From:       svcNode.ID,
				To:         ddNodeID,
				Relation:   graph.RelationLogsIn,
				Confidence: graph.Inferred,
				Source:     "datadog_logs",
				SourceID:   source.ID,
				Context:    "service=" + svcName,
				CreatedAt:  now,
			})
		}
	}
	return edges, nil
}

// refreshSplunkSource creates a graph node for a Splunk source.
func (r *Refresher) refreshSplunkSource(ctx context.Context, source *store.Source, _ splunkadapter.SplunkAdapter) error {
	r.logger.Info("refreshing splunk source", "source_id", source.ID)

	now := time.Now()
	nodeID := obsNodeID(source.ID, source.Type)

	desiredNodes := []graph.Node{
		{
			ID:       nodeID,
			Type:     "splunk_source",
			SourceID: source.ID,
			Metadata: map[string]any{
				"source_id":   source.ID,
				"source_type": source.Type,
				"name":        source.Name,
			},
			LastSeen: now,
		},
	}

	existingNodes, existingEdges, err := LoadGraphStateForSource(ctx, r.services.Graph, source.ID)
	if err != nil {
		return fmt.Errorf("load graph state for splunk source %s: %w", source.ID, err)
	}

	delta := BuildGraphDelta(existingNodes, existingEdges, desiredNodes, nil)
	if err := ApplyGraphDelta(ctx, r.services.Graph, delta); err != nil {
		return fmt.Errorf("apply graph delta for splunk source %s: %w", source.ID, err)
	}

	_ = existingEdges
	r.logger.Info("splunk refresh completed", "source_id", source.ID)
	return nil
}

// refreshDynatraceSource creates a graph node for a Dynatrace source.
func (r *Refresher) refreshDynatraceSource(ctx context.Context, source *store.Source, _ dynatraceadapter.DynatraceAdapter) error {
	r.logger.Info("refreshing dynatrace source", "source_id", source.ID)

	now := time.Now()
	nodeID := obsNodeID(source.ID, source.Type)

	desiredNodes := []graph.Node{
		{
			ID:       nodeID,
			Type:     "dynatrace_source",
			SourceID: source.ID,
			Metadata: map[string]any{
				"source_id":   source.ID,
				"source_type": source.Type,
				"name":        source.Name,
			},
			LastSeen: now,
		},
	}

	existingNodes, existingEdges, err := LoadGraphStateForSource(ctx, r.services.Graph, source.ID)
	if err != nil {
		return fmt.Errorf("load graph state for dynatrace source %s: %w", source.ID, err)
	}

	delta := BuildGraphDelta(existingNodes, existingEdges, desiredNodes, nil)
	if err := ApplyGraphDelta(ctx, r.services.Graph, delta); err != nil {
		return fmt.Errorf("apply graph delta for dynatrace source %s: %w", source.ID, err)
	}

	_ = existingEdges
	r.logger.Info("dynatrace refresh completed", "source_id", source.ID)
	return nil
}

// refreshNewRelicSource creates a graph node for a New Relic source.
func (r *Refresher) refreshNewRelicSource(ctx context.Context, source *store.Source, _ newrelicadapter.NewRelicAdapter) error {
	r.logger.Info("refreshing newrelic source", "source_id", source.ID)

	now := time.Now()
	nodeID := obsNodeID(source.ID, source.Type)

	desiredNodes := []graph.Node{
		{
			ID:       nodeID,
			Type:     "newrelic_source",
			SourceID: source.ID,
			Metadata: map[string]any{
				"source_id":   source.ID,
				"source_type": source.Type,
				"name":        source.Name,
			},
			LastSeen: now,
		},
	}

	existingNodes, existingEdges, err := LoadGraphStateForSource(ctx, r.services.Graph, source.ID)
	if err != nil {
		return fmt.Errorf("load graph state for newrelic source %s: %w", source.ID, err)
	}

	delta := BuildGraphDelta(existingNodes, existingEdges, desiredNodes, nil)
	if err := ApplyGraphDelta(ctx, r.services.Graph, delta); err != nil {
		return fmt.Errorf("apply graph delta for newrelic source %s: %w", source.ID, err)
	}

	_ = existingEdges
	r.logger.Info("newrelic refresh completed", "source_id", source.ID)
	return nil
}
