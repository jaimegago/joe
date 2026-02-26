package safety

// ActionTier classifies what a tool can do to determine authorization requirements.
type ActionTier int

const (
	// TierObserve (T1) is read-only. Always allowed, no policy check needed.
	TierObserve ActionTier = iota + 1

	// TierRecord (T2) changes Joe's internal state (graph, facts, sources).
	// Requires opt-in via record policy. Does not affect external systems.
	TierRecord

	// TierAct (T3) changes external systems (files, infrastructure, deployments).
	// Denied by default. Requires per-action opt-in in act policy.
	TierAct
)

// String returns the human-readable tier label.
func (t ActionTier) String() string {
	switch t {
	case TierObserve:
		return "T1:Observe"
	case TierRecord:
		return "T2:Record"
	case TierAct:
		return "T3:Act"
	default:
		return "Unknown"
	}
}

// ToolClassification maps a tool name to its tier and policy key.
type ToolClassification struct {
	Tier        ActionTier
	PolicyKey   string // key used for IsT2Allowed/IsT3Allowed lookup
	Description string // human-readable description of what this tool can mutate
}

// toolRegistry is the hardcoded classification of every known tool.
// This is a compile-time constant — the LLM cannot change it.
var toolRegistry = map[string]ToolClassification{
	// === T1: Observe (read-only) ===

	// Local tools (joe binary)
	"read_file":        {Tier: TierObserve, Description: "Read file contents"},
	"local_git_status": {Tier: TierObserve, Description: "Show git working tree status"},
	"local_git_diff":   {Tier: TierObserve, Description: "Show git diff"},
	"ask_user":         {Tier: TierObserve, Description: "Ask user a question"},

	// Core tools (call joecored API — query only)
	"list_sources":  {Tier: TierObserve, Description: "List registered sources"},
	"graph_query":   {Tier: TierObserve, Description: "Query the knowledge graph"},
	"graph_related": {Tier: TierObserve, Description: "Find related graph nodes"},
	"k8s_get":       {Tier: TierObserve, Description: "Get Kubernetes resource"},
	"k8s_logs":      {Tier: TierObserve, Description: "Get Kubernetes pod logs"},
	"git_read":      {Tier: TierObserve, Description: "Read file from git repo"},
	"git_log":       {Tier: TierObserve, Description: "Show git commit log"},
	"git_diff":      {Tier: TierObserve, Description: "Show git diff between commits"},
	"aws_ec2":       {Tier: TierObserve, Description: "Describe AWS EC2 instances"},
	"aws_eks":       {Tier: TierObserve, Description: "Describe AWS EKS clusters"},
	"aws_rds":       {Tier: TierObserve, Description: "Describe AWS RDS instances"},
	"aws_vpc":       {Tier: TierObserve, Description: "Describe AWS VPC resources"},

	// Observability tools (Phase 6.3) — read-only queries
	"prometheus_query": {Tier: TierObserve, Description: "Query Prometheus/Mimir metrics via PromQL"},
	"loki_query":       {Tier: TierObserve, Description: "Query Loki logs via LogQL"},
	"tempo_search":     {Tier: TierObserve, Description: "Search traces in Tempo"},
	"jaeger_traces":    {Tier: TierObserve, Description: "Query traces from Jaeger"},

	// Alerting and dashboard tools (Phase 6.4) — read-only queries
	"alertmanager_alerts": {Tier: TierObserve, Description: "List active Alertmanager alerts"},
	"pagerduty_incidents": {Tier: TierObserve, Description: "List PagerDuty incidents"},
	"grafana_dashboards":  {Tier: TierObserve, Description: "List Grafana dashboards and alerts"},

	// Shared diagnostic tools (Phase 6.6) — Go-native, no CLI deps, T1 read-only
	"tcp_connect":  {Tier: TierObserve, Description: "Check TCP connectivity to a host:port"},
	"port_scan":    {Tier: TierObserve, Description: "Scan multiple ports on a host"},
	"dns_lookup":   {Tier: TierObserve, Description: "Resolve DNS records for a hostname"},
	"http_request": {Tier: TierObserve, Description: "Probe an HTTP/HTTPS endpoint"},
	"system_info":  {Tier: TierObserve, Description: "Return system stats: disk, memory, load, OS"},
	"trace_route":  {Tier: TierObserve, Description: "Trace network path to a host (hop-by-hop)"},

	// Data store tools (Phase 6.7) — read-only diagnostic queries, T1
	"postgres_stat":         {Tier: TierObserve, Description: "Query PostgreSQL activity, table stats, replication lag"},
	"postgres_query":        {Tier: TierObserve, Description: "Run a SELECT-only diagnostic query against PostgreSQL"},
	"mysql_stat":            {Tier: TierObserve, Description: "Query MySQL process list and replica status"},
	"mysql_query":           {Tier: TierObserve, Description: "Run a SELECT-only diagnostic query against MySQL"},
	"redis_info":            {Tier: TierObserve, Description: "Query Redis INFO stats by section"},
	"redis_slowlog":         {Tier: TierObserve, Description: "Retrieve recent slow Redis commands"},
	"mongodb_stat":          {Tier: TierObserve, Description: "Query MongoDB server status, replica set health, current ops"},
	"kafka_topics":          {Tier: TierObserve, Description: "List Kafka topics with partition info"},
	"kafka_brokers":         {Tier: TierObserve, Description: "List Kafka brokers and cluster metadata"},
	"kafka_consumers":       {Tier: TierObserve, Description: "List Kafka consumer groups and lag"},
	"elasticsearch_health":  {Tier: TierObserve, Description: "Query Elasticsearch cluster health and node stats"},
	"elasticsearch_indices": {Tier: TierObserve, Description: "List Elasticsearch indices with doc count and health"},

	// Networking & Ingress tools (Phase 6.9) — read-only queries, T1
	"nginx_ingresses":  {Tier: TierObserve, Description: "List Kubernetes Ingress resources from NGINX Ingress Controller"},
	"nginx_status":     {Tier: TierObserve, Description: "Get NGINX connection statistics from status endpoint"},
	"nginx_config":     {Tier: TierObserve, Description: "List NGINX controller ConfigMaps"},
	"envoy_clusters":   {Tier: TierObserve, Description: "List Envoy upstream clusters with health status"},
	"envoy_config":     {Tier: TierObserve, Description: "Dump Envoy configuration sections"},
	"envoy_stats":      {Tier: TierObserve, Description: "Get Envoy statistics filtered by prefix"},
	"istio_config":     {Tier: TierObserve, Description: "List Istio service mesh CRDs from a Kubernetes source"},
	"istio_resource":   {Tier: TierObserve, Description: "Get details for a specific Istio resource"},
	"cilium_policies":  {Tier: TierObserve, Description: "List Cilium network policies"},
	"cilium_endpoints": {Tier: TierObserve, Description: "List Cilium endpoints with identity and health"},

	// Security & runtime tools (Phase 6.11) — read-only queries, T1
	"falco_alerts": {Tier: TierObserve, Description: "List recent Falco runtime security events"},
	"falco_rules":  {Tier: TierObserve, Description: "List Falco rules observed in recent events"},

	// Proprietary observability vendor tools (Phase 6, Step 12) — read-only queries, T1
	"datadog_query":   {Tier: TierObserve, Description: "Query Datadog metrics and log events"},
	"splunk_query":    {Tier: TierObserve, Description: "Search Splunk logs using SPL"},
	"dynatrace_query": {Tier: TierObserve, Description: "Query Dynatrace metrics and events"},
	"newrelic_query":  {Tier: TierObserve, Description: "Execute New Relic NRQL queries"},

	// K8s CRD-based tools (Phase 6.10) — read-only queries, T1
	"certmanager_certs":    {Tier: TierObserve, Description: "List cert-manager Certificate resources with expiry and readiness"},
	"certmanager_issuers":  {Tier: TierObserve, Description: "List cert-manager Issuer and ClusterIssuer resources with status"},
	"keda_scaledobjects":   {Tier: TierObserve, Description: "List KEDA ScaledObject and ScaledJob resources with scaling config"},
	"opa_constraints":      {Tier: TierObserve, Description: "List OPA/Gatekeeper ConstraintTemplates and constraint instances with violation counts"},
	"opa_violations":       {Tier: TierObserve, Description: "Get violation details for a specific OPA/Gatekeeper constraint"},
	"crossplane_providers": {Tier: TierObserve, Description: "List Crossplane Provider resources with health status"},
	"crossplane_resources": {Tier: TierObserve, Description: "List Crossplane CompositeResourceDefinitions and Compositions"},

	// GitOps, CD & IaC tools (Phase 6.8) — read-only queries, T1
	"argocd_apps":        {Tier: TierObserve, Description: "List Argo CD applications with sync and health status"},
	"argocd_app":         {Tier: TierObserve, Description: "Get details for one Argo CD application"},
	"argocd_diff":        {Tier: TierObserve, Description: "Get sync diff status for an Argo CD application"},
	"argocd_history":     {Tier: TierObserve, Description: "Get sync operation history for an Argo CD application"},
	"flux_status":        {Tier: TierObserve, Description: "List all Flux CD resources with reconciliation status"},
	"flux_resource":      {Tier: TierObserve, Description: "Get details for a specific Flux CD resource"},
	"terraform_state":    {Tier: TierObserve, Description: "List managed resources from Terraform state"},
	"terraform_resource": {Tier: TierObserve, Description: "Get details for a specific Terraform resource"},
	"terraform_outputs":  {Tier: TierObserve, Description: "List output values from Terraform state"},
	"helm_releases":      {Tier: TierObserve, Description: "List Helm releases with status and chart version"},
	"helm_release":       {Tier: TierObserve, Description: "Get full details for a specific Helm release"},
	"helm_history":       {Tier: TierObserve, Description: "Get revision history for a Helm release"},

	// Knowledge store drift detection (Phase 8) — read-only, T1
	"detect_doc_drift": {Tier: TierObserve, Description: "Detect documentation drift between knowledge store and external sources"},

	// === T2: Record (internal state mutations) ===

	// Core Agent tools (joecored — graph/fact mutations)
	"graph_add_node":       {Tier: TierRecord, PolicyKey: "graph_mutations", Description: "Add node to knowledge graph"},
	"graph_add_edge":       {Tier: TierRecord, PolicyKey: "graph_mutations", Description: "Add edge to knowledge graph"},
	"graph_update_node":    {Tier: TierRecord, PolicyKey: "graph_mutations", Description: "Update node in knowledge graph"},
	"register_source":      {Tier: TierRecord, PolicyKey: "source_registration", Description: "Register infrastructure source"},
	"save_onboarding_fact": {Tier: TierRecord, PolicyKey: "onboarding_facts", Description: "Save onboarding fact"},

	// Phase 8: doc draft generation (creates proposal in internal state, not external)
	"generate_doc_draft": {Tier: TierRecord, PolicyKey: "draft_generation", Description: "Generate a documentation draft proposal from knowledge store"},

	// === T3: Act (external system mutations) ===

	"write_file":  {Tier: TierAct, PolicyKey: "write_file", Description: "Write file to local filesystem"},
	"run_command": {Tier: TierAct, PolicyKey: "run_command", Description: "Run shell command"},

	// Phase 8: doc publish (writes to external systems)
	"publish_doc_update_confluence": {Tier: TierAct, PolicyKey: "confluence_publish", Description: "Publish doc proposal to Confluence page"},
	"publish_doc_update_notion":     {Tier: TierAct, PolicyKey: "notion_publish", Description: "Publish doc proposal to Notion page"},
	"publish_doc_update_git":        {Tier: TierAct, PolicyKey: "git_push", Description: "Commit and push doc proposal to Git repo"},
	// publish_doc_update selects the runtime-specific policy key per target type.
	"publish_doc_update": {Tier: TierAct, PolicyKey: "confluence_publish", Description: "Publish an approved doc proposal to its target system"},
}

// ClassifyTool returns the classification for a tool by name.
// Unknown tools are classified as TierAct (deny by default) for safety.
func ClassifyTool(name string) ToolClassification {
	if c, ok := toolRegistry[name]; ok {
		return c
	}
	return ToolClassification{
		Tier:        TierAct,
		PolicyKey:   "",
		Description: "Unknown tool (denied by default)",
	}
}

// CheckAccess verifies whether a tool is allowed to execute under the given policy.
// Returns nil if allowed, or an error describing why the action was denied.
func CheckAccess(toolName string, policy *SafetyPolicy) error {
	c := ClassifyTool(toolName)

	switch c.Tier {
	case TierObserve:
		return nil // always allowed

	case TierRecord:
		if policy.IsT2Allowed(c.PolicyKey) {
			return nil
		}
		return &AccessDeniedError{
			ToolName: toolName,
			Tier:     c.Tier,
			Reason:   "T2 action '" + c.PolicyKey + "' is disabled in safety policy",
		}

	case TierAct:
		if c.PolicyKey != "" && policy.IsT3Allowed(c.PolicyKey) {
			return nil
		}
		reason := "T3 action is denied by default"
		if c.PolicyKey != "" {
			reason = "T3 action '" + c.PolicyKey + "' is disabled in safety policy"
		}
		return &AccessDeniedError{
			ToolName: toolName,
			Tier:     c.Tier,
			Reason:   reason,
		}

	default:
		return &AccessDeniedError{
			ToolName: toolName,
			Tier:     c.Tier,
			Reason:   "unknown tier classification",
		}
	}
}

// AccessDeniedError is returned when a tool execution is blocked by safety policy.
type AccessDeniedError struct {
	ToolName string
	Tier     ActionTier
	Reason   string
}

func (e *AccessDeniedError) Error() string {
	return "safety: access denied for tool '" + e.ToolName + "' (" + e.Tier.String() + "): " + e.Reason
}
