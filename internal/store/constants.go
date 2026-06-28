package store

const (
	ComponentTypeAWS        = "aws"
	ComponentTypeAzure      = "azure"
	ComponentTypeGit        = "git"
	ComponentTypeKubernetes = "kubernetes"

	ComponentTypePrometheus = "prometheus"
	ComponentTypeMimir      = "mimir"
	ComponentTypeLoki       = "loki"
	ComponentTypeTempo      = "tempo"
	ComponentTypeJaeger     = "jaeger"
	ComponentTypeDatadog    = "datadog"
	ComponentTypeSplunk     = "splunk"
	ComponentTypeDynatrace  = "dynatrace"
	ComponentTypeNewRelic   = "newrelic"

	ComponentTypeAlertmanager = "alertmanager"
	ComponentTypePagerDuty    = "pagerduty"
	ComponentTypeGrafana      = "grafana"

	// Phase 6.7 data store source types.
	ComponentTypePostgreSQL    = "postgresql"
	ComponentTypeMySQL         = "mysql"
	ComponentTypeRedis         = "redis"
	ComponentTypeMongoDB       = "mongodb"
	ComponentTypeKafka         = "kafka"
	ComponentTypeElasticsearch = "elasticsearch"

	// Phase 6.8 GitOps, CD & IaC source types.
	ComponentTypeArgoCd    = "argocd"
	ComponentTypeTerraform = "terraform"
	ComponentTypeHelm      = "helm"

	// Phase 6.9 Networking & Ingress source types.
	ComponentTypeNginx = "nginx-ingress"
	ComponentTypeEnvoy = "envoy"

	// Phase 6.11 Security & runtime source types.
	ComponentTypeFalco = "falco"

	// Phase 6.13 — Artifact registry source types.
	//
	// DEAD-ON-ARRIVAL / UNREGISTRABLE: these four are deliberately ABSENT from
	// AllowedComponentTypes / IsValidComponentType below, so no surface (HTTP
	// create, register_component tool, web form) will accept them. The constants
	// remain defined only because the coreagent refresh type-switch
	// (internal/coreagent/refresh.go) still names them; their adapter packages
	// and refresh/query paths exist but are never wired into any construction map
	// (neither connectSourcesDefault in cmd/joe/server.go nor newAdapterForType in
	// internal/api/components.go builds them), so those refresh cases can never be
	// reached — they are dead but harmless. Wiring these four into a construction
	// map is deferred post-launch work; see
	// docs/backlog/trim-deadonarrival-component-types.md.
	ComponentTypeOCIRegistry = "oci_registry" // DockerHub, GHCR, Harbor, Quay
	ComponentTypeDockerHub   = "dockerhub"    // DockerHub alias (uses OCI adapter)
	ComponentTypeArtifactory = "artifactory"  // JFrog Artifactory
	ComponentTypeECR         = "ecr"          // AWS Elastic Container Registry

	// Phase 10 — Code review source types.
	ComponentTypeGitHub = "github"
	ComponentTypeGitLab = "gitlab"
)

// AllowedComponentTypes returns the supported source types.
func AllowedComponentTypes() []string {
	return []string{
		ComponentTypeAWS,
		ComponentTypeAzure,
		ComponentTypeGit,
		ComponentTypeKubernetes,
		ComponentTypePrometheus,
		ComponentTypeMimir,
		ComponentTypeLoki,
		ComponentTypeTempo,
		ComponentTypeJaeger,
		ComponentTypeDatadog,
		ComponentTypeSplunk,
		ComponentTypeDynatrace,
		ComponentTypeNewRelic,
		ComponentTypeAlertmanager,
		ComponentTypePagerDuty,
		ComponentTypeGrafana,
		ComponentTypePostgreSQL,
		ComponentTypeMySQL,
		ComponentTypeRedis,
		ComponentTypeMongoDB,
		ComponentTypeKafka,
		ComponentTypeElasticsearch,
		ComponentTypeArgoCd,
		ComponentTypeTerraform,
		ComponentTypeHelm,
		ComponentTypeNginx,
		ComponentTypeEnvoy,
		ComponentTypeFalco,
		ComponentTypeGitHub,
		ComponentTypeGitLab,
	}
}

// IsValidComponentType reports whether the source type is supported.
func IsValidComponentType(sourceType string) bool {
	switch sourceType {
	case
		ComponentTypeAWS,
		ComponentTypeAzure,
		ComponentTypeGit,
		ComponentTypeKubernetes,
		ComponentTypePrometheus,
		ComponentTypeMimir,
		ComponentTypeLoki,
		ComponentTypeTempo,
		ComponentTypeJaeger,
		ComponentTypeDatadog,
		ComponentTypeSplunk,
		ComponentTypeDynatrace,
		ComponentTypeNewRelic,
		ComponentTypeAlertmanager,
		ComponentTypePagerDuty,
		ComponentTypeGrafana,
		ComponentTypePostgreSQL,
		ComponentTypeMySQL,
		ComponentTypeRedis,
		ComponentTypeMongoDB,
		ComponentTypeKafka,
		ComponentTypeElasticsearch,
		ComponentTypeArgoCd,
		ComponentTypeTerraform,
		ComponentTypeHelm,
		ComponentTypeNginx,
		ComponentTypeEnvoy,
		ComponentTypeFalco,
		ComponentTypeGitHub,
		ComponentTypeGitLab:
		return true
	default:
		return false
	}
}

// Phase 10 constants are appended below — source types for GitHub and GitLab.
