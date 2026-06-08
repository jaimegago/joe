package coreagent

import (
	"context"
	"fmt"
	"time"

	alertmanageradapter "github.com/jaimegago/joe/internal/adapters/alerting/alertmanager"
	grafanaadapter "github.com/jaimegago/joe/internal/adapters/alerting/grafana"
	pagerdutyadapter "github.com/jaimegago/joe/internal/adapters/alerting/pagerduty"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/store"
)

// refreshAlertmanagerComponent refreshes an Alertmanager source.
// Creates a graph node for the source and builds alerts_in edges for any
// services that have active alerts.
func (r *Refresher) refreshAlertmanagerComponent(ctx context.Context, source *store.Component, adapter alertmanageradapter.AlertmanagerAdapter) error {
	r.logger.Info("refreshing alertmanager source", "component_id", source.ID)

	now := time.Now()
	nodeID := alertingNodeID(source.ID, source.Type)

	desiredNodes := []graph.Node{
		{
			ID:          nodeID,
			Type:        "alertmanager_component",
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

	// Discover active alerts and create alerts_in edges for matching services.
	alerts, err := adapter.ListAlerts(ctx, "")
	if err != nil {
		r.logger.Warn("failed to list alertmanager alerts (skipping edge discovery)", "component_id", source.ID, "error", err)
	} else {
		edges, edgeErr := r.buildAlertsInEdges(ctx, source, nodeID, alerts, now)
		if edgeErr != nil {
			r.logger.Warn("failed to build alerts_in edges", "component_id", source.ID, "error", edgeErr)
		} else {
			desiredEdges = append(desiredEdges, edges...)
		}
	}

	existingNodes, existingEdges, err := LoadGraphStateForComponent(ctx, r.services.Graph, source.ID)
	if err != nil {
		return fmt.Errorf("load graph state for alertmanager source %s: %w", source.ID, err)
	}

	delta := BuildGraphDelta(existingNodes, existingEdges, desiredNodes, desiredEdges)
	if err := ApplyGraphDelta(ctx, r.services.Graph, delta); err != nil {
		return fmt.Errorf("apply graph delta for alertmanager source %s: %w", source.ID, err)
	}

	r.logger.Info("alertmanager refresh completed",
		"component_id", source.ID,
		"nodes", len(desiredNodes),
		"edges", len(desiredEdges),
	)
	return nil
}

// buildAlertsInEdges creates alerts_in edges from services to the Alertmanager node
// based on active alert labels.
func (r *Refresher) buildAlertsInEdges(ctx context.Context, source *store.Component, amNodeID string, alerts []alertmanageradapter.Alert, now time.Time) ([]graph.Edge, error) {
	var edges []graph.Edge
	seen := make(map[string]bool) // deduplicate by service node ID

	for _, alert := range alerts {
		if alert.Status.State != "active" {
			continue
		}

		// Try to match by "service" label first, then "job" label.
		svcName := alert.Labels["service"]
		if svcName == "" {
			svcName = alert.Labels["job"]
		}
		if svcName == "" {
			continue
		}

		if seen[svcName] {
			continue
		}

		matchingNodes, err := r.services.Graph.Query(ctx, svcName)
		if err != nil {
			r.logger.Debug("graph query failed for alert service", "service", svcName, "error", err)
			continue
		}

		for _, svcNode := range matchingNodes {
			if svcNode.Type != "service" && svcNode.Type != "deployment" {
				continue
			}
			if seen[svcNode.ID] {
				continue
			}
			seen[svcNode.ID] = true
			edges = append(edges, graph.Edge{
				From:        svcNode.ID,
				To:          amNodeID,
				Relation:    graph.RelationAlertsIn,
				Confidence:  graph.Inferred,
				Source:      "alertmanager_labels",
				ComponentID: source.ID,
				Context:     "service=" + svcName,
				CreatedAt:   now,
			})
		}
	}

	return edges, nil
}

// refreshPagerDutyComponent refreshes a PagerDuty source.
// Creates a graph node and builds paged_via edges for services with open incidents.
func (r *Refresher) refreshPagerDutyComponent(ctx context.Context, source *store.Component, adapter pagerdutyadapter.PagerDutyAdapter) error {
	r.logger.Info("refreshing pagerduty source", "component_id", source.ID)

	now := time.Now()
	nodeID := alertingNodeID(source.ID, source.Type)

	desiredNodes := []graph.Node{
		{
			ID:          nodeID,
			Type:        "pagerduty_component",
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

	// Discover services with open incidents and create paged_via edges.
	incidents, err := adapter.ListIncidents(ctx, "", "triggered,acknowledged", 100)
	if err != nil {
		r.logger.Warn("failed to list pagerduty incidents (skipping edge discovery)", "component_id", source.ID, "error", err)
	} else {
		for _, incident := range incidents {
			svcName := incident.Service.Name
			if svcName == "" {
				continue
			}

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
					Relation:    graph.RelationPagedVia,
					Confidence:  graph.Inferred,
					Source:      "pagerduty_incidents",
					ComponentID: source.ID,
					Context:     "pd_service=" + incident.Service.ID,
					CreatedAt:   now,
				})
			}
		}
	}

	existingNodes, existingEdges, err := LoadGraphStateForComponent(ctx, r.services.Graph, source.ID)
	if err != nil {
		return fmt.Errorf("load graph state for pagerduty source %s: %w", source.ID, err)
	}

	delta := BuildGraphDelta(existingNodes, existingEdges, desiredNodes, desiredEdges)
	if err := ApplyGraphDelta(ctx, r.services.Graph, delta); err != nil {
		return fmt.Errorf("apply graph delta for pagerduty source %s: %w", source.ID, err)
	}

	r.logger.Info("pagerduty refresh completed",
		"component_id", source.ID,
		"nodes", len(desiredNodes),
		"edges", len(desiredEdges),
	)
	return nil
}

// refreshGrafanaComponent refreshes a Grafana source.
// Creates a graph node only; dashboard_in edges are discovered via .joe/ files.
func (r *Refresher) refreshGrafanaComponent(ctx context.Context, source *store.Component, _ grafanaadapter.GrafanaAdapter) error {
	r.logger.Info("refreshing grafana source", "component_id", source.ID)

	now := time.Now()
	nodeID := alertingNodeID(source.ID, source.Type)

	desiredNodes := []graph.Node{
		{
			ID:          nodeID,
			Type:        "grafana_component",
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
		return fmt.Errorf("load graph state for grafana source %s: %w", source.ID, err)
	}

	delta := BuildGraphDelta(existingNodes, existingEdges, desiredNodes, nil)
	if err := ApplyGraphDelta(ctx, r.services.Graph, delta); err != nil {
		return fmt.Errorf("apply graph delta for grafana source %s: %w", source.ID, err)
	}

	_ = existingEdges
	r.logger.Info("grafana refresh completed", "component_id", source.ID)
	return nil
}

// alertingNodeID builds a stable graph node ID for an alerting source.
func alertingNodeID(sourceID, sourceType string) string {
	return fmt.Sprintf("alerting/%s/%s", sourceType, sourceID)
}
