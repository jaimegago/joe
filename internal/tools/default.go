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

	return registry
}
