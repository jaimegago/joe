package core

import (
	"context"
	"fmt"

	"github.com/jaimegago/joe/internal/adapters/gitops/argocd"
	"github.com/jaimegago/joe/internal/llm"
)

// ArgoCDClient defines what the Argo CD tools need from the HTTP client.
type ArgoCDClient interface {
	ArgoCDApps(ctx context.Context, sourceID, project string) ([]argocd.App, error)
	ArgoCDGetApp(ctx context.Context, sourceID, name string) (*argocd.AppDetail, error)
	ArgoCDGetDiff(ctx context.Context, sourceID, name string) (*argocd.Diff, error)
	ArgoCDGetHistory(ctx context.Context, sourceID, name string, limit int) ([]argocd.SyncOperation, error)
}

// --- argocd_apps ---

// ArgoCDAppsTool lists Argo CD applications.
type ArgoCDAppsTool struct {
	Client ArgoCDClient
}

func NewArgoCDAppsTool(c ArgoCDClient) *ArgoCDAppsTool {
	return &ArgoCDAppsTool{Client: c}
}

func (t *ArgoCDAppsTool) Name() string { return "argocd_apps" }

func (t *ArgoCDAppsTool) Description() string {
	return "List Argo CD applications with their sync status and health. " +
		"Optionally filter by project. Shows name, project, namespace, sync_status " +
		"(Synced/OutOfSync/Unknown), health (Healthy/Degraded/Progressing/Suspended), " +
		"and current revision. " +
		"If you don't know the component_id, call list_components first."
}

func (t *ArgoCDAppsTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"component_id": {
				Type:        "string",
				Description: "ID of the Argo CD source.",
			},
			"project": {
				Type:        "string",
				Description: "Filter by Argo CD project name. Omit to list all projects.",
			},
		},
		Required: []string{"component_id"},
	}
}

func (t *ArgoCDAppsTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["component_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: component_id")
	}
	project, _ := args["project"].(string)

	apps, err := t.Client.ArgoCDApps(ctx, sourceID, project)
	if err != nil {
		return nil, fmt.Errorf("argocd apps: %w", err)
	}

	if apps == nil {
		apps = []argocd.App{}
	}
	return map[string]any{
		"apps":         apps,
		"count":        len(apps),
		"component_id": sourceID,
	}, nil
}

// --- argocd_app ---

// ArgoCDGetAppTool gets details for one Argo CD application.
type ArgoCDGetAppTool struct {
	Client ArgoCDClient
}

func NewArgoCDGetAppTool(c ArgoCDClient) *ArgoCDGetAppTool {
	return &ArgoCDGetAppTool{Client: c}
}

func (t *ArgoCDGetAppTool) Name() string { return "argocd_app" }

func (t *ArgoCDGetAppTool) Description() string {
	return "Get detailed information for a specific Argo CD application: managed resources " +
		"(kind, namespace, sync status, health), and any status conditions. " +
		"Use this after argocd_apps to dig into a specific app. " +
		"If you don't know the component_id, call list_components first."
}

func (t *ArgoCDGetAppTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"component_id": {
				Type:        "string",
				Description: "ID of the Argo CD source.",
			},
			"name": {
				Type:        "string",
				Description: "Name of the Argo CD application.",
			},
		},
		Required: []string{"component_id", "name"},
	}
}

func (t *ArgoCDGetAppTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["component_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: component_id")
	}
	name, ok := args["name"].(string)
	if !ok || name == "" {
		return nil, fmt.Errorf("missing required parameter: name")
	}

	detail, err := t.Client.ArgoCDGetApp(ctx, sourceID, name)
	if err != nil {
		return nil, fmt.Errorf("argocd get app: %w", err)
	}
	return map[string]any{
		"detail":       detail,
		"component_id": sourceID,
	}, nil
}

// --- argocd_diff ---

// ArgoCDDiffTool shows the sync diff for an Argo CD app.
type ArgoCDDiffTool struct {
	Client ArgoCDClient
}

func NewArgoCDDiffTool(c ArgoCDClient) *ArgoCDDiffTool {
	return &ArgoCDDiffTool{Client: c}
}

func (t *ArgoCDDiffTool) Name() string { return "argocd_diff" }

func (t *ArgoCDDiffTool) Description() string {
	return "Get the sync diff status for an Argo CD application: whether it is Synced or OutOfSync, " +
		"the current revision, and any operation state message. " +
		"Use this to determine if an app has drifted from its desired state. " +
		"If you don't know the component_id, call list_components first."
}

func (t *ArgoCDDiffTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"component_id": {
				Type:        "string",
				Description: "ID of the Argo CD source.",
			},
			"name": {
				Type:        "string",
				Description: "Name of the Argo CD application.",
			},
		},
		Required: []string{"component_id", "name"},
	}
}

func (t *ArgoCDDiffTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["component_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: component_id")
	}
	name, ok := args["name"].(string)
	if !ok || name == "" {
		return nil, fmt.Errorf("missing required parameter: name")
	}

	diff, err := t.Client.ArgoCDGetDiff(ctx, sourceID, name)
	if err != nil {
		return nil, fmt.Errorf("argocd diff: %w", err)
	}
	return map[string]any{
		"diff":         diff,
		"component_id": sourceID,
	}, nil
}

// --- argocd_history ---

// ArgoCDHistoryTool lists sync operation history for an Argo CD app.
type ArgoCDHistoryTool struct {
	Client ArgoCDClient
}

func NewArgoCDHistoryTool(c ArgoCDClient) *ArgoCDHistoryTool {
	return &ArgoCDHistoryTool{Client: c}
}

func (t *ArgoCDHistoryTool) Name() string { return "argocd_history" }

func (t *ArgoCDHistoryTool) Description() string {
	return "Get the sync operation history for an Argo CD application. " +
		"Returns recent sync operations with revision, phase (Succeeded/Failed/Running), " +
		"start time, and finish time. Ordered newest first. " +
		"If you don't know the component_id, call list_components first."
}

func (t *ArgoCDHistoryTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"component_id": {
				Type:        "string",
				Description: "ID of the Argo CD source.",
			},
			"name": {
				Type:        "string",
				Description: "Name of the Argo CD application.",
			},
			"limit": {
				Type:        "integer",
				Description: "Maximum number of history entries to return. Defaults to 10.",
			},
		},
		Required: []string{"component_id", "name"},
	}
}

func (t *ArgoCDHistoryTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["component_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: component_id")
	}
	name, ok := args["name"].(string)
	if !ok || name == "" {
		return nil, fmt.Errorf("missing required parameter: name")
	}

	limit := 10
	if l, ok := args["limit"].(float64); ok && l > 0 {
		limit = int(l)
	}

	history, err := t.Client.ArgoCDGetHistory(ctx, sourceID, name, limit)
	if err != nil {
		return nil, fmt.Errorf("argocd history: %w", err)
	}
	if history == nil {
		history = []argocd.SyncOperation{}
	}
	return map[string]any{
		"history":      history,
		"count":        len(history),
		"component_id": sourceID,
	}, nil
}
