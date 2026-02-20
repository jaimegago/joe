package core

import (
	"context"
	"fmt"
	"strings"

	"github.com/jaimegago/joe/internal/llm"
)

// OPACRDTypes maps OPA/Gatekeeper resource kind names to their full K8s resource identifiers.
var OPACRDTypes = map[string]string{
	"ConstraintTemplate": "constrainttemplates.templates.gatekeeper.sh",
	"Config":             "configs.config.gatekeeper.sh",
}

// OPAK8sClient defines what OPA tools need from the K8s client.
type OPAK8sClient interface {
	K8sListResources(ctx context.Context, sourceID, resource, namespace string) ([]map[string]any, error)
	K8sGetResource(ctx context.Context, sourceID, resource, namespace, name string) (map[string]any, error)
}

// constraintResource derives the K8s resource name for a constraint type from its template kind.
// e.g. "K8sRequiredLabels" → "k8srequiredlabels.constraints.gatekeeper.sh"
func constraintResource(templateKind string) string {
	return strings.ToLower(templateKind) + ".constraints.gatekeeper.sh"
}

// --- opa_constraints ---

// OPAConstraintsTool lists OPA/Gatekeeper ConstraintTemplates and their constraint instances.
type OPAConstraintsTool struct {
	Client OPAK8sClient
}

func NewOPAConstraintsTool(c OPAK8sClient) *OPAConstraintsTool {
	return &OPAConstraintsTool{Client: c}
}

func (t *OPAConstraintsTool) Name() string { return "opa_constraints" }

func (t *OPAConstraintsTool) Description() string {
	return "List OPA/Gatekeeper ConstraintTemplates and their constraint instances with violation counts. " +
		"Without a template filter, lists all ConstraintTemplates to show what policies are defined. " +
		"With a template filter (e.g. K8sRequiredLabels), lists constraint instances of that type " +
		"and shows how many violations each has from the last audit. " +
		"Use source_id of a Kubernetes source where Gatekeeper is installed. " +
		"If you don't know the source_id, call list_sources first."
}

func (t *OPAConstraintsTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"source_id": {
				Type:        "string",
				Description: "ID of the Kubernetes source (where Gatekeeper is installed).",
			},
			"template": {
				Type:        "string",
				Description: "Optional ConstraintTemplate kind name (e.g. K8sRequiredLabels). When provided, lists constraint instances of this type with violation counts.",
			},
		},
		Required: []string{"source_id"},
	}
}

func (t *OPAConstraintsTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["source_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: source_id")
	}
	template, _ := args["template"].(string)

	if template != "" {
		// List constraint instances for a specific template type.
		resource := constraintResource(template)
		constraints, err := t.Client.K8sListResources(ctx, sourceID, resource, "")
		if err != nil {
			return nil, fmt.Errorf("list constraints for template %q: %w", template, err)
		}
		if constraints == nil {
			constraints = []map[string]any{}
		}
		return map[string]any{
			"template":    template,
			"constraints": extractConstraintSummaries(constraints),
			"count":       len(constraints),
			"source_id":   sourceID,
		}, nil
	}

	// List all ConstraintTemplates.
	templates, err := t.Client.K8sListResources(ctx, sourceID, OPACRDTypes["ConstraintTemplate"], "")
	if err != nil {
		return nil, fmt.Errorf("list constraint templates: %w", err)
	}
	if templates == nil {
		templates = []map[string]any{}
	}
	return map[string]any{
		"constraint_templates": extractConstraintTemplateSummaries(templates),
		"count":                len(templates),
		"source_id":            sourceID,
	}, nil
}

// extractConstraintTemplateSummaries extracts summaries from ConstraintTemplate objects.
func extractConstraintTemplateSummaries(templates []map[string]any) []map[string]any {
	var out []map[string]any
	for _, tmpl := range templates {
		summary := map[string]any{}
		if meta, ok := tmpl["metadata"].(map[string]any); ok {
			summary["name"] = meta["name"]
		}
		if spec, ok := tmpl["spec"].(map[string]any); ok {
			if crd, ok := spec["crd"].(map[string]any); ok {
				if crdSpec, ok := crd["spec"].(map[string]any); ok {
					if names, ok := crdSpec["names"].(map[string]any); ok {
						summary["kind"] = names["kind"]
					}
				}
			}
		}
		if status, ok := tmpl["status"].(map[string]any); ok {
			summary["by_pod"] = status["byPod"]
		}
		out = append(out, summary)
	}
	return out
}

// extractConstraintSummaries extracts concise summaries from constraint instance objects.
func extractConstraintSummaries(constraints []map[string]any) []map[string]any {
	var out []map[string]any
	for _, c := range constraints {
		summary := map[string]any{}
		if meta, ok := c["metadata"].(map[string]any); ok {
			summary["name"] = meta["name"]
		}
		if status, ok := c["status"].(map[string]any); ok {
			summary["total_violations"] = status["totalViolations"]
			summary["by_pod"] = status["byPod"]
		}
		out = append(out, summary)
	}
	return out
}

// --- opa_violations ---

// OPAViolationsTool retrieves violation details for a specific OPA/Gatekeeper constraint.
type OPAViolationsTool struct {
	Client OPAK8sClient
}

func NewOPAViolationsTool(c OPAK8sClient) *OPAViolationsTool {
	return &OPAViolationsTool{Client: c}
}

func (t *OPAViolationsTool) Name() string { return "opa_violations" }

func (t *OPAViolationsTool) Description() string {
	return "Get violation details for a specific OPA/Gatekeeper constraint. " +
		"Requires the ConstraintTemplate kind (e.g. K8sRequiredLabels) and the constraint name. " +
		"Returns the full list of audit violations: which resources are violating the policy and why. " +
		"Use opa_constraints first to discover template kinds and constraint names. " +
		"Use source_id of a Kubernetes source where Gatekeeper is installed. " +
		"If you don't know the source_id, call list_sources first."
}

func (t *OPAViolationsTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"source_id": {
				Type:        "string",
				Description: "ID of the Kubernetes source (where Gatekeeper is installed).",
			},
			"template_kind": {
				Type:        "string",
				Description: "ConstraintTemplate kind name, e.g. K8sRequiredLabels.",
			},
			"name": {
				Type:        "string",
				Description: "Name of the specific constraint instance to inspect.",
			},
		},
		Required: []string{"source_id", "template_kind", "name"},
	}
}

func (t *OPAViolationsTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["source_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: source_id")
	}
	templateKind, ok := args["template_kind"].(string)
	if !ok || templateKind == "" {
		return nil, fmt.Errorf("missing required parameter: template_kind")
	}
	name, ok := args["name"].(string)
	if !ok || name == "" {
		return nil, fmt.Errorf("missing required parameter: name")
	}

	resource := constraintResource(templateKind)
	constraint, err := t.Client.K8sGetResource(ctx, sourceID, resource, "", name)
	if err != nil {
		return nil, fmt.Errorf("get constraint %s/%s: %w", templateKind, name, err)
	}

	result := map[string]any{
		"constraint":    name,
		"template_kind": templateKind,
		"source_id":     sourceID,
	}

	if status, ok := constraint["status"].(map[string]any); ok {
		result["total_violations"] = status["totalViolations"]
		result["violations"] = status["violations"]
	}
	if spec, ok := constraint["spec"].(map[string]any); ok {
		result["enforcement_action"] = spec["enforcementAction"]
	}

	return result, nil
}
