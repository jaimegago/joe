package tools

import (
	"github.com/jaimegago/joe/internal/client"
	"github.com/jaimegago/joe/internal/safety"
	coretools "github.com/jaimegago/joe/internal/tools/core"
	"github.com/jaimegago/joe/internal/tools/local/askuser"
	"github.com/jaimegago/joe/internal/tools/local/echo"
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

// NewDefaultRegistry creates a registry with all default tools registered.
// If policy is non-nil, tool-specific settings (e.g., allowed_directories for
// write_file) are extracted from it. If nil, tools use permissive defaults.
func NewDefaultRegistry(policy *safety.SafetyPolicy) *Registry {
	registry := NewRegistry()

	// Register basic tools
	registry.Register(echo.NewTool())
	registry.Register(askuser.NewTool())

	// Register file tools
	registry.Register(readfile.New())

	var writeAllowedDirs []string
	if policy != nil {
		writeAllowedDirs = policy.Act.WriteFile.AllowedDirectories
	}
	registry.Register(writefile.New(writeAllowedDirs...))

	// Register git tools
	registry.Register(gitstatus.New())
	registry.Register(gitdiff.New())

	// Register command runner with safe defaults.
	// Only read-only commands are included. Mutation-capable commands (kubectl,
	// helm, argocd) are excluded — they must be explicitly enabled in
	// ~/.joe/safety-policy.yaml and are subject to subcommand allowlists even
	// when enabled.
	registry.Register(runcmd.New([]string{
		"ls", "cat", "head", "tail", "grep", "find", "wc",
	}))

	// Register shared diagnostic tools (T1, Go-native, no CLI deps).
	// These run in-process and work from both joe (user's machine perspective)
	// and joecored (cluster/server perspective).
	registry.Register(netcheck.NewTCPConnectTool())
	registry.Register(netcheck.NewPortScanTool())
	registry.Register(dnsquery.NewDNSLookupTool())
	registry.Register(httpreq.NewHTTPRequestTool())
	registry.Register(sysinfo.NewSystemInfoTool())
	registry.Register(traceroute.NewTraceRouteTool())

	return registry
}

// NewDefaultRegistryWithClient creates a registry with all default tools plus
// core tools that communicate with joecored via the HTTP client.
func NewDefaultRegistryWithClient(coreClient *client.Client, policy *safety.SafetyPolicy) *Registry {
	registry := NewDefaultRegistry(policy)

	// Register core tools (call joecored API)
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

	// Data store tools (Phase 6.7)
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

	// GitOps, CD & IaC tools (Phase 6.8)
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

	// Networking & Ingress tools (Phase 6.9)
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

	// K8s CRD-based tools (Phase 6.10)
	registry.Register(coretools.NewCertManagerCertsTool(coreClient))
	registry.Register(coretools.NewCertManagerIssuersTool(coreClient))
	registry.Register(coretools.NewKEDAScaledObjectsTool(coreClient))
	registry.Register(coretools.NewOPAConstraintsTool(coreClient))
	registry.Register(coretools.NewOPAViolationsTool(coreClient))
	registry.Register(coretools.NewCrossplaneProvidersTool(coreClient))
	registry.Register(coretools.NewCrossplaneResourcesTool(coreClient))

	// Security & runtime tools (Phase 6.11)
	registry.Register(coretools.NewFalcoAlertsTool(coreClient))
	registry.Register(coretools.NewFalcoRulesTool(coreClient))

	return registry
}
