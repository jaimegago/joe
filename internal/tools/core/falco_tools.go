package core

import (
	"context"
	"fmt"

	falcoadapter "github.com/jaimegago/joe/internal/adapters/security/falco"
	"github.com/jaimegago/joe/internal/llm"
)

// FalcoClient defines the subset of client.Client needed for Falco tools.
type FalcoClient interface {
	FalcoEvents(ctx context.Context, sourceID, priority, source, rule string, limit int) ([]falcoadapter.Event, error)
	FalcoRules(ctx context.Context, sourceID string) ([]falcoadapter.Rule, error)
}

// --- falco_alerts tool ---

// FalcoAlertsTool queries recent Falco runtime security events via joecored.
type FalcoAlertsTool struct {
	Client FalcoClient
}

// NewFalcoAlertsTool creates a new falco_alerts tool.
func NewFalcoAlertsTool(c FalcoClient) *FalcoAlertsTool {
	return &FalcoAlertsTool{Client: c}
}

func (t *FalcoAlertsTool) Name() string { return "falco_alerts" }

func (t *FalcoAlertsTool) Description() string {
	return "List recent Falco runtime security events. Filter by priority (Emergency, Alert, Critical, Error, Warning, Notice, Informational, Debug), source (syscall, k8s_audit), or rule name. Returns event output, priority, rule, and process/container context. If you don't know the component_id, call list_components first."
}

func (t *FalcoAlertsTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"component_id": {
				Type:        "string",
				Description: "ID of the Falco component to query.",
			},
			"priority": {
				Type:        "string",
				Description: "Optional priority filter (e.g., \"Critical\", \"Warning\"). Matches exact priority level.",
			},
			"source": {
				Type:        "string",
				Description: "Optional event source filter: \"syscall\" or \"k8s_audit\".",
			},
			"rule": {
				Type:        "string",
				Description: "Optional rule name filter (e.g., \"Terminal shell in container\").",
			},
			"limit": {
				Type:        "integer",
				Description: "Maximum number of events to return (default 50).",
			},
		},
		Required: []string{"component_id"},
	}
}

func (t *FalcoAlertsTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["component_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: component_id")
	}

	priority, _ := args["priority"].(string)
	source, _ := args["source"].(string)
	rule, _ := args["rule"].(string)

	limit := 50
	if l, ok := args["limit"].(float64); ok && l > 0 {
		limit = int(l)
	}

	events, err := t.Client.FalcoEvents(ctx, sourceID, priority, source, rule, limit)
	if err != nil {
		return nil, fmt.Errorf("falco events failed: %w", err)
	}

	return map[string]any{
		"events":       events,
		"count":        len(events),
		"component_id": sourceID,
	}, nil
}

// --- falco_rules tool ---

// FalcoRulesTool lists Falco rules observed in recent events via joecored.
type FalcoRulesTool struct {
	Client FalcoClient
}

// NewFalcoRulesTool creates a new falco_rules tool.
func NewFalcoRulesTool(c FalcoClient) *FalcoRulesTool {
	return &FalcoRulesTool{Client: c}
}

func (t *FalcoRulesTool) Name() string { return "falco_rules" }

func (t *FalcoRulesTool) Description() string {
	return "List Falco rules observed in recent runtime security events. Returns rule name, priority, source (syscall/k8s_audit), and event count. Use this to understand which rules are actively firing. If you don't know the component_id, call list_components first."
}

func (t *FalcoRulesTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"component_id": {
				Type:        "string",
				Description: "ID of the Falco component to query.",
			},
		},
		Required: []string{"component_id"},
	}
}

func (t *FalcoRulesTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["component_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: component_id")
	}

	rules, err := t.Client.FalcoRules(ctx, sourceID)
	if err != nil {
		return nil, fmt.Errorf("falco rules failed: %w", err)
	}

	return map[string]any{
		"rules":        rules,
		"count":        len(rules),
		"component_id": sourceID,
	}, nil
}
