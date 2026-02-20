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
	"echo":             {Tier: TierObserve, Description: "Echo input back"},
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

	// === T2: Record (internal state mutations) ===

	// Core Agent tools (joecored — graph/fact mutations)
	"graph_add_node":       {Tier: TierRecord, PolicyKey: "graph_mutations", Description: "Add node to knowledge graph"},
	"graph_add_edge":       {Tier: TierRecord, PolicyKey: "graph_mutations", Description: "Add edge to knowledge graph"},
	"graph_update_node":    {Tier: TierRecord, PolicyKey: "graph_mutations", Description: "Update node in knowledge graph"},
	"register_source":      {Tier: TierRecord, PolicyKey: "source_registration", Description: "Register infrastructure source"},
	"save_onboarding_fact": {Tier: TierRecord, PolicyKey: "onboarding_facts", Description: "Save onboarding fact"},

	// === T3: Act (external system mutations) ===

	"write_file":  {Tier: TierAct, PolicyKey: "write_file", Description: "Write file to local filesystem"},
	"run_command": {Tier: TierAct, PolicyKey: "run_command", Description: "Run shell command"},
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
