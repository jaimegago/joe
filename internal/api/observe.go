package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	alertmanageradapter "github.com/jaimegago/joe/internal/adapters/alerting/alertmanager"
	pagerdutyadapter "github.com/jaimegago/joe/internal/adapters/alerting/pagerduty"
	datadogadapter "github.com/jaimegago/joe/internal/adapters/observability/datadog"
	dynatraceadapter "github.com/jaimegago/joe/internal/adapters/observability/dynatrace"
	jaegeradapter "github.com/jaimegago/joe/internal/adapters/observability/jaeger"
	lokiadapter "github.com/jaimegago/joe/internal/adapters/observability/loki"
	newrelicadapter "github.com/jaimegago/joe/internal/adapters/observability/newrelic"
	prometheusadapter "github.com/jaimegago/joe/internal/adapters/observability/prometheus"
	splunkadapter "github.com/jaimegago/joe/internal/adapters/observability/splunk"
	tempoadapter "github.com/jaimegago/joe/internal/adapters/observability/tempo"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/observe"
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

// resolveSourceForService finds the first source for a service via the given graph edge relation.
// Returns (sourceID, sourceType, error).
func (s *Server) resolveSourceForService(r *http.Request, service, relation string) (string, string, error) {
	ctx := r.Context()

	nodes, err := s.services.Graph.Query(ctx, service)
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

	subgraph, err := s.services.Graph.Related(ctx, serviceNodeID, 1)
	if err != nil {
		return "", "", fmt.Errorf("graph related failed: %w", err)
	}

	for _, edge := range subgraph.Edges {
		if edge.Relation == relation && edge.From == serviceNodeID {
			for _, node := range subgraph.Nodes {
				if node.ID == edge.To && node.SourceID != "" {
					return node.SourceID, node.Type, nil
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

	sourceID, sourceType, err := s.resolveSourceForService(r, req.Service, graph.RelationMetricsIn)
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
		Source:      sourceType,
		SourceID:    sourceID,
		NativeQuery: nativeQuery,
		Data:        []observe.DataPoint{},
	}

	adapter, err := s.getAdapter(sourceID)
	if err != nil {
		writeError(w, http.StatusNotFound, errorCodeNotFound, fmt.Sprintf("adapter not found: %s", sourceID))
		return
	}

	now := time.Now()
	from := now.Add(-time.Hour).Unix()
	to := now.Unix()

	switch a := adapter.(type) {
	case prometheusadapter.PrometheusAdapter:
		raw, qErr := a.Query(r.Context(), nativeQuery, time.Time{})
		if qErr != nil {
			writeInternalError(w, qErr, "prometheus query")
			return
		}
		result.RawResult = raw
	case datadogadapter.DatadogAdapter:
		raw, qErr := a.MetricsQuery(r.Context(), nativeQuery, from, to)
		if qErr != nil {
			writeInternalError(w, qErr, "datadog metrics query")
			return
		}
		result.RawResult = raw
	case dynatraceadapter.DynatraceAdapter:
		raw, qErr := a.MetricsQuery(r.Context(), nativeQuery, from*1000, to*1000) // Dynatrace uses ms
		if qErr != nil {
			writeInternalError(w, qErr, "dynatrace metrics query")
			return
		}
		result.RawResult = raw
	case newrelicadapter.NewRelicAdapter:
		raw, qErr := a.NRQLQuery(r.Context(), 0, nativeQuery)
		if qErr != nil {
			writeInternalError(w, qErr, "newrelic metrics query")
			return
		}
		result.RawResult = raw
	default:
		writeError(w, http.StatusBadRequest, errorCodeInvalidSource,
			fmt.Sprintf("source %q (type %q) does not support metrics queries via the observe API", sourceID, sourceType))
		return
	}

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

	sourceID, sourceType, err := s.resolveSourceForService(r, req.Service, graph.RelationLogsIn)
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
		Source:      sourceType,
		SourceID:    sourceID,
		NativeQuery: nativeQuery,
		Data:        []observe.DataPoint{},
	}

	adapter, err := s.getAdapter(sourceID)
	if err != nil {
		writeError(w, http.StatusNotFound, errorCodeNotFound, fmt.Sprintf("adapter not found: %s", sourceID))
		return
	}

	now := time.Now()
	from := now.Add(-time.Hour).Unix()
	to := now.Unix()

	switch a := adapter.(type) {
	case lokiadapter.LokiAdapter:
		raw, qErr := a.Query(r.Context(), nativeQuery, 100, time.Hour)
		if qErr != nil {
			writeInternalError(w, qErr, "loki logs query")
			return
		}
		result.RawResult = raw
	case datadogadapter.DatadogAdapter:
		raw, qErr := a.LogsSearch(r.Context(), nativeQuery, from, to, 100)
		if qErr != nil {
			writeInternalError(w, qErr, "datadog logs search")
			return
		}
		result.RawResult = raw
	case splunkadapter.SplunkAdapter:
		raw, qErr := a.Search(r.Context(), nativeQuery, "-1h", "now", 100)
		if qErr != nil {
			writeInternalError(w, qErr, "splunk logs search")
			return
		}
		result.RawResult = raw
	default:
		writeError(w, http.StatusBadRequest, errorCodeInvalidSource,
			fmt.Sprintf("source %q (type %q) does not support logs queries via the observe API", sourceID, sourceType))
		return
	}

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

	sourceID, sourceType, err := s.resolveSourceForService(r, req.Service, graph.RelationTracesIn)
	if err != nil {
		writeError(w, http.StatusNotFound, errorCodeNotFound, err.Error())
		return
	}

	result := &observe.ObservabilityResult{
		Source:      sourceType,
		SourceID:    sourceID,
		NativeQuery: fmt.Sprintf("service=%s", req.Service),
		Data:        []observe.DataPoint{},
	}

	adapter, err := s.getAdapter(sourceID)
	if err != nil {
		writeError(w, http.StatusNotFound, errorCodeNotFound, fmt.Sprintf("adapter not found: %s", sourceID))
		return
	}

	switch a := adapter.(type) {
	case tempoadapter.TempoAdapter:
		raw, qErr := a.Search(r.Context(), req.Service, "", 0, 0, 20)
		if qErr != nil {
			writeInternalError(w, qErr, "tempo traces search")
			return
		}
		result.RawResult = raw
	case jaegeradapter.JaegerAdapter:
		raw, qErr := a.SearchTraces(r.Context(), req.Service, "", 20)
		if qErr != nil {
			writeInternalError(w, qErr, "jaeger traces search")
			return
		}
		result.RawResult = raw
	default:
		writeError(w, http.StatusBadRequest, errorCodeInvalidSource,
			fmt.Sprintf("source %q (type %q) does not support traces queries via the observe API", sourceID, sourceType))
		return
	}

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
		sourceID, sourceType, resolveErr = s.resolveSourceForService(r, req.Service, graph.RelationAlertsIn)
		if resolveErr != nil {
			// Also try paged_via (PagerDuty)
			sourceID, sourceType, resolveErr = s.resolveSourceForService(r, req.Service, graph.RelationPagedVia)
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

	result.Source = sourceType
	result.SourceID = sourceID

	adapter, err := s.getAdapter(sourceID)
	if err != nil {
		writeError(w, http.StatusNotFound, errorCodeNotFound, fmt.Sprintf("adapter not found: %s", sourceID))
		return
	}

	switch a := adapter.(type) {
	case alertmanageradapter.AlertmanagerAdapter:
		alerts, qErr := a.ListAlerts(r.Context(), req.Question)
		if qErr != nil {
			writeInternalError(w, qErr, "alertmanager list alerts")
			return
		}
		for _, alert := range alerts {
			result.Alerts = append(result.Alerts, observe.Alert{
				Name:    alert.Labels["alertname"],
				State:   alert.Status.State,
				Labels:  alert.Labels,
				Summary: alert.Annotations["summary"],
			})
		}
	case pagerdutyadapter.PagerDutyAdapter:
		incidents, qErr := a.ListIncidents(r.Context(), "", "triggered,acknowledged", 50)
		if qErr != nil {
			writeInternalError(w, qErr, "pagerduty list incidents")
			return
		}
		for _, inc := range incidents {
			result.Alerts = append(result.Alerts, observe.Alert{
				Name:    inc.Title,
				State:   inc.Status,
				Summary: inc.Description,
			})
		}
	default:
		writeError(w, http.StatusBadRequest, errorCodeInvalidSource,
			fmt.Sprintf("source %q (type %q) does not support alerts queries via the observe API", sourceID, sourceType))
		return
	}

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
	sourceID, err := s.resolveK8sSourceForService(r, req.Service)
	if err != nil {
		writeError(w, http.StatusNotFound, errorCodeNotFound, err.Error())
		return
	}

	k8sAdapter, err := s.getK8sAdapter(sourceID)
	if handleAdapterLookupError(w, err, sourceID, "kubernetes") {
		return
	}

	result := &observe.K8sResult{
		Source:      "kubernetes",
		SourceID:    sourceID,
		NativeQuery: fmt.Sprintf("service=%s", req.Service),
	}

	wantLogs := strings.Contains(strings.ToLower(req.Question), "log")

	if wantLogs {
		pods, lErr := k8sAdapter.ListResources(r.Context(), "pods", "")
		if lErr != nil {
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

		logs, lErr := k8sAdapter.GetPodLogs(r.Context(), ns, podName, "", 100)
		if lErr != nil {
			writeInternalError(w, lErr, "observe k8s get pod logs")
			return
		}
		result.NativeQuery = fmt.Sprintf("logs pod=%s namespace=%s", podName, ns)
		result.Data = map[string]any{"pod": podName, "namespace": ns, "logs": logs}
	} else {
		pods, lErr := k8sAdapter.ListResources(r.Context(), "pods", "")
		if lErr != nil {
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

// resolveK8sSourceForService finds a k8s source for a service by locating k8s-type nodes
// in the graph that are related to the service.
func (s *Server) resolveK8sSourceForService(r *http.Request, service string) (string, error) {
	ctx := r.Context()

	nodes, err := s.services.Graph.Query(ctx, service)
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
	subgraph, err := s.services.Graph.Related(ctx, serviceNodeID, 2)
	if err != nil {
		return "", fmt.Errorf("graph related failed: %w", err)
	}

	for _, node := range subgraph.Nodes {
		if strings.HasPrefix(node.Type, "k8s_") && node.SourceID != "" {
			return node.SourceID, nil
		}
	}

	return "", fmt.Errorf("no Kubernetes source found for service %q — ensure the service has k8s nodes in the graph", service)
}
