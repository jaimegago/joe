package client

import "time"

const (
	// DefaultTimeout is the default HTTP client timeout.
	DefaultTimeout = 30 * time.Second

	// API endpoints.
	apiStatusPath       = "/api/v1/status"
	apiGraphQueryPath   = "/api/v1/graph/query"
	apiGraphRelatedPath = "/api/v1/graph/related"
	apiGraphSummaryPath = "/api/v1/graph/summary"
	apiSourcesPath      = "/api/v1/sources"
	apiK8sBasePath      = "/api/v1/k8s"
	apiGitBasePath      = "/api/v1/git"
	apiAWSBasePath        = "/api/v1/aws"
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
)
