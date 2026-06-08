package core

import (
	"context"
	"fmt"

	"github.com/jaimegago/joe/internal/adapters/iac/terraform"
	"github.com/jaimegago/joe/internal/llm"
)

// TerraformClient defines what the Terraform tools need from the HTTP client.
type TerraformClient interface {
	TerraformResources(ctx context.Context, sourceID, resourceType string) ([]terraform.Resource, error)
	TerraformGetResource(ctx context.Context, sourceID, address string) (*terraform.Resource, error)
	TerraformOutputs(ctx context.Context, sourceID string) (map[string]terraform.Output, error)
}

// --- terraform_state ---

// TerraformStateTool lists managed resources from a Terraform state file.
type TerraformStateTool struct {
	Client TerraformClient
}

func NewTerraformStateTool(c TerraformClient) *TerraformStateTool {
	return &TerraformStateTool{Client: c}
}

func (t *TerraformStateTool) Name() string { return "terraform_state" }

func (t *TerraformStateTool) Description() string {
	return "List managed resources from a Terraform state file. " +
		"Shows resource type, name, and address (e.g. aws_instance.web). " +
		"Sensitive attributes are automatically redacted. " +
		"Optionally filter by resource type. " +
		"If you don't know the component_id, call list_components first."
}

func (t *TerraformStateTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"component_id": {
				Type:        "string",
				Description: "ID of the Terraform state source.",
			},
			"resource_type": {
				Type:        "string",
				Description: "Filter by resource type (e.g. aws_instance, google_compute_instance). Omit to list all.",
			},
		},
		Required: []string{"component_id"},
	}
}

func (t *TerraformStateTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["component_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: component_id")
	}
	resourceType, _ := args["resource_type"].(string)

	resources, err := t.Client.TerraformResources(ctx, sourceID, resourceType)
	if err != nil {
		return nil, fmt.Errorf("terraform state: %w", err)
	}
	if resources == nil {
		resources = []terraform.Resource{}
	}
	return map[string]any{
		"resources":    resources,
		"count":        len(resources),
		"component_id": sourceID,
	}, nil
}

// --- terraform_resource ---

// TerraformResourceTool gets details for a specific Terraform resource.
type TerraformResourceTool struct {
	Client TerraformClient
}

func NewTerraformResourceTool(c TerraformClient) *TerraformResourceTool {
	return &TerraformResourceTool{Client: c}
}

func (t *TerraformResourceTool) Name() string { return "terraform_resource" }

func (t *TerraformResourceTool) Description() string {
	return "Get full details for a specific Terraform resource by address (e.g. aws_instance.web). " +
		"Returns all attributes and dependencies. Sensitive attributes are redacted. " +
		"Use terraform_state first to find the resource address. " +
		"If you don't know the component_id, call list_components first."
}

func (t *TerraformResourceTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"component_id": {
				Type:        "string",
				Description: "ID of the Terraform state source.",
			},
			"address": {
				Type:        "string",
				Description: "Resource address in the state file (e.g. aws_instance.web, module.vpc.aws_vpc.main).",
			},
		},
		Required: []string{"component_id", "address"},
	}
}

func (t *TerraformResourceTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["component_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: component_id")
	}
	address, ok := args["address"].(string)
	if !ok || address == "" {
		return nil, fmt.Errorf("missing required parameter: address")
	}

	resource, err := t.Client.TerraformGetResource(ctx, sourceID, address)
	if err != nil {
		return nil, fmt.Errorf("terraform resource: %w", err)
	}
	return map[string]any{
		"resource":     resource,
		"component_id": sourceID,
	}, nil
}

// --- terraform_outputs ---

// TerraformOutputsTool lists output values from a Terraform state file.
type TerraformOutputsTool struct {
	Client TerraformClient
}

func NewTerraformOutputsTool(c TerraformClient) *TerraformOutputsTool {
	return &TerraformOutputsTool{Client: c}
}

func (t *TerraformOutputsTool) Name() string { return "terraform_outputs" }

func (t *TerraformOutputsTool) Description() string {
	return "List output values from a Terraform state file. " +
		"Sensitive outputs are automatically redacted. " +
		"Use this to find connection strings, IP addresses, and other output values. " +
		"If you don't know the component_id, call list_components first."
}

func (t *TerraformOutputsTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"component_id": {
				Type:        "string",
				Description: "ID of the Terraform state source.",
			},
		},
		Required: []string{"component_id"},
	}
}

func (t *TerraformOutputsTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["component_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: component_id")
	}

	outputs, err := t.Client.TerraformOutputs(ctx, sourceID)
	if err != nil {
		return nil, fmt.Errorf("terraform outputs: %w", err)
	}
	if outputs == nil {
		outputs = map[string]terraform.Output{}
	}
	return map[string]any{
		"outputs":      outputs,
		"count":        len(outputs),
		"component_id": sourceID,
	}, nil
}
