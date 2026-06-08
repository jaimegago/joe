package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/jaimegago/joe/internal/adapters"
	alertmanageradapter "github.com/jaimegago/joe/internal/adapters/alerting/alertmanager"
	grafanaadapter "github.com/jaimegago/joe/internal/adapters/alerting/grafana"
	pagerdutyadapter "github.com/jaimegago/joe/internal/adapters/alerting/pagerduty"
	awsadapter "github.com/jaimegago/joe/internal/adapters/aws"
	azureadapter "github.com/jaimegago/joe/internal/adapters/azure"
	elasticsearchadapter "github.com/jaimegago/joe/internal/adapters/datastore/elasticsearch"
	kafkaadapter "github.com/jaimegago/joe/internal/adapters/datastore/kafka"
	mongodbadapter "github.com/jaimegago/joe/internal/adapters/datastore/mongodb"
	mysqladapter "github.com/jaimegago/joe/internal/adapters/datastore/mysql"
	postgresadapter "github.com/jaimegago/joe/internal/adapters/datastore/postgres"
	redisadapter "github.com/jaimegago/joe/internal/adapters/datastore/redis"
	gitadapter "github.com/jaimegago/joe/internal/adapters/git"
	argocdadapter "github.com/jaimegago/joe/internal/adapters/gitops/argocd"
	terraformadapter "github.com/jaimegago/joe/internal/adapters/iac/terraform"
	"github.com/jaimegago/joe/internal/adapters/k8s"
	envoyadapter "github.com/jaimegago/joe/internal/adapters/networking/envoy"
	nginxadapter "github.com/jaimegago/joe/internal/adapters/networking/nginx"
	jaegeradapter "github.com/jaimegago/joe/internal/adapters/observability/jaeger"
	lokiadapter "github.com/jaimegago/joe/internal/adapters/observability/loki"
	prometheusadapter "github.com/jaimegago/joe/internal/adapters/observability/prometheus"
	tempoadapter "github.com/jaimegago/joe/internal/adapters/observability/tempo"
	helmadapter "github.com/jaimegago/joe/internal/adapters/packaging/helm"
	falcoadapter "github.com/jaimegago/joe/internal/adapters/security/falco"
	"github.com/jaimegago/joe/internal/store"
)

func (s *Server) handleListComponents(w http.ResponseWriter, r *http.Request) {
	components, err := s.services.Store.Components.List(r.Context())
	if err != nil {
		writeInternalError(w, err, "list components")
		return
	}

	if components == nil {
		components = []*store.Component{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"components": components,
		"count":      len(components),
	})
}

// newAdapterForType returns a fresh, unconnected adapter for the given source
// type, or nil when the type has no live connection to establish (config-only or
// metadata source types that are persisted as-is). It is the single source of
// truth for the type→adapter mapping shared by source creation and connection
// testing, so the two paths can never disagree on which components have adapters.
func newAdapterForType(sourceType string) adapters.Adapter {
	switch sourceType {
	case store.ComponentTypeAWS:
		return awsadapter.New()
	case store.ComponentTypeAzure:
		return azureadapter.New()
	case store.ComponentTypeKubernetes:
		return k8s.New()
	case store.ComponentTypeGit:
		return gitadapter.New()
	case store.ComponentTypePrometheus, store.ComponentTypeMimir:
		return prometheusadapter.New()
	case store.ComponentTypeLoki:
		return lokiadapter.New()
	case store.ComponentTypeTempo:
		return tempoadapter.New()
	case store.ComponentTypeJaeger:
		return jaegeradapter.New()
	case store.ComponentTypeAlertmanager:
		return alertmanageradapter.New()
	case store.ComponentTypePagerDuty:
		return pagerdutyadapter.New()
	case store.ComponentTypeGrafana:
		return grafanaadapter.New()
	case store.ComponentTypePostgreSQL:
		return postgresadapter.New()
	case store.ComponentTypeMySQL:
		return mysqladapter.New()
	case store.ComponentTypeRedis:
		return redisadapter.New()
	case store.ComponentTypeMongoDB:
		return mongodbadapter.New()
	case store.ComponentTypeKafka:
		return kafkaadapter.New()
	case store.ComponentTypeElasticsearch:
		return elasticsearchadapter.New()
	case store.ComponentTypeArgoCd:
		return argocdadapter.New()
	case store.ComponentTypeTerraform:
		return terraformadapter.New()
	case store.ComponentTypeHelm:
		return helmadapter.New()
	case store.ComponentTypeNginx:
		return nginxadapter.New()
	case store.ComponentTypeEnvoy:
		return envoyadapter.New()
	case store.ComponentTypeFalco:
		return falcoadapter.New()
	default:
		return nil
	}
}

// createComponentRequest is the JSON body for POST /api/v1/components.
type createComponentRequest struct {
	ID     string          `json:"id"`
	Type   string          `json:"type"`
	Name   string          `json:"name"`
	Config json.RawMessage `json:"config"`
}

func (s *Server) handleCreateComponent(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "failed to read body")
		return
	}

	var req createComponentRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "invalid JSON")
		return
	}

	if req.ID == "" || req.Type == "" || req.Name == "" {
		missing := []string{}
		if req.ID == "" {
			missing = append(missing, "id")
		}
		if req.Type == "" {
			missing = append(missing, "type")
		}
		if req.Name == "" {
			missing = append(missing, "name")
		}
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "id, type, and name are required", map[string]any{
			"missing": missing,
		})
		return
	}

	if !store.IsValidComponentType(req.Type) {
		writeError(w, http.StatusBadRequest, errorCodeInvalidComponent, "unsupported source type", map[string]any{
			"type":    req.Type,
			"allowed": store.AllowedComponentTypes(),
		})
		return
	}

	// Check if source already exists
	existing, err := s.services.Store.Components.Get(r.Context(), req.ID)
	if err != nil {
		writeInternalError(w, err, "get source")
		return
	}
	if existing != nil {
		writeError(w, http.StatusConflict, errorCodeInvalidRequest, "source already exists", map[string]any{
			"component_id": req.ID,
		})
		return
	}

	source := &store.Component{
		ID:     req.ID,
		Type:   req.Type,
		Name:   req.Name,
		Config: req.Config,
	}

	// Try to connect the adapter before saving. Source types with no adapter
	// (config-only/metadata types) return nil and are persisted directly.
	ctx := r.Context()
	if adapter := newAdapterForType(req.Type); adapter != nil {
		if err := adapter.Connect(ctx, *source); err != nil {
			writeBadRequest(w, err, "connect "+req.Type+" source", fmt.Sprintf("failed to connect to %s source", req.Type))
			return
		}
		s.services.Adapters.Register(req.ID, adapter)
	}

	if err := s.services.Store.Components.Create(r.Context(), source); err != nil {
		writeInternalError(w, err, "create source")
		return
	}

	writeJSON(w, http.StatusCreated, source)
}

func (s *Server) handleGetComponent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing source id", map[string]any{
			"param": "id",
		})
		return
	}

	source, err := s.services.Store.Components.Get(r.Context(), id)
	if err != nil {
		writeInternalError(w, err, "get source")
		return
	}
	if source == nil {
		writeError(w, http.StatusNotFound, errorCodeNotFound, "source not found", map[string]any{
			"component_id": id,
		})
		return
	}

	writeJSON(w, http.StatusOK, source)
}

func (s *Server) handleDeleteComponent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing source id", map[string]any{
			"param": "id",
		})
		return
	}

	source, err := s.services.Store.Components.Get(r.Context(), id)
	if err != nil {
		writeInternalError(w, err, "get source")
		return
	}
	if source == nil {
		writeError(w, http.StatusNotFound, errorCodeNotFound, "source not found", map[string]any{
			"component_id": id,
		})
		return
	}

	// Disconnect and unregister adapter
	if err := s.services.Adapters.Unregister(id); err != nil {
		writeInternalError(w, err, "unregister source")
		return
	}

	if err := s.services.Store.Components.Delete(r.Context(), id); err != nil {
		writeInternalError(w, err, "delete source")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
