package core_test

import (
	"testing"

	"github.com/jaimegago/joe/internal/client"
	coretools "github.com/jaimegago/joe/internal/tools/core"
)

func TestAWSEC2Tool(t *testing.T) {
	tool := coretools.NewAWSEC2Tool(&client.Client{})

	if tool.Name() != "aws_ec2" {
		t.Errorf("tool name: got %q, want %q", tool.Name(), "aws_ec2")
	}

	desc := tool.Description()
	if desc == "" {
		t.Error("tool description should not be empty")
	}

	params := tool.Parameters()
	if params.Type != "object" {
		t.Errorf("parameter type: got %q, want %q", params.Type, "object")
	}
}

func TestAWSEKSTool(t *testing.T) {
	tool := coretools.NewAWSEKSTool(&client.Client{})

	if tool.Name() != "aws_eks" {
		t.Errorf("tool name: got %q, want %q", tool.Name(), "aws_eks")
	}

	desc := tool.Description()
	if desc == "" {
		t.Error("tool description should not be empty")
	}

	params := tool.Parameters()
	if params.Type != "object" {
		t.Errorf("parameter type: got %q, want %q", params.Type, "object")
	}
}

func TestAWSRDSTool(t *testing.T) {
	tool := coretools.NewAWSRDSTool(&client.Client{})

	if tool.Name() != "aws_rds" {
		t.Errorf("tool name: got %q, want %q", tool.Name(), "aws_rds")
	}

	desc := tool.Description()
	if desc == "" {
		t.Error("tool description should not be empty")
	}

	params := tool.Parameters()
	if params.Type != "object" {
		t.Errorf("parameter type: got %q, want %q", params.Type, "object")
	}
}

func TestAWSVPCTool(t *testing.T) {
	tool := coretools.NewAWSVPCTool(&client.Client{})

	if tool.Name() != "aws_vpc" {
		t.Errorf("tool name: got %q, want %q", tool.Name(), "aws_vpc")
	}

	desc := tool.Description()
	if desc == "" {
		t.Error("tool description should not be empty")
	}

	params := tool.Parameters()
	if params.Type != "object" {
		t.Errorf("parameter type: got %q, want %q", params.Type, "object")
	}
}

func TestAWSToolsRegistration(t *testing.T) {
	// Test that all AWS tools are correctly named
	client := &client.Client{}

	tools := []struct {
		name     string
		expected string
	}{
		{"NewAWSEC2Tool", "aws_ec2"},
		{"NewAWSEKSTool", "aws_eks"},
		{"NewAWSRDSTool", "aws_rds"},
		{"NewAWSVPCTool", "aws_vpc"},
	}

	for _, tt := range tools {
		t.Run(tt.name, func(t *testing.T) {
			var tool interface{ Name() string }

			switch tt.name {
			case "NewAWSEC2Tool":
				tool = coretools.NewAWSEC2Tool(client)
			case "NewAWSEKSTool":
				tool = coretools.NewAWSEKSTool(client)
			case "NewAWSRDSTool":
				tool = coretools.NewAWSRDSTool(client)
			case "NewAWSVPCTool":
				tool = coretools.NewAWSVPCTool(client)
			}

			if tool.Name() != tt.expected {
				t.Errorf("%s name: got %q, want %q", tt.name, tool.Name(), tt.expected)
			}
		})
	}
}
