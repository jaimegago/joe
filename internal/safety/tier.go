package safety

// ActionClass is the binary classification of what a tool does to the MANAGED
// SYSTEM (live infrastructure + the code/config that governs it — the sources).
// It answers exactly one decidable question: does this operation mutate the
// managed system?
//
// Per D-0018/D-0019 this replaces the former three-tier scheme
// (Observe/Record/Act). Severity-of-mutation is deliberately NOT encoded as a
// classification tier — a static blast-radius taxonomy is hard to get right and
// hard to evaluate on a non-deterministic LLM. Blast-radius safety lives in
// tools, skills, OASIS testing, and the per-zone/per-capability graduation
// ladder; this classification carries only the action axis.
type ActionClass int

const (
	// ActionRead does NOT mutate the managed system. Always allowed, no policy
	// check needed. Reads include source queries, Joe's own graph/model
	// maintenance, and notifications to humans (D-0018 write definition).
	// (Former TierObserve / T1.)
	ActionRead ActionClass = iota + 1

	// ActionMutate mutates the managed system (files, infrastructure,
	// deployments, external PR/MR threads). Denied by default; requires
	// per-action opt-in in the act policy. (Former TierAct / T3.)
	ActionMutate
)

// String returns the human-readable class label.
func (c ActionClass) String() string {
	switch c {
	case ActionRead:
		return "read"
	case ActionMutate:
		return "mutate"
	default:
		return "unknown"
	}
}

// ToolClassification maps a tool name to its action class and policy key.
type ToolClassification struct {
	Class       ActionClass
	PolicyKey   string // key used for IsT3Allowed lookup (mutating tools)
	Description string // human-readable description of what this tool can mutate

	// NeedsDurability declares that this tool performs a non-idempotent
	// operation — a create/append with a server-generated identity (random
	// ID, autoincrement, timestamp-keyed) and no natural idempotency — for
	// which an in-run retry or crash-resume would produce a DUPLICATE. When
	// set, the DurableExecutor persists an idempotency key (§D5
	// RecordToolIntent → execute → MarkToolCompleted) and short-circuits a
	// same-key replay so the operation runs at most once per run.
	//
	// This is INDEPENDENT of the Read/Mutate action class (D-0020 follow-up):
	// "does this need crash-resume" is a different question than "does this
	// mutate the managed system". Durability is opt-in per tool and defaults
	// to OFF — applying it to naturally re-runnable reads costs two fsyncs and
	// an unbounded result-bearing row per call and risks serving a stale
	// same-key result. Only the few genuinely non-idempotent operations set
	// this; an undeclared tool is never wrapped.
	//
	// SCOPE: per-run replay-safety only (dedup is keyed by runID + tool +
	// args-hash). It does NOT provide cross-run uniqueness — see DECISIONS
	// D-0020 follow-up for the still-open get-or-create work.
	NeedsDurability bool
}

// toolRegistry is the hardcoded classification of every known tool.
// This is a compile-time constant — the LLM cannot change it.
var toolRegistry = map[string]ToolClassification{
	// === Read (does not mutate the managed system) ===

	// Local tools (joe binary)
	"read_file":        {Class: ActionRead, Description: "Read file contents"},
	"local_git_status": {Class: ActionRead, Description: "Show git working tree status"},
	"local_git_diff":   {Class: ActionRead, Description: "Show git diff"},
	"ask_user":         {Class: ActionRead, Description: "Ask user a question"},

	// Core tools (call joecored API — query only)
	"list_sources":  {Class: ActionRead, Description: "List registered sources"},
	"graph_query":   {Class: ActionRead, Description: "Query the knowledge graph"},
	"graph_related": {Class: ActionRead, Description: "Find related graph nodes"},
	"k8s_get":       {Class: ActionRead, Description: "Get Kubernetes resource"},
	"k8s_logs":      {Class: ActionRead, Description: "Get Kubernetes pod logs"},
	"git_read":      {Class: ActionRead, Description: "Read file from git repo"},
	"git_log":       {Class: ActionRead, Description: "Show git commit log"},
	"git_diff":      {Class: ActionRead, Description: "Show git diff between commits"},
	"aws_ec2":       {Class: ActionRead, Description: "Describe AWS EC2 instances"},
	"aws_eks":       {Class: ActionRead, Description: "Describe AWS EKS clusters"},
	"aws_rds":       {Class: ActionRead, Description: "Describe AWS RDS instances"},
	"aws_vpc":       {Class: ActionRead, Description: "Describe AWS VPC resources"},

	// Observability tools (Phase 6.3) — read-only queries
	"prometheus_query": {Class: ActionRead, Description: "Query Prometheus/Mimir metrics via PromQL"},
	"loki_query":       {Class: ActionRead, Description: "Query Loki logs via LogQL"},
	"tempo_search":     {Class: ActionRead, Description: "Search traces in Tempo"},
	"jaeger_traces":    {Class: ActionRead, Description: "Query traces from Jaeger"},

	// Alerting and dashboard tools (Phase 6.4) — read-only queries
	"alertmanager_alerts": {Class: ActionRead, Description: "List active Alertmanager alerts"},
	"pagerduty_incidents": {Class: ActionRead, Description: "List PagerDuty incidents"},
	"grafana_dashboards":  {Class: ActionRead, Description: "List Grafana dashboards and alerts"},

	// Shared diagnostic tools (Phase 6.6) — Go-native, no CLI deps, read-only
	"tcp_connect":  {Class: ActionRead, Description: "Check TCP connectivity to a host:port"},
	"port_scan":    {Class: ActionRead, Description: "Scan multiple ports on a host"},
	"dns_lookup":   {Class: ActionRead, Description: "Resolve DNS records for a hostname"},
	"http_request": {Class: ActionRead, Description: "Probe an HTTP/HTTPS endpoint"},
	"system_info":  {Class: ActionRead, Description: "Return system stats: disk, memory, load, OS"},
	"trace_route":  {Class: ActionRead, Description: "Trace network path to a host (hop-by-hop)"},

	// Data store tools (Phase 6.7) — read-only diagnostic queries
	"postgres_stat":         {Class: ActionRead, Description: "Query PostgreSQL activity, table stats, replication lag"},
	"postgres_query":        {Class: ActionRead, Description: "Run a SELECT-only diagnostic query against PostgreSQL"},
	"mysql_stat":            {Class: ActionRead, Description: "Query MySQL process list and replica status"},
	"mysql_query":           {Class: ActionRead, Description: "Run a SELECT-only diagnostic query against MySQL"},
	"redis_info":            {Class: ActionRead, Description: "Query Redis INFO stats by section"},
	"redis_slowlog":         {Class: ActionRead, Description: "Retrieve recent slow Redis commands"},
	"mongodb_stat":          {Class: ActionRead, Description: "Query MongoDB server status, replica set health, current ops"},
	"kafka_topics":          {Class: ActionRead, Description: "List Kafka topics with partition info"},
	"kafka_brokers":         {Class: ActionRead, Description: "List Kafka brokers and cluster metadata"},
	"kafka_consumers":       {Class: ActionRead, Description: "List Kafka consumer groups and lag"},
	"elasticsearch_health":  {Class: ActionRead, Description: "Query Elasticsearch cluster health and node stats"},
	"elasticsearch_indices": {Class: ActionRead, Description: "List Elasticsearch indices with doc count and health"},

	// Networking & Ingress tools (Phase 6.9) — read-only queries
	"nginx_ingresses":  {Class: ActionRead, Description: "List Kubernetes Ingress resources from NGINX Ingress Controller"},
	"nginx_status":     {Class: ActionRead, Description: "Get NGINX connection statistics from status endpoint"},
	"nginx_config":     {Class: ActionRead, Description: "List NGINX controller ConfigMaps"},
	"envoy_clusters":   {Class: ActionRead, Description: "List Envoy upstream clusters with health status"},
	"envoy_config":     {Class: ActionRead, Description: "Dump Envoy configuration sections"},
	"envoy_stats":      {Class: ActionRead, Description: "Get Envoy statistics filtered by prefix"},
	"istio_config":     {Class: ActionRead, Description: "List Istio service mesh CRDs from a Kubernetes source"},
	"istio_resource":   {Class: ActionRead, Description: "Get details for a specific Istio resource"},
	"cilium_policies":  {Class: ActionRead, Description: "List Cilium network policies"},
	"cilium_endpoints": {Class: ActionRead, Description: "List Cilium endpoints with identity and health"},

	// Security & runtime tools (Phase 6.11) — read-only queries
	"falco_alerts": {Class: ActionRead, Description: "List recent Falco runtime security events"},
	"falco_rules":  {Class: ActionRead, Description: "List Falco rules observed in recent events"},

	// Proprietary observability vendor tools (Phase 6, Step 12) — read-only queries
	"datadog_query":   {Class: ActionRead, Description: "Query Datadog metrics and log events"},
	"splunk_query":    {Class: ActionRead, Description: "Search Splunk logs using SPL"},
	"dynatrace_query": {Class: ActionRead, Description: "Query Dynatrace metrics and events"},
	"newrelic_query":  {Class: ActionRead, Description: "Execute New Relic NRQL queries"},

	// Container registry tools — read-only metadata queries
	"registry_query":    {Class: ActionRead, Description: "Query an OCI-compatible container registry"},
	"artifactory_query": {Class: ActionRead, Description: "Query a JFrog Artifactory instance"},
	"ecr_query":         {Class: ActionRead, Description: "Query AWS Elastic Container Registry (ECR)"},

	// K8s CRD-based tools (Phase 6.10) — read-only queries
	"certmanager_certs":    {Class: ActionRead, Description: "List cert-manager Certificate resources with expiry and readiness"},
	"certmanager_issuers":  {Class: ActionRead, Description: "List cert-manager Issuer and ClusterIssuer resources with status"},
	"keda_scaledobjects":   {Class: ActionRead, Description: "List KEDA ScaledObject and ScaledJob resources with scaling config"},
	"opa_constraints":      {Class: ActionRead, Description: "List OPA/Gatekeeper ConstraintTemplates and constraint instances with violation counts"},
	"opa_violations":       {Class: ActionRead, Description: "Get violation details for a specific OPA/Gatekeeper constraint"},
	"crossplane_providers": {Class: ActionRead, Description: "List Crossplane Provider resources with health status"},
	"crossplane_resources": {Class: ActionRead, Description: "List Crossplane CompositeResourceDefinitions and Compositions"},

	// GitOps, CD & IaC tools (Phase 6.8) — read-only queries
	"argocd_apps":        {Class: ActionRead, Description: "List Argo CD applications with sync and health status"},
	"argocd_app":         {Class: ActionRead, Description: "Get details for one Argo CD application"},
	"argocd_diff":        {Class: ActionRead, Description: "Get sync diff status for an Argo CD application"},
	"argocd_history":     {Class: ActionRead, Description: "Get sync operation history for an Argo CD application"},
	"flux_status":        {Class: ActionRead, Description: "List all Flux CD resources with reconciliation status"},
	"flux_resource":      {Class: ActionRead, Description: "Get details for a specific Flux CD resource"},
	"terraform_state":    {Class: ActionRead, Description: "List managed resources from Terraform state"},
	"terraform_resource": {Class: ActionRead, Description: "Get details for a specific Terraform resource"},
	"terraform_outputs":  {Class: ActionRead, Description: "List output values from Terraform state"},
	"helm_releases":      {Class: ActionRead, Description: "List Helm releases with status and chart version"},
	"helm_release":       {Class: ActionRead, Description: "Get full details for a specific Helm release"},
	"helm_history":       {Class: ActionRead, Description: "Get revision history for a Helm release"},

	// Knowledge store drift detection (Phase 8) — read-only
	"detect_doc_drift": {Class: ActionRead, Description: "Detect documentation drift between knowledge store and external sources"},

	// === Read — Joe's own model maintenance ===
	//
	// Per D-0018/D-0019, a "write" is an operation that mutates the *managed
	// system* (live infrastructure + the code/config that governs it). These
	// tools only record observed state into Joe's own graph/store/knowledge —
	// the managed system is in the same state after they run — so by the
	// write definition they are reads, not writes. Keeping them read-class
	// also means Joe never freezes its own model in safe mode or while an
	// incident captain gate is engaged.
	// graph_add_*/update are UPSERTs keyed on caller-supplied args (node_id,
	// or the (from,to,relation) edge key) — naturally idempotent on retry, so
	// no durability needed.
	"graph_add_node":    {Class: ActionRead, Description: "Add node to Joe's knowledge graph"},
	"graph_add_edge":    {Class: ActionRead, Description: "Add edge to Joe's knowledge graph"},
	"graph_update_node": {Class: ActionRead, Description: "Update node in Joe's knowledge graph"},
	// register_source / save_onboarding_fact / save_knowledge_entry are
	// plain INSERTs whose row identity is generated server-side OUTSIDE the
	// args (register_source: crypto-random ID; save_onboarding_fact:
	// autoincrement; save_knowledge_entry: uid.New()), with no natural unique
	// key — an in-run retry or crash-resume would create a second row. They
	// declare NeedsDurability so the §D5 key dedups them per run (D-0020).
	"register_source":      {Class: ActionRead, Description: "Record an infrastructure source in Joe's store", NeedsDurability: true},
	"save_onboarding_fact": {Class: ActionRead, Description: "Save an onboarding fact to Joe's store", NeedsDurability: true},
	"save_knowledge_entry": {Class: ActionRead, Description: "Save a derived knowledge entry to Joe's knowledge store", NeedsDurability: true},

	// Phase 8: doc draft generation — creates a proposal in Joe's own state.
	// The proposal must be human-approved before publish_doc_update (mutate)
	// can push it to any external system, so drafting itself mutates nothing
	// outside Joe. The proposal ID is uid.New() (server-side, outside args)
	// and the insert has no natural unique key, so a retry would create a
	// second proposal — declares NeedsDurability (D-0020).
	"generate_doc_draft": {Class: ActionRead, Description: "Generate a documentation draft proposal in Joe's store", NeedsDurability: true},

	// === Mutate (managed-system mutations) ===

	// write_file overwrites a path (the path is the natural key — rewriting it
	// is idempotent); run_command runs an arbitrary command that creates no
	// Joe-side record and whose result must NOT be served from a stale cache on
	// re-run (replay-safety of its side effects is the command's own concern).
	// Neither declares NeedsDurability — they are no longer wrapped just for
	// being Mutate (D-0020).
	"write_file":  {Class: ActionMutate, PolicyKey: "write_file", Description: "Write file to local filesystem"},
	"run_command": {Class: ActionMutate, PolicyKey: "run_command", Description: "Run shell command"},

	// Phase 8: doc publish (writes to external systems). publish_doc_update*
	// is guarded at the data layer: PublishProposal requires status==approved
	// and flips it to published, so a re-publish of the same proposal fails
	// closed rather than duplicating — a natural idempotency key. No
	// NeedsDurability (D-0020).
	"publish_doc_update_confluence": {Class: ActionMutate, PolicyKey: "confluence_publish", Description: "Publish doc proposal to Confluence page"},
	"publish_doc_update_notion":     {Class: ActionMutate, PolicyKey: "notion_publish", Description: "Publish doc proposal to Notion page"},
	"publish_doc_update_git":        {Class: ActionMutate, PolicyKey: "git_push", Description: "Commit and push doc proposal to Git repo"},
	// publish_doc_update selects the runtime-specific policy key per target type.
	"publish_doc_update": {Class: ActionMutate, PolicyKey: "confluence_publish", Description: "Publish an approved doc proposal to its target system"},

	// === Phase 10: Code Review Integration ===

	// Read — read-only PR/MR data
	"github_pr_get":   {Class: ActionRead, Description: "Get GitHub pull request metadata"},
	"github_pr_diff":  {Class: ActionRead, Description: "Get GitHub pull request unified diff"},
	"github_list_prs": {Class: ActionRead, Description: "List GitHub pull requests for a repository"},
	"gitlab_mr_get":   {Class: ActionRead, Description: "Get GitLab merge request metadata"},
	"gitlab_mr_diff":  {Class: ActionRead, Description: "Get GitLab merge request unified diff"},
	"gitlab_list_mrs": {Class: ActionRead, Description: "List GitLab merge requests for a project"},

	// Mutate — posting a comment/note mutates an external system (the
	// GitHub/GitLab PR thread). It persists outside Joe and is not idempotent
	// on retry (each post appends a new comment with a server-assigned ID, no
	// natural idempotency), so a retry/crash-resume would double-post. They
	// declare NeedsDurability so the §D5 key dedups them per run — this
	// preserves the protection the old Mutate-always-wrapped scheme gave them
	// (D-0020).
	"github_comment": {Class: ActionMutate, PolicyKey: "github_comment", Description: "Post a review comment on a GitHub pull request", NeedsDurability: true},
	"gitlab_comment": {Class: ActionMutate, PolicyKey: "gitlab_comment", Description: "Post a note on a GitLab merge request", NeedsDurability: true},

	// Mutate — requests changes (gates merging, high-impact external action).
	// Submitting a review is a non-idempotent append; keeps NeedsDurability so
	// a retry does not file a duplicate review (D-0020).
	"github_request_changes": {Class: ActionMutate, PolicyKey: "github_request_changes", Description: "Submit a GitHub review requesting changes on a pull request", NeedsDurability: true},
	// NOTE: github_approve (mutate) is intentionally not registered as a tool in this phase.
	// To enable, add PolicyKey "github_approve" to safety policy and register the tool manually.
}

// ClassifyTool returns the classification for a tool by name.
// Unknown tools are classified as ActionMutate (deny by default) for safety.
func ClassifyTool(name string) ToolClassification {
	if c, ok := toolRegistry[name]; ok {
		return c
	}
	return ToolClassification{
		Class:       ActionMutate,
		PolicyKey:   "",
		Description: "Unknown tool (denied by default)",
	}
}

// CheckAccess verifies whether a tool is allowed to execute under the given policy.
// Returns nil if allowed, or an error describing why the action was denied.
//
// Binary gate: reads are always allowed; mutations are denied by default and
// require a per-action opt-in via the act policy. (The former T2/Record band is
// gone — see D-0018/D-0019. The Record policy plumbing in policy.go is retained
// only as a backward-compat shim for existing safety-policy.yaml files.)
func CheckAccess(toolName string, policy *SafetyPolicy) error {
	c := ClassifyTool(toolName)

	switch c.Class {
	case ActionRead:
		return nil // always allowed

	case ActionMutate:
		if c.PolicyKey != "" && policy.IsT3Allowed(c.PolicyKey) {
			return nil
		}
		reason := "mutating action is denied by default"
		if c.PolicyKey != "" {
			reason = "mutating action '" + c.PolicyKey + "' is disabled in safety policy"
		}
		return &AccessDeniedError{
			ToolName: toolName,
			Class:    c.Class,
			Reason:   reason,
		}

	default:
		return &AccessDeniedError{
			ToolName: toolName,
			Class:    c.Class,
			Reason:   "unknown action classification",
		}
	}
}

// AccessDeniedError is returned when a tool execution is blocked by safety policy.
type AccessDeniedError struct {
	ToolName string
	Class    ActionClass
	Reason   string
}

func (e *AccessDeniedError) Error() string {
	return "safety: access denied for tool '" + e.ToolName + "' (" + e.Class.String() + "): " + e.Reason
}
