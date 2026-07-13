package coreagent

import (
	"context"
	"fmt"
	"strings"
	"time"

	envoypadapter "github.com/jaimegago/joe/internal/adapters/networking/envoy"
	nginxadapter "github.com/jaimegago/joe/internal/adapters/networking/nginx"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/store"
)

// refreshNginxComponent refreshes an NGINX Ingress Controller source.
// Creates ingress nodes from K8s Ingress resources and builds ingress_for
// edges linking each ingress to the backend service it routes to.
func (r *Refresher) refreshNginxComponent(ctx context.Context, source *store.Component, adapter nginxadapter.NginxAdapter) error {
	r.logger.Info("refreshing nginx component", "component_id", source.ID)

	now := time.Now()
	sourceNodeID := networkingNodeID(source.ID, source.Type)

	desiredNodes := []graph.Node{
		{
			ID:          sourceNodeID,
			Type:        "nginx_component",
			ComponentID: source.ID,
			Metadata:    networkingMetadata(source),
			LastSeen:    now,
		},
	}
	desiredEdges := make([]graph.Edge, 0)

	// List all ingresses across all namespaces.
	ingresses, err := adapter.ListIngresses(ctx, "")
	if err != nil {
		r.logger.Warn("failed to list nginx ingresses (skipping edge discovery)", "component_id", source.ID, "error", err)
	} else {
		for _, ing := range ingresses {
			ingNodeID := fmt.Sprintf("nginx/%s/%s/%s", source.ID, ing.Namespace, ing.Name)
			desiredNodes = append(desiredNodes, graph.Node{
				ID:          ingNodeID,
				Type:        "nginx_ingress",
				ComponentID: source.ID,
				Metadata: map[string]any{
					"name":      ing.Name,
					"namespace": ing.Namespace,
					"class":     ing.Class,
				},
				LastSeen: now,
			})

			// For each path rule, find the backend service and create ingress_for edge.
			for _, rule := range ing.Rules {
				for _, path := range rule.Paths {
					if path.ServiceName == "" {
						continue
					}
					edges := r.buildIngressForEdges(ctx, source, ingNodeID, path.ServiceName, ing.Namespace, rule.Host, now)
					desiredEdges = append(desiredEdges, edges...)
				}
			}
		}
	}

	existingNodes, existingEdges, err := LoadGraphStateForComponent(ctx, r.services.Graph, source.ID)
	if err != nil {
		return fmt.Errorf("load graph state for nginx component %s: %w", source.ID, err)
	}

	delta := BuildGraphDelta(existingNodes, existingEdges, desiredNodes, desiredEdges)
	if err := ApplyGraphDelta(ctx, r.services.Graph, delta); err != nil {
		return fmt.Errorf("apply graph delta for nginx component %s: %w", source.ID, err)
	}

	r.logger.Info("nginx refresh completed",
		"component_id", source.ID,
		"ingresses", len(ingresses),
		"nodes", len(desiredNodes),
		"edges", len(desiredEdges),
	)
	return nil
}

// refreshEnvoyComponent refreshes an Envoy proxy source.
// Creates a source node and builds proxies edges from the Envoy instance to
// service nodes matching its upstream cluster names.
func (r *Refresher) refreshEnvoyComponent(ctx context.Context, source *store.Component, adapter envoypadapter.EnvoyAdapter) error {
	r.logger.Info("refreshing envoy component", "component_id", source.ID)

	now := time.Now()
	sourceNodeID := networkingNodeID(source.ID, source.Type)

	desiredNodes := []graph.Node{
		{
			ID:          sourceNodeID,
			Type:        "envoy_component",
			ComponentID: source.ID,
			Metadata:    networkingMetadata(source),
			LastSeen:    now,
		},
	}
	desiredEdges := make([]graph.Edge, 0)

	clusters, err := adapter.Clusters(ctx)
	if err != nil {
		r.logger.Warn("failed to list envoy clusters (skipping edge discovery)", "component_id", source.ID, "error", err)
	} else {
		for _, cluster := range clusters {
			// Cluster names often encode the service name (e.g. "outbound|80||payment.default.svc.cluster.local").
			svcName := extractServiceNameFromCluster(cluster.Name)
			if svcName == "" {
				continue
			}
			edges := r.buildProxiesEdges(ctx, source, sourceNodeID, svcName, cluster.Name, now)
			desiredEdges = append(desiredEdges, edges...)
		}
	}

	existingNodes, existingEdges, err := LoadGraphStateForComponent(ctx, r.services.Graph, source.ID)
	if err != nil {
		return fmt.Errorf("load graph state for envoy component %s: %w", source.ID, err)
	}

	delta := BuildGraphDelta(existingNodes, existingEdges, desiredNodes, desiredEdges)
	if err := ApplyGraphDelta(ctx, r.services.Graph, delta); err != nil {
		return fmt.Errorf("apply graph delta for envoy component %s: %w", source.ID, err)
	}

	r.logger.Info("envoy refresh completed",
		"component_id", source.ID,
		"clusters", len(clusters),
		"nodes", len(desiredNodes),
		"edges", len(desiredEdges),
	)
	return nil
}

// buildIngressForEdges finds K8s service/deployment nodes matching backendSvc
// and creates ingress_for edges from the ingress node to those service nodes.
func (r *Refresher) buildIngressForEdges(ctx context.Context, source *store.Component, ingNodeID, backendSvc, namespace, host string, now time.Time) []graph.Edge {
	var edges []graph.Edge

	matchingNodes, err := r.services.Graph.Query(ctx, backendSvc)
	if err != nil {
		r.logger.Debug("graph query failed for ingress_for match", "service", backendSvc, "error", err)
		return edges
	}

	for _, node := range matchingNodes {
		if node.Type != "service" && node.Type != "deployment" {
			continue
		}
		// Prefer same-namespace matches.
		if namespace != "" {
			nodeNS, _ := node.Metadata["namespace"].(string)
			if nodeNS != "" && nodeNS != namespace {
				continue
			}
		}
		ctx := "backend=" + backendSvc
		if host != "" {
			ctx += ",host=" + host
		}
		edges = append(edges, graph.Edge{
			From:        ingNodeID,
			To:          node.ID,
			Relation:    graph.RelationIngressFor,
			Confidence:  graph.Explicit,
			Source:      "nginx_ingress_rules",
			ComponentID: source.ID,
			Context:     ctx,
			CreatedAt:   now,
		})
	}

	return edges
}

// buildProxiesEdges finds service/deployment nodes matching svcName and creates
// proxies edges from the Envoy node to those service nodes.
func (r *Refresher) buildProxiesEdges(ctx context.Context, source *store.Component, envoyNodeID, svcName, clusterName string, now time.Time) []graph.Edge {
	var edges []graph.Edge

	matchingNodes, err := r.services.Graph.Query(ctx, svcName)
	if err != nil {
		r.logger.Debug("graph query failed for proxies match", "service", svcName, "error", err)
		return edges
	}

	for _, node := range matchingNodes {
		if node.Type != "service" && node.Type != "deployment" {
			continue
		}
		edges = append(edges, graph.Edge{
			From:        envoyNodeID,
			To:          node.ID,
			Relation:    graph.RelationProxies,
			Confidence:  graph.Inferred,
			Source:      "envoy_clusters",
			ComponentID: source.ID,
			Context:     "cluster=" + clusterName,
			CreatedAt:   now,
		})
	}

	return edges
}

// extractServiceNameFromCluster extracts a service name from an Envoy cluster
// name. Handles two common formats:
//   - Istio format: "outbound|80||payment.default.svc.cluster.local" → "payment"
//   - Simple format: "payment" or "payment_80" → "payment"
func extractServiceNameFromCluster(clusterName string) string {
	// Istio outbound format: outbound|port||fqdn
	if strings.HasPrefix(clusterName, "outbound|") || strings.HasPrefix(clusterName, "inbound|") {
		parts := strings.SplitN(clusterName, "|", 4)
		if len(parts) == 4 && parts[3] != "" {
			fqdn := parts[3]
			// Extract the first label of the FQDN as the service name.
			host := strings.SplitN(fqdn, ".", 2)[0]
			if host != "" && host != "PassthroughCluster" && host != "BlackHoleCluster" {
				return host
			}
		}
		return ""
	}

	// Strip common suffixes like "_80", "_443", "_grpc".
	name := clusterName
	if idx := strings.LastIndex(name, "_"); idx > 0 {
		suffix := name[idx+1:]
		if isPortOrProto(suffix) {
			name = name[:idx]
		}
	}

	return name
}

// isPortOrProto reports whether s looks like a port number or protocol name.
func isPortOrProto(s string) bool {
	switch s {
	case "http", "https", "grpc", "tcp", "udp":
		return true
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(s) > 0
}

// networkingNodeID builds a stable graph node ID for a networking source.
func networkingNodeID(sourceID, sourceType string) string {
	return fmt.Sprintf("networking/%s/%s", sourceType, sourceID)
}

// networkingMetadata builds the standard metadata map for a networking source node.
func networkingMetadata(source *store.Component) map[string]any {
	return map[string]any{
		"component_id":   source.ID,
		"component_type": source.Type,
		"name":           source.Name,
	}
}
