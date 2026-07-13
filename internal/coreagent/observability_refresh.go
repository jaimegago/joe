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

// refreshPrometheusComponent refreshes a Prometheus/Mimir source.
// It creates a graph node for the source and attempts to discover metrics_in
// edges by matching Prometheus scrape targets to existing service nodes.
func (r *Refresher) refreshPrometheusComponent(ctx context.Context, source *store.Component, adapter prometheusadapter.PrometheusAdapter) error {
	r.logger.Info("refreshing prometheus component", "component_id", source.ID, "type", source.Type)

	now := time.Now()
	nodeID := obsNodeID(source.ID, source.Type)

	desiredNodes := []graph.Node{
		{
			ID:          nodeID,
			Type:        source.Type + "_component",
			ComponentID: source.ID,
			Metadata: map[string]any{
				"component_id":   source.ID,
				"component_type": source.Type,
				"name":           source.Name,
			},
			LastSeen: now,
		},
	}

	desiredEdges := make([]graph.Edge, 0)

	// Discover targets and create metrics_in edges to matching service nodes.
	targets, err := adapter.Targets(ctx)
	if err != nil {
		r.logger.Warn("failed to list prometheus targets (skipping edge discovery)", "component_id", source.ID, "error", err)
	} else {
		edges, edgeErr := r.buildMetricsInEdges(ctx, source, nodeID, targets, now)
		if edgeErr != nil {
			r.logger.Warn("failed to build metrics_in edges", "component_id", source.ID, "error", edgeErr)
		} else {
			desiredEdges = append(desiredEdges, edges...)
		}
	}

	existingNodes, existingEdges, err := LoadGraphStateForComponent(ctx, r.services.Graph, source.ID)
	if err != nil {
		return fmt.Errorf("load graph state for prometheus component %s: %w", source.ID, err)
	}

	delta := BuildGraphDelta(existingNodes, existingEdges, desiredNodes, desiredEdges)
	if err := ApplyGraphDelta(ctx, r.services.Graph, delta); err != nil {
		return fmt.Errorf("apply graph delta for prometheus component %s: %w", source.ID, err)
	}

	r.logger.Info("prometheus refresh completed",
		"component_id", source.ID,
		"nodes", len(desiredNodes),
		"edges", len(desiredEdges),
	)
	return nil
}

// buildMetricsInEdges attempts to match Prometheus targets to existing service
// nodes using the "job" label, creating metrics_in edges.
func (r *Refresher) buildMetricsInEdges(ctx context.Context, source *store.Component, promNodeID string, targets []prometheusadapter.Target, now time.Time) ([]graph.Edge, error) {
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
				From:        svcNode.ID,
				To:          promNodeID,
				Relation:    graph.RelationMetricsIn,
				Confidence:  graph.Inferred,
				Source:      "prometheus_targets",
				ComponentID: source.ID,
				Context:     "job=" + jobName,
				CreatedAt:   now,
			})
		}
	}

	return edges, nil
}

// refreshLokiComponent refreshes a Loki source.
// Discovers services sending logs to Loki via the label values API and emits logs_in edges.
func (r *Refresher) refreshLokiComponent(ctx context.Context, source *store.Component, adapter lokiadapter.LokiAdapter) error {
	r.logger.Info("refreshing loki component", "component_id", source.ID)

	now := time.Now()
	nodeID := obsNodeID(source.ID, source.Type)

	desiredNodes := []graph.Node{
		{
			ID:          nodeID,
			Type:        "loki_component",
			ComponentID: source.ID,
			Metadata: map[string]any{
				"component_id":   source.ID,
				"component_type": source.Type,
				"name":           source.Name,
			},
			LastSeen: now,
		},
	}

	desiredEdges := make([]graph.Edge, 0)

	// Discover services shipping logs to Loki and create logs_in edges.
	services, err := adapter.ListServices(ctx)
	if err != nil {
		r.logger.Warn("failed to list loki services (skipping edge discovery)", "component_id", source.ID, "error", err)
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
					From:        svcNode.ID,
					To:          nodeID,
					Relation:    graph.RelationLogsIn,
					Confidence:  graph.Inferred,
					Source:      "loki_labels",
					ComponentID: source.ID,
					Context:     "app=" + svcName,
					CreatedAt:   now,
				})
			}
		}
	}

	existingNodes, existingEdges, err := LoadGraphStateForComponent(ctx, r.services.Graph, source.ID)
	if err != nil {
		return fmt.Errorf("load graph state for loki component %s: %w", source.ID, err)
	}

	delta := BuildGraphDelta(existingNodes, existingEdges, desiredNodes, desiredEdges)
	if err := ApplyGraphDelta(ctx, r.services.Graph, delta); err != nil {
		return fmt.Errorf("apply graph delta for loki component %s: %w", source.ID, err)
	}

	r.logger.Info("loki refresh completed",
		"component_id", source.ID,
		"nodes", len(desiredNodes),
		"edges", len(desiredEdges),
	)
	return nil
}

// refreshTempoComponent refreshes a Tempo source.
// Discovers services sending traces to Tempo via the tag values API and emits traces_in edges.
func (r *Refresher) refreshTempoComponent(ctx context.Context, source *store.Component, adapter tempoadapter.TempoAdapter) error {
	r.logger.Info("refreshing tempo component", "component_id", source.ID)

	now := time.Now()
	nodeID := obsNodeID(source.ID, source.Type)

	desiredNodes := []graph.Node{
		{
			ID:          nodeID,
			Type:        "tempo_component",
			ComponentID: source.ID,
			Metadata: map[string]any{
				"component_id":   source.ID,
				"component_type": source.Type,
				"name":           source.Name,
			},
			LastSeen: now,
		},
	}

	desiredEdges := make([]graph.Edge, 0)

	// Discover services sending traces to Tempo and create traces_in edges.
	services, err := adapter.ListServices(ctx)
	if err != nil {
		r.logger.Warn("failed to list tempo services (skipping edge discovery)", "component_id", source.ID, "error", err)
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
					From:        svcNode.ID,
					To:          nodeID,
					Relation:    graph.RelationTracesIn,
					Confidence:  graph.Inferred,
					Source:      "tempo_tags",
					ComponentID: source.ID,
					Context:     "service.name=" + svcName,
					CreatedAt:   now,
				})
			}
		}
	}

	existingNodes, existingEdges, err := LoadGraphStateForComponent(ctx, r.services.Graph, source.ID)
	if err != nil {
		return fmt.Errorf("load graph state for tempo component %s: %w", source.ID, err)
	}

	delta := BuildGraphDelta(existingNodes, existingEdges, desiredNodes, desiredEdges)
	if err := ApplyGraphDelta(ctx, r.services.Graph, delta); err != nil {
		return fmt.Errorf("apply graph delta for tempo component %s: %w", source.ID, err)
	}

	r.logger.Info("tempo refresh completed",
		"component_id", source.ID,
		"nodes", len(desiredNodes),
		"edges", len(desiredEdges),
	)
	return nil
}

// refreshJaegerComponent refreshes a Jaeger source and creates traces_in edges
// for any services Jaeger has seen.
func (r *Refresher) refreshJaegerComponent(ctx context.Context, source *store.Component, adapter jaegeradapter.JaegerAdapter) error {
	r.logger.Info("refreshing jaeger component", "component_id", source.ID)

	now := time.Now()
	nodeID := obsNodeID(source.ID, source.Type)

	desiredNodes := []graph.Node{
		{
			ID:          nodeID,
			Type:        "jaeger_component",
			ComponentID: source.ID,
			Metadata: map[string]any{
				"component_id":   source.ID,
				"component_type": source.Type,
				"name":           source.Name,
			},
			LastSeen: now,
		},
	}

	desiredEdges := make([]graph.Edge, 0)

	// Discover services and create traces_in edges.
	services, err := adapter.ListServices(ctx)
	if err != nil {
		r.logger.Warn("failed to list jaeger services (skipping edge discovery)", "component_id", source.ID, "error", err)
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
					From:        svcNode.ID,
					To:          nodeID,
					Relation:    graph.RelationTracesIn,
					Confidence:  graph.Inferred,
					Source:      "jaeger_services",
					ComponentID: source.ID,
					Context:     "service=" + svcName,
					CreatedAt:   now,
				})
			}
		}
	}

	existingNodes, existingEdges, err := LoadGraphStateForComponent(ctx, r.services.Graph, source.ID)
	if err != nil {
		return fmt.Errorf("load graph state for jaeger component %s: %w", source.ID, err)
	}

	delta := BuildGraphDelta(existingNodes, existingEdges, desiredNodes, desiredEdges)
	if err := ApplyGraphDelta(ctx, r.services.Graph, delta); err != nil {
		return fmt.Errorf("apply graph delta for jaeger component %s: %w", source.ID, err)
	}

	r.logger.Info("jaeger refresh completed",
		"component_id", source.ID,
		"nodes", len(desiredNodes),
		"edges", len(desiredEdges),
	)
	return nil
}

// obsNodeID builds a stable graph node ID for an observability source.
func obsNodeID(sourceID, sourceType string) string {
	return fmt.Sprintf("obs/%s/%s", sourceType, sourceID)
}

// refreshDatadogComponent refreshes a Datadog source and discovers metrics_in and logs_in
// edges by querying the Datadog API for active hosts and recent log events.
func (r *Refresher) refreshDatadogComponent(ctx context.Context, source *store.Component, adapter datadogadapter.DatadogAdapter) error {
	r.logger.Info("refreshing datadog component", "component_id", source.ID)

	now := time.Now()
	nodeID := obsNodeID(source.ID, source.Type)

	desiredNodes := []graph.Node{
		{
			ID:          nodeID,
			Type:        "datadog_component",
			ComponentID: source.ID,
			Metadata: map[string]any{
				"component_id":   source.ID,
				"component_type": source.Type,
				"name":           source.Name,
			},
			LastSeen: now,
		},
	}

	desiredEdges := make([]graph.Edge, 0)

	// Discover metrics_in edges from active Datadog hosts.
	metricServices, err := adapter.ListActiveServices(ctx)
	if err != nil {
		r.logger.Warn("failed to list datadog metric services (skipping metrics_in discovery)", "component_id", source.ID, "error", err)
	} else {
		edges, edgeErr := r.buildDDMetricsInEdges(ctx, source, nodeID, metricServices, now)
		if edgeErr != nil {
			r.logger.Warn("failed to build datadog metrics_in edges", "component_id", source.ID, "error", edgeErr)
		} else {
			desiredEdges = append(desiredEdges, edges...)
		}
	}

	// Discover logs_in edges from recent Datadog log events.
	logServices, err := adapter.ListLogServices(ctx)
	if err != nil {
		r.logger.Warn("failed to list datadog log services (skipping logs_in discovery)", "component_id", source.ID, "error", err)
	} else {
		edges, edgeErr := r.buildDDLogsInEdges(ctx, source, nodeID, logServices, now)
		if edgeErr != nil {
			r.logger.Warn("failed to build datadog logs_in edges", "component_id", source.ID, "error", edgeErr)
		} else {
			desiredEdges = append(desiredEdges, edges...)
		}
	}

	existingNodes, existingEdges, err := LoadGraphStateForComponent(ctx, r.services.Graph, source.ID)
	if err != nil {
		return fmt.Errorf("load graph state for datadog component %s: %w", source.ID, err)
	}

	delta := BuildGraphDelta(existingNodes, existingEdges, desiredNodes, desiredEdges)
	if err := ApplyGraphDelta(ctx, r.services.Graph, delta); err != nil {
		return fmt.Errorf("apply graph delta for datadog component %s: %w", source.ID, err)
	}

	r.logger.Info("datadog refresh completed",
		"component_id", source.ID,
		"nodes", len(desiredNodes),
		"edges", len(desiredEdges),
	)
	return nil
}

// buildDDMetricsInEdges creates metrics_in edges by matching Datadog metric service names
// (from active hosts) to existing service/deployment nodes in the graph.
func (r *Refresher) buildDDMetricsInEdges(ctx context.Context, source *store.Component, ddNodeID string, services []string, now time.Time) ([]graph.Edge, error) {
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
				From:        svcNode.ID,
				To:          ddNodeID,
				Relation:    graph.RelationMetricsIn,
				Confidence:  graph.Inferred,
				Source:      "datadog_hosts",
				ComponentID: source.ID,
				Context:     "service=" + svcName,
				CreatedAt:   now,
			})
		}
	}
	return edges, nil
}

// buildDDLogsInEdges creates logs_in edges by matching Datadog log service names
// (from recent log events) to existing service/deployment nodes in the graph.
func (r *Refresher) buildDDLogsInEdges(ctx context.Context, source *store.Component, ddNodeID string, services []string, now time.Time) ([]graph.Edge, error) {
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
				From:        svcNode.ID,
				To:          ddNodeID,
				Relation:    graph.RelationLogsIn,
				Confidence:  graph.Inferred,
				Source:      "datadog_logs",
				ComponentID: source.ID,
				Context:     "service=" + svcName,
				CreatedAt:   now,
			})
		}
	}
	return edges, nil
}

// refreshSplunkComponent creates a graph node for a Splunk source.
func (r *Refresher) refreshSplunkComponent(ctx context.Context, source *store.Component, _ splunkadapter.SplunkAdapter) error {
	r.logger.Info("refreshing splunk component", "component_id", source.ID)

	now := time.Now()
	nodeID := obsNodeID(source.ID, source.Type)

	desiredNodes := []graph.Node{
		{
			ID:          nodeID,
			Type:        "splunk_component",
			ComponentID: source.ID,
			Metadata: map[string]any{
				"component_id":   source.ID,
				"component_type": source.Type,
				"name":           source.Name,
			},
			LastSeen: now,
		},
	}

	existingNodes, existingEdges, err := LoadGraphStateForComponent(ctx, r.services.Graph, source.ID)
	if err != nil {
		return fmt.Errorf("load graph state for splunk component %s: %w", source.ID, err)
	}

	delta := BuildGraphDelta(existingNodes, existingEdges, desiredNodes, nil)
	if err := ApplyGraphDelta(ctx, r.services.Graph, delta); err != nil {
		return fmt.Errorf("apply graph delta for splunk component %s: %w", source.ID, err)
	}

	_ = existingEdges
	r.logger.Info("splunk refresh completed", "component_id", source.ID)
	return nil
}

// refreshDynatraceComponent creates a graph node for a Dynatrace source.
func (r *Refresher) refreshDynatraceComponent(ctx context.Context, source *store.Component, _ dynatraceadapter.DynatraceAdapter) error {
	r.logger.Info("refreshing dynatrace component", "component_id", source.ID)

	now := time.Now()
	nodeID := obsNodeID(source.ID, source.Type)

	desiredNodes := []graph.Node{
		{
			ID:          nodeID,
			Type:        "dynatrace_component",
			ComponentID: source.ID,
			Metadata: map[string]any{
				"component_id":   source.ID,
				"component_type": source.Type,
				"name":           source.Name,
			},
			LastSeen: now,
		},
	}

	existingNodes, existingEdges, err := LoadGraphStateForComponent(ctx, r.services.Graph, source.ID)
	if err != nil {
		return fmt.Errorf("load graph state for dynatrace component %s: %w", source.ID, err)
	}

	delta := BuildGraphDelta(existingNodes, existingEdges, desiredNodes, nil)
	if err := ApplyGraphDelta(ctx, r.services.Graph, delta); err != nil {
		return fmt.Errorf("apply graph delta for dynatrace component %s: %w", source.ID, err)
	}

	_ = existingEdges
	r.logger.Info("dynatrace refresh completed", "component_id", source.ID)
	return nil
}

// refreshNewRelicComponent creates a graph node for a New Relic source.
func (r *Refresher) refreshNewRelicComponent(ctx context.Context, source *store.Component, _ newrelicadapter.NewRelicAdapter) error {
	r.logger.Info("refreshing newrelic component", "component_id", source.ID)

	now := time.Now()
	nodeID := obsNodeID(source.ID, source.Type)

	desiredNodes := []graph.Node{
		{
			ID:          nodeID,
			Type:        "newrelic_component",
			ComponentID: source.ID,
			Metadata: map[string]any{
				"component_id":   source.ID,
				"component_type": source.Type,
				"name":           source.Name,
			},
			LastSeen: now,
		},
	}

	existingNodes, existingEdges, err := LoadGraphStateForComponent(ctx, r.services.Graph, source.ID)
	if err != nil {
		return fmt.Errorf("load graph state for newrelic component %s: %w", source.ID, err)
	}

	delta := BuildGraphDelta(existingNodes, existingEdges, desiredNodes, nil)
	if err := ApplyGraphDelta(ctx, r.services.Graph, delta); err != nil {
		return fmt.Errorf("apply graph delta for newrelic component %s: %w", source.ID, err)
	}

	_ = existingEdges
	r.logger.Info("newrelic refresh completed", "component_id", source.ID)
	return nil
}
