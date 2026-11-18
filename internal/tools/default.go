package tools

import (
	"github.com/jaimegago/joe/internal/safety"
	"github.com/jaimegago/joe/internal/search"
	coretools "github.com/jaimegago/joe/internal/tools/core"
	"github.com/jaimegago/joe/internal/tools/shared/dnsquery"
	"github.com/jaimegago/joe/internal/tools/shared/httpreq"
	"github.com/jaimegago/joe/internal/tools/shared/netcheck"
	"github.com/jaimegago/joe/internal/tools/shared/traceroute"
	"github.com/jaimegago/joe/internal/tools/shared/websearch"
)

// registerSharedTools registers shared diagnostic tools (Read-class, Go-native,
// no CLI deps). These run in-process and work from both joe (user's machine
// perspective) and joecored (cluster/server perspective).
//
// searchProvider is the boot-resolved web-search backend threaded in from
// Services; it may be nil (web search unconfigured), in which case web_search is
// still registered and advertised but returns a no-backend-configured
// tool-error when called (exposed-and-deny). This registration path backs the
// user task loop only; the autonomous agent:core registry
// (internal/coreagent) registers its own tools and does NOT include web_search.
func registerSharedTools(registry *Registry, searchProvider search.Provider) {
	registry.Register(netcheck.NewTCPConnectTool())
	registry.Register(netcheck.NewPortScanTool())
	registry.Register(dnsquery.NewDNSLookupTool())
	registry.Register(httpreq.NewHTTPRequestTool())
	registry.Register(traceroute.NewTraceRouteTool())
	registry.Register(websearch.NewWebSearchTool(searchProvider))
}

// NewCoreRegistry creates a registry with shared diagnostic tools and core
// tools. It omits local tools (read_file, write_file, run_command, git tools,
// askuser) since those only make sense on the user's local machine. This is
// used by the task execution endpoint on joe.
//
// After Identity Phase E (docs/reference/joe-identity-design.md §3), the value passed in
// is an in-process accessor-backed client constructed by joe (see
// internal/api/inproc_client.go) — there is no loopback HTTP self-call. The
// argument type is coretools.CoreToolsClient (the aggregate of each tool's
// small *Client interface), so the HTTP *client.Client also satisfies it for
// external test harnesses (e.g. test/e2e).
//
// searchProvider is the boot-resolved web-search backend (nil when web search
// is unconfigured) threaded into the web_search shared tool. This is the
// user-task-loop registration path only.
func NewCoreRegistry(coreClient coretools.CoreToolsClient, policy *safety.SafetyPolicy, searchProvider search.Provider) *Registry {
	registry := NewRegistry()

	// Shared diagnostic tools (Read-class, Go-native, no CLI deps).
	registerSharedTools(registry, searchProvider)

	// Core tools.
	registerCoreTools(registry, coreClient)

	return registry
}

// registerCoreTools registers all core tools that communicate with joecored.
//
// Every tool that reaches a managed system is registered through act, and the
// four that read only Joe's own stores are registered bare. The split is
// pinned by TestCoreRegistry_ActDeclarationCoversManagedSystemTools, which
// derives the bare set from this function's four names and refuses a fifth.
func registerCoreTools(registry *Registry, coreClient coretools.CoreToolsClient) {
	// act registers a tool whose calls reach a managed system through the
	// guarded accessor. Each such call leaves an audit entry, and the act
	// declaration is how the model states its reason and trigger with the
	// call (see WithActDeclaration).
	act := func(t Tool) { registry.Register(WithActDeclaration(t)) }

	// Joe's own stores: the component registry and the infrastructure graph.
	// These calls reach no managed system and carry no act declaration.
	registry.Register(coretools.NewListComponentsTool(coreClient))
	registry.Register(coretools.NewResolveComponentTool(coreClient))
	registry.Register(coretools.NewGraphQueryTool(coreClient))
	registry.Register(coretools.NewGraphRelatedTool(coreClient))

	// Managed systems.
	act(coretools.NewK8sGetTool(coreClient))
	act(coretools.NewK8sLogsTool(coreClient))
	act(coretools.NewGitReadTool(coreClient))
	act(coretools.NewGitLogTool(coreClient))
	act(coretools.NewGitDiffTool(coreClient))
	act(coretools.NewRepoSearchTool(coreClient))
	act(coretools.NewAWSEC2Tool(coreClient))
	act(coretools.NewAWSEKSTool(coreClient))
	act(coretools.NewAWSRDSTool(coreClient))
	act(coretools.NewAWSVPCTool(coreClient))

	// Observability tools
	act(coretools.NewPrometheusQueryTool(coreClient))
	act(coretools.NewLokiQueryTool(coreClient))
	act(coretools.NewTempoSearchTool(coreClient))
	act(coretools.NewJaegerTracesTool(coreClient))

	// Alerting tools
	act(coretools.NewAlertmanagerAlertsTool(coreClient))
	act(coretools.NewPagerDutyIncidentsTool(coreClient))
	act(coretools.NewGrafanaDashboardsTool(coreClient))

	// Data store tools
	act(coretools.NewPostgresStatTool(coreClient))
	act(coretools.NewPostgresQueryTool(coreClient))
	act(coretools.NewMySQLStatTool(coreClient))
	act(coretools.NewMySQLQueryTool(coreClient))
	act(coretools.NewRedisInfoTool(coreClient))
	act(coretools.NewRedisSlowLogTool(coreClient))
	act(coretools.NewMongoDBStatTool(coreClient))
	act(coretools.NewKafkaTopicsTool(coreClient))
	act(coretools.NewKafkaBrokersTool(coreClient))
	act(coretools.NewKafkaConsumerGroupsTool(coreClient))
	act(coretools.NewElasticsearchHealthTool(coreClient))
	act(coretools.NewElasticsearchIndicesTool(coreClient))

	// GitOps, CD & IaC tools
	act(coretools.NewArgoCDAppsTool(coreClient))
	act(coretools.NewArgoCDGetAppTool(coreClient))
	act(coretools.NewArgoCDDiffTool(coreClient))
	act(coretools.NewArgoCDHistoryTool(coreClient))
	act(coretools.NewFluxStatusTool(coreClient))
	act(coretools.NewFluxResourceTool(coreClient))
	act(coretools.NewTerraformStateTool(coreClient))
	act(coretools.NewTerraformResourceTool(coreClient))
	act(coretools.NewTerraformOutputsTool(coreClient))
	act(coretools.NewHelmReleasesTool(coreClient))
	act(coretools.NewHelmGetReleaseTool(coreClient))
	act(coretools.NewHelmHistoryTool(coreClient))

	// Networking & Ingress tools
	act(coretools.NewNginxIngressesTool(coreClient))
	act(coretools.NewNginxStatusTool(coreClient))
	act(coretools.NewNginxConfigTool(coreClient))
	act(coretools.NewEnvoyClustersTool(coreClient))
	act(coretools.NewEnvoyConfigTool(coreClient))
	act(coretools.NewEnvoyStatsTool(coreClient))
	act(coretools.NewIstioConfigTool(coreClient))
	act(coretools.NewIstioResourceTool(coreClient))
	act(coretools.NewCiliumPoliciesTool(coreClient))
	act(coretools.NewCiliumEndpointsTool(coreClient))

	// K8s CRD-based tools
	act(coretools.NewCertManagerCertsTool(coreClient))
	act(coretools.NewCertManagerIssuersTool(coreClient))
	act(coretools.NewKEDAScaledObjectsTool(coreClient))
	act(coretools.NewOPAConstraintsTool(coreClient))
	act(coretools.NewOPAViolationsTool(coreClient))
	act(coretools.NewCrossplaneProvidersTool(coreClient))
	act(coretools.NewCrossplaneResourcesTool(coreClient))

	// Security & runtime tools
	act(coretools.NewFalcoAlertsTool(coreClient))
	act(coretools.NewFalcoRulesTool(coreClient))

	// Artifact registry tools
	act(coretools.NewRegistryQueryTool(coreClient))
	act(coretools.NewArtifactoryQueryTool(coreClient))
	act(coretools.NewECRQueryTool(coreClient))

	// Code review tools
	act(coretools.NewGitHubPRGetTool(coreClient))
	act(coretools.NewGitHubPRDiffTool(coreClient))
	act(coretools.NewGitHubCommentTool(coreClient))
	act(coretools.NewGitHubRequestChangesTool(coreClient))
	act(coretools.NewGitLabMRGetTool(coreClient))
	act(coretools.NewGitLabMRDiffTool(coreClient))
	act(coretools.NewGitLabCommentTool(coreClient))
}
