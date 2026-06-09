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
	apiK8sBasePath          = "/api/v1/k8s"
	apiGitBasePath          = "/api/v1/git"
	apiAWSBasePath          = "/api/v1/aws"
	apiPrometheusBasePath   = "/api/v1/prometheus"
	apiLokiBasePath         = "/api/v1/loki"
	apiTempoBasePath        = "/api/v1/tempo"
	apiJaegerBasePath       = "/api/v1/jaeger"
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

	// GitOps, CD & IaC base paths.
	apiArgoCDBasePath    = "/api/v1/argocd"
	apiTerraformBasePath = "/api/v1/terraform"
	apiHelmBasePath      = "/api/v1/helm"

	// Networking & Ingress base paths.
	apiNginxBasePath = "/api/v1/nginx"
	apiEnvoyBasePath = "/api/v1/envoy"

	// Security & runtime base paths.
	apiFalcoBasePath = "/api/v1/falco"

	// Proprietary observability vendor base paths (Phase 6, Step 12).
	apiDatadogBasePath   = "/api/v1/datadog"
	apiSplunkBasePath    = "/api/v1/splunk"
	apiDynatraceBasePath = "/api/v1/dynatrace"
	apiNewRelicBasePath  = "/api/v1/newrelic"

	// Knowledge store base paths (Phase 7).
	apiKnowledgeEntriesPath = "/api/v1/knowledge/entries"
	apiKnowledgeSearchPath  = "/api/v1/knowledge/search"
	apiKnowledgeSourcesPath = "/api/v1/knowledge/components"

	// Artifact registry base paths (Phase 6.13).
	apiRegistryOCIBasePath         = "/api/v1/registry/oci"
	apiRegistryArtifactoryBasePath = "/api/v1/registry/artifactory"
	apiRegistryECRBasePath         = "/api/v1/registry/ecr"

	// GitHub PR / GitLab MR base paths (Phase 10).
	apiGitHubBasePath = "/api/v1/github"
	apiGitLabBasePath = "/api/v1/gitlab"

	// Category-based observe endpoints (MCP plan).
	apiObserveBasePath = "/api/v1/observe"

	// Model control plane (Phase 2): list/swap the single LLM contact point.
	apiModelsPath        = "/api/v1/models"
	apiModelsCurrentPath = "/api/v1/models/current"

	// Streamed agentic turn for the thin CLI (Phase 2, SSE).
	apiTasksStreamPath = "/api/v1/tasks/stream"
)
