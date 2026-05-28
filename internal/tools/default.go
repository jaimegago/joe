package tools

import (
	"github.com/jaimegago/joe/internal/client"
	"github.com/jaimegago/joe/internal/safety"
	coretools "github.com/jaimegago/joe/internal/tools/core"
	"github.com/jaimegago/joe/internal/tools/local/askuser"
	"github.com/jaimegago/joe/internal/tools/local/gitdiff"
	"github.com/jaimegago/joe/internal/tools/local/gitstatus"
	"github.com/jaimegago/joe/internal/tools/local/readfile"
	"github.com/jaimegago/joe/internal/tools/local/runcmd"
	"github.com/jaimegago/joe/internal/tools/local/writefile"
	"github.com/jaimegago/joe/internal/tools/shared/dnsquery"
	"github.com/jaimegago/joe/internal/tools/shared/httpreq"
	"github.com/jaimegago/joe/internal/tools/shared/netcheck"
	"github.com/jaimegago/joe/internal/tools/shared/sysinfo"
	"github.com/jaimegago/joe/internal/tools/shared/traceroute"
)

// registerLocalTools registers the tools that only make sense on the user's
// local machine (filesystem, shell, local git, interactive prompt). If policy
// is non-nil, tool-specific settings (e.g., allowed_directories for write_file)
// are extracted from it.
func registerLocalTools(registry *Registry, policy *safety.SafetyPolicy) {
	// Basic / interactive tools
	registry.Register(askuser.NewTool())

	// File tools
	registry.Register(readfile.New())

	var writeAllowedDirs []string
	if policy != nil {
		writeAllowedDirs = policy.Act.WriteFile.AllowedDirectories
	}
	registry.Register(writefile.New(writeAllowedDirs...))

	// Git tools
	registry.Register(gitstatus.New())
	registry.Register(gitdiff.New())

	// Command runner with safe defaults. Only read-only commands are included.
	// Mutation-capable commands (kubectl, helm, argocd) are excluded — they
	// must be explicitly enabled in ~/.joe/safety-policy.yaml and are subject
	// to subcommand allowlists even when enabled.
	registry.Register(runcmd.New([]string{
		"ls", "cat", "head", "tail", "grep", "find", "wc",
	}))
}

// registerSharedTools registers shared diagnostic tools (T1, Go-native, no CLI
// deps). These run in-process and work from both joe (user's machine
// perspective) and joecored (cluster/server perspective).
func registerSharedTools(registry *Registry) {
	registry.Register(netcheck.NewTCPConnectTool())
	registry.Register(netcheck.NewPortScanTool())
	registry.Register(dnsquery.NewDNSLookupTool())
	registry.Register(httpreq.NewHTTPRequestTool())
	registry.Register(sysinfo.NewSystemInfoTool())
	registry.Register(traceroute.NewTraceRouteTool())
}

// NewDefaultRegistry creates a registry with all default tools registered
// (local + shared). If policy is non-nil, tool-specific settings are extracted
// from it. If nil, tools use permissive defaults.
func NewDefaultRegistry(policy *safety.SafetyPolicy) *Registry {
	registry := NewRegistry()
	registerLocalTools(registry, policy)
	registerSharedTools(registry)
	return registry
}

// NewLocalRegistry creates a registry with only the local-machine tools
// (read_file, write_file, run_command, local git, ask_user). After the Phase 2
// runtime collapse the CLI uses this set both to advertise its local tools to
// joe-core and to execute the delegated callbacks against the operator's own
// machine — shared and core tools run inside joe-core's loop instead.
func NewLocalRegistry(policy *safety.SafetyPolicy) *Registry {
	registry := NewRegistry()
	registerLocalTools(registry, policy)
	return registry
}

// NewCoreRegistry creates a registry with shared diagnostic tools and core
// tools that communicate with joecored via the HTTP client. Unlike
// NewDefaultRegistryWithClient, it omits local tools (read_file, write_file,
// run_command, git tools, askuser) since those only make sense on the user's
// local machine. This is used by the task execution endpoint on joe-core.
func NewCoreRegistry(coreClient *client.Client, policy *safety.SafetyPolicy) *Registry {
	registry := NewRegistry()

	// Shared diagnostic tools (T1, Go-native, no CLI deps).
	registerSharedTools(registry)

	// Core tools (same set as NewDefaultRegistryWithClient).
	registerCoreTools(registry, coreClient)

	return registry
}

// registerCoreTools registers all core tools that communicate with joecored via HTTP.
func registerCoreTools(registry *Registry, coreClient *client.Client) {
	registry.Register(coretools.NewListSourcesTool(coreClient))
	registry.Register(coretools.NewGraphQueryTool(coreClient))
	registry.Register(coretools.NewGraphRelatedTool(coreClient))
	registry.Register(coretools.NewK8sGetTool(coreClient))
	registry.Register(coretools.NewK8sLogsTool(coreClient))
	registry.Register(coretools.NewGitReadTool(coreClient))
	registry.Register(coretools.NewGitLogTool(coreClient))
	registry.Register(coretools.NewGitDiffTool(coreClient))
	registry.Register(coretools.NewAWSEC2Tool(coreClient))
	registry.Register(coretools.NewAWSEKSTool(coreClient))
	registry.Register(coretools.NewAWSRDSTool(coreClient))
	registry.Register(coretools.NewAWSVPCTool(coreClient))

	// Observability tools
	registry.Register(coretools.NewPrometheusQueryTool(coreClient))
	registry.Register(coretools.NewLokiQueryTool(coreClient))
	registry.Register(coretools.NewTempoSearchTool(coreClient))
	registry.Register(coretools.NewJaegerTracesTool(coreClient))

	// Alerting tools
	registry.Register(coretools.NewAlertmanagerAlertsTool(coreClient))
	registry.Register(coretools.NewPagerDutyIncidentsTool(coreClient))
	registry.Register(coretools.NewGrafanaDashboardsTool(coreClient))

	// Data store tools
	registry.Register(coretools.NewPostgresStatTool(coreClient))
	registry.Register(coretools.NewPostgresQueryTool(coreClient))
	registry.Register(coretools.NewMySQLStatTool(coreClient))
	registry.Register(coretools.NewMySQLQueryTool(coreClient))
	registry.Register(coretools.NewRedisInfoTool(coreClient))
	registry.Register(coretools.NewRedisSlowLogTool(coreClient))
	registry.Register(coretools.NewMongoDBStatTool(coreClient))
	registry.Register(coretools.NewKafkaTopicsTool(coreClient))
	registry.Register(coretools.NewKafkaBrokersTool(coreClient))
	registry.Register(coretools.NewKafkaConsumerGroupsTool(coreClient))
	registry.Register(coretools.NewElasticsearchHealthTool(coreClient))
	registry.Register(coretools.NewElasticsearchIndicesTool(coreClient))

	// GitOps, CD & IaC tools
	registry.Register(coretools.NewArgoCDAppsTool(coreClient))
	registry.Register(coretools.NewArgoCDGetAppTool(coreClient))
	registry.Register(coretools.NewArgoCDDiffTool(coreClient))
	registry.Register(coretools.NewArgoCDHistoryTool(coreClient))
	registry.Register(coretools.NewFluxStatusTool(coreClient))
	registry.Register(coretools.NewFluxResourceTool(coreClient))
	registry.Register(coretools.NewTerraformStateTool(coreClient))
	registry.Register(coretools.NewTerraformResourceTool(coreClient))
	registry.Register(coretools.NewTerraformOutputsTool(coreClient))
	registry.Register(coretools.NewHelmReleasesTool(coreClient))
	registry.Register(coretools.NewHelmGetReleaseTool(coreClient))
	registry.Register(coretools.NewHelmHistoryTool(coreClient))

	// Networking & Ingress tools
	registry.Register(coretools.NewNginxIngressesTool(coreClient))
	registry.Register(coretools.NewNginxStatusTool(coreClient))
	registry.Register(coretools.NewNginxConfigTool(coreClient))
	registry.Register(coretools.NewEnvoyClustersTool(coreClient))
	registry.Register(coretools.NewEnvoyConfigTool(coreClient))
	registry.Register(coretools.NewEnvoyStatsTool(coreClient))
	registry.Register(coretools.NewIstioConfigTool(coreClient))
	registry.Register(coretools.NewIstioResourceTool(coreClient))
	registry.Register(coretools.NewCiliumPoliciesTool(coreClient))
	registry.Register(coretools.NewCiliumEndpointsTool(coreClient))

	// K8s CRD-based tools
	registry.Register(coretools.NewCertManagerCertsTool(coreClient))
	registry.Register(coretools.NewCertManagerIssuersTool(coreClient))
	registry.Register(coretools.NewKEDAScaledObjectsTool(coreClient))
	registry.Register(coretools.NewOPAConstraintsTool(coreClient))
	registry.Register(coretools.NewOPAViolationsTool(coreClient))
	registry.Register(coretools.NewCrossplaneProvidersTool(coreClient))
	registry.Register(coretools.NewCrossplaneResourcesTool(coreClient))

	// Security & runtime tools
	registry.Register(coretools.NewFalcoAlertsTool(coreClient))
	registry.Register(coretools.NewFalcoRulesTool(coreClient))

	// Knowledge store tools
	registry.Register(coretools.NewSearchKnowledgeTool(coreClient))

	// Artifact registry tools
	registry.Register(coretools.NewRegistryQueryTool(coreClient))
	registry.Register(coretools.NewArtifactoryQueryTool(coreClient))
	registry.Register(coretools.NewECRQueryTool(coreClient))

	// Documentation co-pilot tools
	registry.Register(coretools.NewDetectDocDriftTool(coreClient))
	registry.Register(coretools.NewGenerateDocDraftTool(coreClient))
	registry.Register(coretools.NewPublishDocUpdateTool(coreClient))

	// Code review tools
	registry.Register(coretools.NewGitHubPRGetTool(coreClient))
	registry.Register(coretools.NewGitHubPRDiffTool(coreClient))
	registry.Register(coretools.NewGitHubCommentTool(coreClient))
	registry.Register(coretools.NewGitHubRequestChangesTool(coreClient))
	registry.Register(coretools.NewGitLabMRGetTool(coreClient))
	registry.Register(coretools.NewGitLabMRDiffTool(coreClient))
	registry.Register(coretools.NewGitLabCommentTool(coreClient))
}

// NewDefaultRegistryWithClient creates a registry with all default tools plus
// core tools that communicate with joecored via the HTTP client.
func NewDefaultRegistryWithClient(coreClient *client.Client, policy *safety.SafetyPolicy) *Registry {
	registry := NewDefaultRegistry(policy)
	registerCoreTools(registry, coreClient)
	return registry
}
