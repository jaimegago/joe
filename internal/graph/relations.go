package graph

// Relation constants for graph edges.
const (
	RelationMetricsIn   = "metrics_in"
	RelationLogsIn      = "logs_in"
	RelationTracesIn    = "traces_in"
	RelationAlertsIn    = "alerts_in"
	RelationPagedVia    = "paged_via"
	RelationDashboardIn = "dashboard_in"
	RelationIsK8sNode   = "is_k8s_node"

	// Phase 6.7 — data store relations.
	RelationStoresIn = "stores_in" // service → database (postgresql/mysql/mongodb/redis/elasticsearch)
	RelationQueuesIn = "queues_in" // service → message broker (kafka)

	// Phase 6.8 — GitOps / CD / IaC relations.
	RelationManagedBy  = "managed_by" // k8s resource → argo cd app / flux / helm release
	RelationProvisions = "provisions" // terraform resource / crossplane → cloud resource

	// Phase 6.9 — Networking & ingress relations.
	RelationIngressFor = "ingress_for" // nginx ingress → backend service
	RelationProxies    = "proxies"     // envoy → service
	RelationMeshFor    = "mesh_for"    // istio config → service

	// Phase 6.9/6.10 — Policy enforcement relations.
	RelationPolicyEnforces = "policy_enforces" // opa constraint / cilium policy → namespace/workload

	// Phase 6.10 — K8s CRD relations.
	RelationScaledBy = "scaled_by" // keda scaled object → workload
	RelationSecures  = "secures"   // certificate → service/ingress

	// Phase 6.13 — Artifact registry relations.
	RelationImageStoredIn = "image_stored_in" // k8s deployment → image_repository
	RelationPublishesTo   = "publishes_to"    // git_repo → image_repository
)
