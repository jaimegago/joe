package graph

// Node-type constants: the single source of the graph's node-type vocabulary.
//
// Every node type a refresher writes is declared here, and every consumer that
// selects nodes by type references these constants rather than re-encoding a
// string. The set is not hand-kept beside the writers: TestNodeTypeVocabulary
// (nodetypes_guard_test.go) fails the suite if a writer or reader uses a
// string literal where a node type belongs, or if a constant here is written
// by no refresher. A reader can therefore name only a type some writer emits.
//
// Out of the set by design: the computed observability anchor
// (source.Type + "_component" in observability_refresh.go), whose value is a
// function of the component-type registry rather than a literal any writer
// declares, and the parked LLM graph-write tool (D-0110), whose type comes
// from tool arguments.
const (
	// Component anchors for the refreshers whose nodes are otherwise only the
	// resources they discover — k8s_refresh.go, aws_refresh.go,
	// azure_refresh.go, git_refresh.go. One per component, carrying no edges.
	NodeTypeKubernetesComponent = "kubernetes_component"
	NodeTypeAWSComponent        = "aws_component"
	NodeTypeAzureComponent      = "azure_component"
	NodeTypeGitComponent        = "git_component"

	// Kubernetes resources — k8s_refresh.go.
	NodeTypeDeployment  = "deployment"
	NodeTypeStatefulSet = "statefulset"
	NodeTypeDaemonSet   = "daemonset"
	NodeTypeService     = "service"
	NodeTypeConfigMap   = "configmap"
	NodeTypeSecret      = "secret"
	NodeTypeNamespace   = "namespace"
	NodeTypeNode        = "node"

	// AWS — aws_refresh.go.
	NodeTypeVPC         = "vpc"
	NodeTypeEC2Instance = "ec2_instance"
	NodeTypeEKSCluster  = "eks_cluster"
	NodeTypeRDSInstance = "rds_instance"

	// Azure — azure_refresh.go.
	NodeTypeVNet        = "vnet"
	NodeTypeVM          = "vm"
	NodeTypeAKSCluster  = "aks_cluster"
	NodeTypeSQLDatabase = "sql_database"

	// Git — git_refresh.go.
	NodeTypeCodeHost = "code_host"
	NodeTypeGitRepo  = "git_repo"

	// Kubernetes CRDs — crd_refresh.go.
	NodeTypeKEDAScaledObject      = "keda_scaledobject"
	NodeTypeCertificate           = "certificate"
	NodeTypeOPAConstraintTemplate = "opa_constraint_template"
	NodeTypeCiliumNetworkPolicy   = "cilium_network_policy"
	NodeTypeIstioVirtualService   = "istio_virtual_service"
	NodeTypeCrossplaneResource    = "crossplane_resource"

	// GitOps / CD / IaC — gitops_refresh.go.
	NodeTypeArgoCDComponent    = "argocd_component"
	NodeTypeArgoCDApp          = "argocd_app"
	NodeTypeHelmComponent      = "helm_component"
	NodeTypeHelmRelease        = "helm_release"
	NodeTypeTerraformComponent = "terraform_component"
	NodeTypeTerraformResource  = "terraform_resource"

	// Networking — networking_refresh.go.
	NodeTypeNginxComponent = "nginx_component"
	NodeTypeNginxIngress   = "nginx_ingress"
	NodeTypeEnvoyComponent = "envoy_component"

	// Artifact registries — registry_refresh.go.
	NodeTypeArtifactRegistry = "artifact_registry"
	NodeTypeImageRepository  = "image_repository"

	// Alerting — alerting_refresh.go.
	NodeTypeAlertmanagerComponent = "alertmanager_component"
	NodeTypePagerDutyComponent    = "pagerduty_component"
	NodeTypeGrafanaComponent      = "grafana_component"

	// Data stores — datastore_refresh.go.
	NodeTypeKafkaComponent         = "kafka_component"
	NodeTypePostgreSQLComponent    = "postgresql_component"
	NodeTypeMySQLComponent         = "mysql_component"
	NodeTypeRedisComponent         = "redis_component"
	NodeTypeMongoDBComponent       = "mongodb_component"
	NodeTypeElasticsearchComponent = "elasticsearch_component"

	// Observability backends — observability_refresh.go.
	NodeTypeLokiComponent      = "loki_component"
	NodeTypeTempoComponent     = "tempo_component"
	NodeTypeJaegerComponent    = "jaeger_component"
	NodeTypeDatadogComponent   = "datadog_component"
	NodeTypeSplunkComponent    = "splunk_component"
	NodeTypeDynatraceComponent = "dynatrace_component"
	NodeTypeNewRelicComponent  = "newrelic_component"
)
