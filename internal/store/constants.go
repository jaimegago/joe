package store

// UNREGISTRABLE component types.
//
// A constant marked UNREGISTRABLE below is deliberately ABSENT from
// AllowedComponentTypes / IsValidComponentType. Every surface that admits a
// component type consults that single seam, so all of them reject an
// unregistrable type with exactly the invalid-type response a wholly unknown
// type takes: the HTTP create endpoint (handleCreateComponent,
// internal/api/components.go), the register_component LLM tool
// (RegisterComponentTool.Execute, internal/coreagent/agent.go), the web
// registration form (whose type selector is populated from
// handleListComponentTypes → AllowedComponentTypes), and the auto_promote_reads
// admin surface (listReadPromotions / setReadPromotion, internal/api/admin.go).
// No surface is special-cased.
//
// An unregistrable constant stays DEFINED when code outside the two registrable
// lists still names it — the boot connect pass (connectSourcesDefault,
// cmd/joe/server.go), the coreagent refresh type-switch
// (internal/coreagent/refresh.go), or the runtime adapter map (newAdapterForType,
// internal/api/components.go). Those paths act on STORED rows, which registration
// can no longer create, so they are dead but harmless and are left in place
// deliberately. A constant that nothing outside the registrable lists references
// is deleted outright instead.
//
// Read paths are type-agnostic — GET, list, and Test read and serialize the
// stored type without validating it — so a row stored before a type became
// unregistrable still lists and reads.
//
// Two trims populated this set. D-0058 removed the artifact-registry group for
// having no construction path. trim-unsupported-component-types removed the
// types that fail the D-0055 documentable gate: none was credential-wired
// (internal/credential/wiring.go), so promotion already rejected them and they
// could never be completed into a working integration. Restoring any of them
// means wiring its credential path first and only then adding it back to the two
// lists below; that work is deferred for the types still marked below, see
// docs/backlog/trim-deadonarrival-component-types.md.
//
// git is the ONE type that has taken that restore path (D-0150): it is
// credential-wired to two kinds — a static HTTPS-token reference and an explicit
// "none" kind for public repositories — and is registrable again. The other types
// this trim removed are unchanged.
const (
	ComponentTypeAWS        = "aws"   // UNREGISTRABLE — not credential-wired
	ComponentTypeAzure      = "azure" // UNREGISTRABLE — not credential-wired
	ComponentTypeGit        = "git"
	ComponentTypeKubernetes = "kubernetes"

	ComponentTypePrometheus = "prometheus"
	ComponentTypeMimir      = "mimir"
	ComponentTypeLoki       = "loki"
	ComponentTypeTempo      = "tempo"
	ComponentTypeJaeger     = "jaeger"
	ComponentTypeDatadog    = "datadog" // UNREGISTRABLE — not credential-wired
	ComponentTypeSplunk     = "splunk"
	ComponentTypeDynatrace  = "dynatrace"
	ComponentTypeNewRelic   = "newrelic"

	ComponentTypeAlertmanager = "alertmanager"
	ComponentTypePagerDuty    = "pagerduty"
	ComponentTypeGrafana      = "grafana"

	// Phase 6.7 data store source types. UNREGISTRABLE as a group — none is
	// credential-wired.
	ComponentTypePostgreSQL    = "postgresql"
	ComponentTypeMySQL         = "mysql"
	ComponentTypeRedis         = "redis"
	ComponentTypeMongoDB       = "mongodb"
	ComponentTypeKafka         = "kafka"
	ComponentTypeElasticsearch = "elasticsearch"

	// Phase 6.8 GitOps, CD & IaC source types.
	ComponentTypeArgoCd    = "argocd"
	ComponentTypeTerraform = "terraform"
	ComponentTypeHelm      = "helm" // UNREGISTRABLE — not credential-wired

	// Phase 6.9 Networking & Ingress source types.
	ComponentTypeNginx = "nginx-ingress" // UNREGISTRABLE — not credential-wired
	ComponentTypeEnvoy = "envoy"

	// Phase 6.11 Security & runtime source types.
	ComponentTypeFalco = "falco"

	// Phase 6.13 — Artifact registry source types.
	//
	// UNREGISTRABLE as a group (D-0058), for a reason distinct from the
	// credential-wiring gate above: these four have no construction path at all.
	// Their adapter packages and refresh/query paths exist but are wired into
	// neither construction map (neither connectSourcesDefault nor
	// newAdapterForType builds them), so their refresh cases can never be
	// reached. The constants remain defined because the refresh type-switch
	// still names them. Wiring them into a construction map is deferred; see
	// docs/backlog/trim-deadonarrival-component-types.md.
	ComponentTypeOCIRegistry = "oci_registry" // DockerHub, GHCR, Harbor, Quay
	ComponentTypeDockerHub   = "dockerhub"    // DockerHub alias (uses OCI adapter)
	ComponentTypeArtifactory = "artifactory"  // JFrog Artifactory
	ComponentTypeECR         = "ecr"          // AWS Elastic Container Registry

	// Phase 10 — Code review source types.
	ComponentTypeGitHub = "github"
	ComponentTypeGitLab = "gitlab"
)

// AllowedComponentTypes returns the registrable source types. It and
// IsValidComponentType are the single authoritative seam every registration
// surface consults; a type absent from both is unregistrable everywhere. Keep
// the two in lockstep — see the UNREGISTRABLE note on the constant block for
// which types are deliberately excluded and why.
func AllowedComponentTypes() []string {
	return []string{
		ComponentTypeKubernetes,
		ComponentTypeGit,
		ComponentTypePrometheus,
		ComponentTypeMimir,
		ComponentTypeLoki,
		ComponentTypeTempo,
		ComponentTypeJaeger,
		ComponentTypeSplunk,
		ComponentTypeDynatrace,
		ComponentTypeNewRelic,
		ComponentTypeAlertmanager,
		ComponentTypePagerDuty,
		ComponentTypeGrafana,
		ComponentTypeArgoCd,
		ComponentTypeTerraform,
		ComponentTypeEnvoy,
		ComponentTypeFalco,
		ComponentTypeGitHub,
		ComponentTypeGitLab,
	}
}

// IsValidComponentType reports whether the source type is registrable. It must
// admit exactly the set AllowedComponentTypes returns.
func IsValidComponentType(sourceType string) bool {
	switch sourceType {
	case
		ComponentTypeKubernetes,
		ComponentTypeGit,
		ComponentTypePrometheus,
		ComponentTypeMimir,
		ComponentTypeLoki,
		ComponentTypeTempo,
		ComponentTypeJaeger,
		ComponentTypeSplunk,
		ComponentTypeDynatrace,
		ComponentTypeNewRelic,
		ComponentTypeAlertmanager,
		ComponentTypePagerDuty,
		ComponentTypeGrafana,
		ComponentTypeArgoCd,
		ComponentTypeTerraform,
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
