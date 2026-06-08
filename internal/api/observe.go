package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/observe"
	"github.com/jaimegago/joe/internal/rbac"
)

// registerObserveCategoryRoutes registers the category-based observe endpoints.
func (s *Server) registerObserveCategoryRoutes(mux *http.ServeMux, prefix string) {
	mux.HandleFunc(fmt.Sprintf("POST %s/observe/metrics", prefix), s.handleObserveMetrics)
	mux.HandleFunc(fmt.Sprintf("POST %s/observe/logs", prefix), s.handleObserveLogs)
	mux.HandleFunc(fmt.Sprintf("POST %s/observe/traces", prefix), s.handleObserveTraces)
	mux.HandleFunc(fmt.Sprintf("POST %s/observe/alerts", prefix), s.handleObserveAlerts)
	mux.HandleFunc(fmt.Sprintf("POST %s/observe/k8s", prefix), s.handleObserveK8s)
}

// observeCategoryRequest is the shared request body for all /observe/* endpoints.
type observeCategoryRequest struct {
	Service  string `json:"service"`
	Question string `json:"question"`
}

// resolveComponentForService finds the first source for a service via the given graph edge relation.
// Returns (sourceID, sourceType, error).
func (s *Server) resolveComponentForService(r *http.Request, service, relation string) (string, string, error) {
	ctx := r.Context()
	principal := rbac.PrincipalFromContext(ctx)

	nodes, err := s.accessor.GraphQuery(ctx, principal, service)
	if err != nil {
		return "", "", fmt.Errorf("graph query failed: %w", err)
	}
	if len(nodes) == 0 {
		return "", "", fmt.Errorf("no graph node found for service %q", service)
	}

	// Prefer a node of type "service"; fall back to first result.
	serviceNodeID := nodes[0].ID
	for _, n := range nodes {
		if n.Type == "service" {
			serviceNodeID = n.ID
			break
		}
	}

	subgraph, err := s.accessor.GraphRelated(ctx, principal, serviceNodeID, 1)
	if err != nil {
		return "", "", fmt.Errorf("graph related failed: %w", err)
	}

	for _, edge := range subgraph.Edges {
		if edge.Relation == relation && edge.From == serviceNodeID {
			for _, node := range subgraph.Nodes {
				if node.ID == edge.To && node.ComponentID != "" {
					return node.ComponentID, node.Type, nil
				}
			}
		}
	}

	return "", "", fmt.Errorf("no %s source found for service %q — add a %s edge in the graph", relation, service, relation)
}

// translateQuestion uses the LLM to translate a natural language question to a native query.
// Falls back to returning the question as-is when no LLM is configured.
func (s *Server) translateQuestion(r *http.Request, question, sourceType string) (string, error) {
	if s.services.LLM == nil {
		return question, nil
	}
	t := observe.NewLLMTranslator(s.services.LLM)
	return t.Translate(r.Context(), question, sourceType)
}

// handleObserveMetrics handles POST /api/v1/observe/metrics.
// Resolves the metrics source for the service, translates the question, and executes the query.
func (s *Server) handleObserveMetrics(w http.ResponseWriter, r *http.Request) {
	var req observeCategoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, err, "observe metrics", "invalid JSON payload")
		return
	}
	if req.Service == "" || req.Question == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "fields 'service' and 'question' are required")
		return
	}

	sourceID, sourceType, err := s.resolveComponentForService(r, req.Service, graph.RelationMetricsIn)
	if err != nil {
		writeError(w, http.StatusNotFound, errorCodeNotFound, err.Error())
		return
	}

	nativeQuery, err := s.translateQuestion(r, req.Question, sourceType)
	if err != nil {
		writeInternalError(w, err, "observe metrics translation")
		return
	}

	result := &observe.ObservabilityResult{
		Component:   sourceType,
		ComponentID: sourceID,
		NativeQuery: nativeQuery,
		Data:        []observe.DataPoint{},
	}

	principal := rbac.PrincipalFromContext(r.Context())
	now := time.Now()
	from := now.Add(-time.Hour).Unix()
	to := now.Unix()

	raw, supported, qErr := s.accessor.ObserveMetrics(r.Context(), principal, sourceID, nativeQuery, from, to)
	if qErr != nil {
		if handleAccessError(w, qErr, sourceID, sourceType) {
			return
		}
		writeInternalError(w, qErr, "observe metrics query")
		return
	}
	if !supported {
		writeError(w, http.StatusBadRequest, errorCodeInvalidComponent,
			fmt.Sprintf("source %q (type %q) does not support metrics queries via the observe API", sourceID, sourceType))
		return
	}
	result.RawResult = raw

	writeJSON(w, http.StatusOK, result)
}

// handleObserveLogs handles POST /api/v1/observe/logs.
func (s *Server) handleObserveLogs(w http.ResponseWriter, r *http.Request) {
	var req observeCategoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, err, "observe logs", "invalid JSON payload")
		return
	}
	if req.Service == "" || req.Question == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "fields 'service' and 'question' are required")
		return
	}

	sourceID, sourceType, err := s.resolveComponentForService(r, req.Service, graph.RelationLogsIn)
	if err != nil {
		writeError(w, http.StatusNotFound, errorCodeNotFound, err.Error())
		return
	}

	nativeQuery, err := s.translateQuestion(r, req.Question, sourceType)
	if err != nil {
		writeInternalError(w, err, "observe logs translation")
		return
	}

	result := &observe.ObservabilityResult{
		Component:   sourceType,
		ComponentID: sourceID,
		NativeQuery: nativeQuery,
		Data:        []observe.DataPoint{},
	}

	principal := rbac.PrincipalFromContext(r.Context())
	now := time.Now()
	from := now.Add(-time.Hour).Unix()
	to := now.Unix()

	raw, supported, qErr := s.accessor.ObserveLogs(r.Context(), principal, sourceID, nativeQuery, from, to)
	if qErr != nil {
		if handleAccessError(w, qErr, sourceID, sourceType) {
			return
		}
		writeInternalError(w, qErr, "observe logs query")
		return
	}
	if !supported {
		writeError(w, http.StatusBadRequest, errorCodeInvalidComponent,
			fmt.Sprintf("source %q (type %q) does not support logs queries via the observe API", sourceID, sourceType))
		return
	}
	result.RawResult = raw

	writeJSON(w, http.StatusOK, result)
}

// handleObserveTraces handles POST /api/v1/observe/traces.
func (s *Server) handleObserveTraces(w http.ResponseWriter, r *http.Request) {
	var req observeCategoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, err, "observe traces", "invalid JSON payload")
		return
	}
	if req.Service == "" || req.Question == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "fields 'service' and 'question' are required")
		return
	}

	sourceID, sourceType, err := s.resolveComponentForService(r, req.Service, graph.RelationTracesIn)
	if err != nil {
		writeError(w, http.StatusNotFound, errorCodeNotFound, err.Error())
		return
	}

	result := &observe.ObservabilityResult{
		Component:   sourceType,
		ComponentID: sourceID,
		NativeQuery: fmt.Sprintf("service=%s", req.Service),
		Data:        []observe.DataPoint{},
	}

	principal := rbac.PrincipalFromContext(r.Context())
	raw, supported, qErr := s.accessor.ObserveTraces(r.Context(), principal, sourceID, req.Service)
	if qErr != nil {
		if handleAccessError(w, qErr, sourceID, sourceType) {
			return
		}
		writeInternalError(w, qErr, "observe traces query")
		return
	}
	if !supported {
		writeError(w, http.StatusBadRequest, errorCodeInvalidComponent,
			fmt.Sprintf("source %q (type %q) does not support traces queries via the observe API", sourceID, sourceType))
		return
	}
	result.RawResult = raw

	writeJSON(w, http.StatusOK, result)
}

// handleObserveAlerts handles POST /api/v1/observe/alerts.
// service and question are optional; if provided, the graph is used to resolve the alerts source.
func (s *Server) handleObserveAlerts(w http.ResponseWriter, r *http.Request) {
	var req observeCategoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, err, "observe alerts", "invalid JSON payload")
		return
	}

	result := &observe.AlertsResult{
		Alerts: []observe.Alert{},
	}

	var sourceID, sourceType string
	var resolveErr error

	if req.Service != "" {
		sourceID, sourceType, resolveErr = s.resolveComponentForService(r, req.Service, graph.RelationAlertsIn)
		if resolveErr != nil {
			// Also try paged_via (PagerDuty)
			sourceID, sourceType, resolveErr = s.resolveComponentForService(r, req.Service, graph.RelationPagedVia)
		}
	}

	if sourceID == "" {
		msg := "fields 'service' is required and must have an alerts_in or paged_via edge in the graph"
		if resolveErr != nil {
			msg = resolveErr.Error()
		}
		writeError(w, http.StatusNotFound, errorCodeNotFound, msg)
		return
	}

	result.Component = sourceType
	result.ComponentID = sourceID

	principal := rbac.PrincipalFromContext(r.Context())
	alerts, supported, qErr := s.accessor.ObserveAlerts(r.Context(), principal, sourceID, req.Question)
	if qErr != nil {
		if handleAccessError(w, qErr, sourceID, sourceType) {
			return
		}
		writeInternalError(w, qErr, "observe alerts query")
		return
	}
	if !supported {
		writeError(w, http.StatusBadRequest, errorCodeInvalidComponent,
			fmt.Sprintf("source %q (type %q) does not support alerts queries via the observe API", sourceID, sourceType))
		return
	}
	result.Alerts = append(result.Alerts, alerts...)

	result.Count = len(result.Alerts)
	writeJSON(w, http.StatusOK, result)
}

// handleObserveK8s handles POST /api/v1/observe/k8s.
func (s *Server) handleObserveK8s(w http.ResponseWriter, r *http.Request) {
	var req observeCategoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, err, "observe k8s", "invalid JSON payload")
		return
	}
	if req.Service == "" || req.Question == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "fields 'service' and 'question' are required")
		return
	}

	// Resolve k8s source: look for k8s-type nodes related to the service in the graph.
	sourceID, err := s.resolveK8sComponentForService(r, req.Service)
	if err != nil {
		writeError(w, http.StatusNotFound, errorCodeNotFound, err.Error())
		return
	}

	principal := rbac.PrincipalFromContext(r.Context())

	result := &observe.K8sResult{
		Component:   "kubernetes",
		ComponentID: sourceID,
		NativeQuery: fmt.Sprintf("service=%s", req.Service),
	}

	wantLogs := strings.Contains(strings.ToLower(req.Question), "log")

	if wantLogs {
		pods, lErr := s.accessor.K8sListResources(r.Context(), principal, sourceID, "pods", "")
		if lErr != nil {
			if handleAccessError(w, lErr, sourceID, "kubernetes") {
				return
			}
			writeInternalError(w, lErr, "observe k8s list pods for logs")
			return
		}

		// Find first pod whose name contains the service name.
		podName, ns := "", ""
		for _, pod := range pods {
			meta, _ := pod.Object["metadata"].(map[string]any)
			if meta == nil {
				continue
			}
			name, _ := meta["name"].(string)
			namespace, _ := meta["namespace"].(string)
			if strings.Contains(name, req.Service) {
				podName = name
				ns = namespace
				break
			}
		}

		if podName == "" {
			result.Data = map[string]any{"message": fmt.Sprintf("no pods found matching service %q", req.Service)}
			writeJSON(w, http.StatusOK, result)
			return
		}

		logs, lErr := s.accessor.K8sGetPodLogs(r.Context(), principal, sourceID, ns, podName, "", 100)
		if lErr != nil {
			if handleAccessError(w, lErr, sourceID, "kubernetes") {
				return
			}
			writeInternalError(w, lErr, "observe k8s get pod logs")
			return
		}
		result.NativeQuery = fmt.Sprintf("logs pod=%s namespace=%s", podName, ns)
		result.Data = map[string]any{"pod": podName, "namespace": ns, "logs": logs}
	} else {
		pods, lErr := s.accessor.K8sListResources(r.Context(), principal, sourceID, "pods", "")
		if lErr != nil {
			if handleAccessError(w, lErr, sourceID, "kubernetes") {
				return
			}
			writeInternalError(w, lErr, "observe k8s list pods")
			return
		}

		// Filter by service name in pod name.
		resources := make([]map[string]any, 0, len(pods))
		for _, pod := range pods {
			meta, _ := pod.Object["metadata"].(map[string]any)
			if meta == nil {
				resources = append(resources, pod.Object)
				continue
			}
			name, _ := meta["name"].(string)
			if strings.Contains(name, req.Service) {
				resources = append(resources, pod.Object)
			}
		}
		result.Data = map[string]any{"pods": resources, "count": len(resources)}
	}

	writeJSON(w, http.StatusOK, result)
}

// resolveK8sComponentForService finds a k8s source for a service by locating k8s-type nodes
// in the graph that are related to the service.
func (s *Server) resolveK8sComponentForService(r *http.Request, service string) (string, error) {
	ctx := r.Context()
	principal := rbac.PrincipalFromContext(ctx)

	nodes, err := s.accessor.GraphQuery(ctx, principal, service)
	if err != nil || len(nodes) == 0 {
		return "", fmt.Errorf("no graph node found for service %q", service)
	}

	serviceNodeID := nodes[0].ID
	for _, n := range nodes {
		if n.Type == "service" {
			serviceNodeID = n.ID
			break
		}
	}

	// Depth 2 to catch k8s nodes linked via intermediate nodes.
	subgraph, err := s.accessor.GraphRelated(ctx, principal, serviceNodeID, 2)
	if err != nil {
		return "", fmt.Errorf("graph related failed: %w", err)
	}

	for _, node := range subgraph.Nodes {
		if strings.HasPrefix(node.Type, "k8s_") && node.ComponentID != "" {
			return node.ComponentID, nil
		}
	}

	return "", fmt.Errorf("no Kubernetes source found for service %q — ensure the service has k8s nodes in the graph", service)
}
