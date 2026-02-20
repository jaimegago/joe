package store

const (
	SourceTypeAWS        = "aws"
	SourceTypeAzure      = "azure"
	SourceTypeGit        = "git"
	SourceTypeKubernetes = "kubernetes"

	SourceTypePrometheus   = "prometheus"
	SourceTypeMimir        = "mimir"
	SourceTypeLoki         = "loki"
	SourceTypeTempo        = "tempo"
	SourceTypeJaeger       = "jaeger"
	SourceTypeDatadog      = "datadog"
	SourceTypeSplunk       = "splunk"
	SourceTypeDynatrace    = "dynatrace"
	SourceTypeNewRelic     = "newrelic"
	SourceTypeCloudWatch   = "cloudwatch"
	SourceTypeAzureMonitor = "azuremonitor"

	SourceTypeAlertmanager = "alertmanager"
	SourceTypePagerDuty    = "pagerduty"
	SourceTypeGrafana      = "grafana"

	// Phase 6.7 data store source types.
	SourceTypePostgreSQL    = "postgresql"
	SourceTypeMySQL         = "mysql"
	SourceTypeRedis         = "redis"
	SourceTypeMongoDB       = "mongodb"
	SourceTypeKafka         = "kafka"
	SourceTypeElasticsearch = "elasticsearch"

	// Phase 6.8 GitOps, CD & IaC source types.
	SourceTypeArgoCd    = "argocd"
	SourceTypeTerraform = "terraform"
	SourceTypeHelm      = "helm"

	// Phase 6.9 Networking & Ingress source types.
	SourceTypeNginx = "nginx-ingress"
	SourceTypeEnvoy = "envoy"
)

// AllowedSourceTypes returns the supported source types.
func AllowedSourceTypes() []string {
	return []string{
		SourceTypeAWS,
		SourceTypeAzure,
		SourceTypeGit,
		SourceTypeKubernetes,
		SourceTypePrometheus,
		SourceTypeMimir,
		SourceTypeLoki,
		SourceTypeTempo,
		SourceTypeJaeger,
		SourceTypeDatadog,
		SourceTypeSplunk,
		SourceTypeDynatrace,
		SourceTypeNewRelic,
		SourceTypeCloudWatch,
		SourceTypeAzureMonitor,
		SourceTypeAlertmanager,
		SourceTypePagerDuty,
		SourceTypeGrafana,
		SourceTypePostgreSQL,
		SourceTypeMySQL,
		SourceTypeRedis,
		SourceTypeMongoDB,
		SourceTypeKafka,
		SourceTypeElasticsearch,
		SourceTypeArgoCd,
		SourceTypeTerraform,
		SourceTypeHelm,
		SourceTypeNginx,
		SourceTypeEnvoy,
	}
}

// IsValidSourceType reports whether the source type is supported.
func IsValidSourceType(sourceType string) bool {
	switch sourceType {
	case
		SourceTypeAWS,
		SourceTypeAzure,
		SourceTypeGit,
		SourceTypeKubernetes,
		SourceTypePrometheus,
		SourceTypeMimir,
		SourceTypeLoki,
		SourceTypeTempo,
		SourceTypeJaeger,
		SourceTypeDatadog,
		SourceTypeSplunk,
		SourceTypeDynatrace,
		SourceTypeNewRelic,
		SourceTypeCloudWatch,
		SourceTypeAzureMonitor,
		SourceTypeAlertmanager,
		SourceTypePagerDuty,
		SourceTypeGrafana,
		SourceTypePostgreSQL,
		SourceTypeMySQL,
		SourceTypeRedis,
		SourceTypeMongoDB,
		SourceTypeKafka,
		SourceTypeElasticsearch,
		SourceTypeArgoCd,
		SourceTypeTerraform,
		SourceTypeHelm,
		SourceTypeNginx,
		SourceTypeEnvoy:
		return true
	default:
		return false
	}
}
