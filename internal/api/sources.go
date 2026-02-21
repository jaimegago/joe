package api

import (
	"encoding/json"
	"io"
	"net/http"

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

func (s *Server) handleListSources(w http.ResponseWriter, r *http.Request) {
	sources, err := s.services.Store.Sources.List(r.Context())
	if err != nil {
		writeInternalError(w, err, "list sources")
		return
	}

	if sources == nil {
		sources = []*store.Source{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"sources": sources,
		"count":   len(sources),
	})
}

// createSourceRequest is the JSON body for POST /api/v1/sources.
type createSourceRequest struct {
	ID     string          `json:"id"`
	Type   string          `json:"type"`
	Name   string          `json:"name"`
	Config json.RawMessage `json:"config"`
}

func (s *Server) handleCreateSource(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "failed to read body")
		return
	}

	var req createSourceRequest
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

	if !store.IsValidSourceType(req.Type) {
		writeError(w, http.StatusBadRequest, errorCodeInvalidSource, "unsupported source type", map[string]any{
			"type":    req.Type,
			"allowed": store.AllowedSourceTypes(),
		})
		return
	}

	// Check if source already exists
	existing, err := s.services.Store.Sources.Get(r.Context(), req.ID)
	if err != nil {
		writeInternalError(w, err, "get source")
		return
	}
	if existing != nil {
		writeError(w, http.StatusConflict, errorCodeInvalidRequest, "source already exists", map[string]any{
			"source_id": req.ID,
		})
		return
	}

	source := &store.Source{
		ID:     req.ID,
		Type:   req.Type,
		Name:   req.Name,
		Config: req.Config,
	}

	// Try to connect the adapter before saving
	ctx := r.Context()
	switch req.Type {
	case store.SourceTypeAWS:
		adapter := awsadapter.New()
		if err := adapter.Connect(ctx, *source); err != nil {
			writeBadRequest(w, err, "connect aws source", "failed to connect to AWS")
			return
		}
		s.services.Adapters.Register(req.ID, adapter)
	case store.SourceTypeAzure:
		adapter := azureadapter.New()
		if err := adapter.Connect(ctx, *source); err != nil {
			writeBadRequest(w, err, "connect azure source", "failed to connect to Azure")
			return
		}
		s.services.Adapters.Register(req.ID, adapter)
	case store.SourceTypeKubernetes:
		adapter := k8s.New()
		if err := adapter.Connect(ctx, *source); err != nil {
			writeBadRequest(w, err, "connect kubernetes source", "failed to connect to cluster")
			return
		}
		s.services.Adapters.Register(req.ID, adapter)
	case store.SourceTypeGit:
		adapter := gitadapter.New()
		if err := adapter.Connect(ctx, *source); err != nil {
			writeBadRequest(w, err, "connect git source", "failed to connect to git repo")
			return
		}
		s.services.Adapters.Register(req.ID, adapter)
	case store.SourceTypePrometheus, store.SourceTypeMimir:
		adapter := prometheusadapter.New()
		if err := adapter.Connect(ctx, *source); err != nil {
			writeBadRequest(w, err, "connect prometheus source", "failed to connect to Prometheus/Mimir")
			return
		}
		s.services.Adapters.Register(req.ID, adapter)
	case store.SourceTypeLoki:
		adapter := lokiadapter.New()
		if err := adapter.Connect(ctx, *source); err != nil {
			writeBadRequest(w, err, "connect loki source", "failed to connect to Loki")
			return
		}
		s.services.Adapters.Register(req.ID, adapter)
	case store.SourceTypeTempo:
		adapter := tempoadapter.New()
		if err := adapter.Connect(ctx, *source); err != nil {
			writeBadRequest(w, err, "connect tempo source", "failed to connect to Tempo")
			return
		}
		s.services.Adapters.Register(req.ID, adapter)
	case store.SourceTypeJaeger:
		adapter := jaegeradapter.New()
		if err := adapter.Connect(ctx, *source); err != nil {
			writeBadRequest(w, err, "connect jaeger source", "failed to connect to Jaeger")
			return
		}
		s.services.Adapters.Register(req.ID, adapter)
	case store.SourceTypeAlertmanager:
		adapter := alertmanageradapter.New()
		if err := adapter.Connect(ctx, *source); err != nil {
			writeBadRequest(w, err, "connect alertmanager source", "failed to connect to Alertmanager")
			return
		}
		s.services.Adapters.Register(req.ID, adapter)
	case store.SourceTypePagerDuty:
		adapter := pagerdutyadapter.New()
		if err := adapter.Connect(ctx, *source); err != nil {
			writeBadRequest(w, err, "connect pagerduty source", "failed to connect to PagerDuty")
			return
		}
		s.services.Adapters.Register(req.ID, adapter)
	case store.SourceTypeGrafana:
		adapter := grafanaadapter.New()
		if err := adapter.Connect(ctx, *source); err != nil {
			writeBadRequest(w, err, "connect grafana source", "failed to connect to Grafana")
			return
		}
		s.services.Adapters.Register(req.ID, adapter)
	case store.SourceTypePostgreSQL:
		adapter := postgresadapter.New()
		if err := adapter.Connect(ctx, *source); err != nil {
			writeBadRequest(w, err, "connect postgresql source", "failed to connect to PostgreSQL")
			return
		}
		s.services.Adapters.Register(req.ID, adapter)
	case store.SourceTypeMySQL:
		adapter := mysqladapter.New()
		if err := adapter.Connect(ctx, *source); err != nil {
			writeBadRequest(w, err, "connect mysql source", "failed to connect to MySQL")
			return
		}
		s.services.Adapters.Register(req.ID, adapter)
	case store.SourceTypeRedis:
		adapter := redisadapter.New()
		if err := adapter.Connect(ctx, *source); err != nil {
			writeBadRequest(w, err, "connect redis source", "failed to connect to Redis")
			return
		}
		s.services.Adapters.Register(req.ID, adapter)
	case store.SourceTypeMongoDB:
		adapter := mongodbadapter.New()
		if err := adapter.Connect(ctx, *source); err != nil {
			writeBadRequest(w, err, "connect mongodb source", "failed to connect to MongoDB")
			return
		}
		s.services.Adapters.Register(req.ID, adapter)
	case store.SourceTypeKafka:
		adapter := kafkaadapter.New()
		if err := adapter.Connect(ctx, *source); err != nil {
			writeBadRequest(w, err, "connect kafka source", "failed to connect to Kafka")
			return
		}
		s.services.Adapters.Register(req.ID, adapter)
	case store.SourceTypeElasticsearch:
		adapter := elasticsearchadapter.New()
		if err := adapter.Connect(ctx, *source); err != nil {
			writeBadRequest(w, err, "connect elasticsearch source", "failed to connect to Elasticsearch")
			return
		}
		s.services.Adapters.Register(req.ID, adapter)
	case store.SourceTypeArgoCd:
		adapter := argocdadapter.New()
		if err := adapter.Connect(ctx, *source); err != nil {
			writeBadRequest(w, err, "connect argocd source", "failed to connect to Argo CD")
			return
		}
		s.services.Adapters.Register(req.ID, adapter)
	case store.SourceTypeTerraform:
		adapter := terraformadapter.New()
		if err := adapter.Connect(ctx, *source); err != nil {
			writeBadRequest(w, err, "connect terraform source", "failed to load Terraform state")
			return
		}
		s.services.Adapters.Register(req.ID, adapter)
	case store.SourceTypeHelm:
		adapter := helmadapter.New()
		if err := adapter.Connect(ctx, *source); err != nil {
			writeBadRequest(w, err, "connect helm source", "failed to connect to Helm")
			return
		}
		s.services.Adapters.Register(req.ID, adapter)
	case store.SourceTypeNginx:
		adapter := nginxadapter.New()
		if err := adapter.Connect(ctx, *source); err != nil {
			writeBadRequest(w, err, "connect nginx source", "failed to connect to NGINX Ingress Controller")
			return
		}
		s.services.Adapters.Register(req.ID, adapter)
	case store.SourceTypeEnvoy:
		adapter := envoyadapter.New()
		if err := adapter.Connect(ctx, *source); err != nil {
			writeBadRequest(w, err, "connect envoy source", "failed to connect to Envoy admin API")
			return
		}
		s.services.Adapters.Register(req.ID, adapter)
	case store.SourceTypeFalco:
		adapter := falcoadapter.New()
		if err := adapter.Connect(ctx, *source); err != nil {
			writeBadRequest(w, err, "connect falco source", "failed to connect to Falco")
			return
		}
		s.services.Adapters.Register(req.ID, adapter)
	}

	if err := s.services.Store.Sources.Create(r.Context(), source); err != nil {
		writeInternalError(w, err, "create source")
		return
	}

	writeJSON(w, http.StatusCreated, source)
}

func (s *Server) handleGetSource(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing source id", map[string]any{
			"param": "id",
		})
		return
	}

	source, err := s.services.Store.Sources.Get(r.Context(), id)
	if err != nil {
		writeInternalError(w, err, "get source")
		return
	}
	if source == nil {
		writeError(w, http.StatusNotFound, errorCodeNotFound, "source not found", map[string]any{
			"source_id": id,
		})
		return
	}

	writeJSON(w, http.StatusOK, source)
}

func (s *Server) handleDeleteSource(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing source id", map[string]any{
			"param": "id",
		})
		return
	}

	source, err := s.services.Store.Sources.Get(r.Context(), id)
	if err != nil {
		writeInternalError(w, err, "get source")
		return
	}
	if source == nil {
		writeError(w, http.StatusNotFound, errorCodeNotFound, "source not found", map[string]any{
			"source_id": id,
		})
		return
	}

	// Disconnect and unregister adapter
	if err := s.services.Adapters.Unregister(id); err != nil {
		writeInternalError(w, err, "unregister source")
		return
	}

	if err := s.services.Store.Sources.Delete(r.Context(), id); err != nil {
		writeInternalError(w, err, "delete source")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
