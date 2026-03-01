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

	// Phase 6.11 Security & runtime source types.
	SourceTypeFalco = "falco"

	// Phase 6.13 — Artifact registry source types.
	SourceTypeOCIRegistry = "oci_registry" // DockerHub, GHCR, Harbor, Quay
	SourceTypeDockerHub   = "dockerhub"    // DockerHub alias (uses OCI adapter)
	SourceTypeArtifactory = "artifactory"  // JFrog Artifactory
	SourceTypeECR         = "ecr"          // AWS Elastic Container Registry

	// Phase 10 — Code review source types.
	SourceTypeGitHub = "github"
	SourceTypeGitLab = "gitlab"
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
		SourceTypeFalco,
		SourceTypeOCIRegistry,
		SourceTypeDockerHub,
		SourceTypeArtifactory,
		SourceTypeECR,
		SourceTypeGitHub,
		SourceTypeGitLab,
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
		SourceTypeEnvoy,
		SourceTypeFalco,
		SourceTypeOCIRegistry,
		SourceTypeDockerHub,
		SourceTypeArtifactory,
		SourceTypeECR,
		SourceTypeGitHub,
		SourceTypeGitLab:
		return true
	default:
		return false
	}
}

// Phase 10 constants are appended below — source types for GitHub and GitLab.
