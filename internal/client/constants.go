package client

import "time"

const (
	// DefaultTimeout is the default HTTP client timeout.
	DefaultTimeout = 30 * time.Second

	// API endpoints.
	apiStatusPath           = "/api/v1/status"
	apiGraphQueryPath       = "/api/v1/graph/query"
	apiGraphRelatedPath     = "/api/v1/graph/related"
	apiGraphSummaryPath     = "/api/v1/graph/summary"
	apiComponentsPath       = "/api/v1/components"
	apiAlertmanagerBasePath = "/api/v1/alertmanager"
	apiPagerDutyBasePath    = "/api/v1/pagerduty"
	apiGrafanaBasePath      = "/api/v1/grafana"
	// Datastore base paths.
	apiPostgresBasePath      = "/api/v1/postgresql"
	apiMySQLBasePath         = "/api/v1/mysql"
	apiRedisBasePath         = "/api/v1/redis"
	apiMongoDBBasePath       = "/api/v1/mongodb"
	apiKafkaBasePath         = "/api/v1/kafka"
	apiElasticsearchBasePath = "/api/v1/elasticsearch"

	// Networking & Ingress base paths.
	apiNginxBasePath = "/api/v1/nginx"
	apiEnvoyBasePath = "/api/v1/envoy"

	// Security & runtime base paths.
	apiFalcoBasePath = "/api/v1/falco"

	// Artifact registry base paths (Phase 6.13).
	apiRegistryOCIBasePath         = "/api/v1/registry/oci"
	apiRegistryArtifactoryBasePath = "/api/v1/registry/artifactory"
	apiRegistryECRBasePath         = "/api/v1/registry/ecr"

	// Category-based observe endpoints (MCP plan).
	apiObserveBasePath = "/api/v1/observe"

	// Model control plane (Phase 2): list/swap the single LLM contact point.
	apiModelsPath        = "/api/v1/models"
	apiModelsCurrentPath = "/api/v1/models/current"

	// Streamed agentic turn for the thin CLI (Phase 2, SSE).
	apiTasksStreamPath = "/api/v1/tasks/stream"
)
