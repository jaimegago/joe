package agentloop

import (
	"context"

	"github.com/jaimegago/joe/internal/llm"
)

// echoTool is a minimal in-package test fixture used to drive the agentic loop.
// It mirrors the simple "echo" tool the agentloop tests previously imported
// from internal/tools/local/echo, which was removed with the local-tool tree.
type echoTool struct{}

func newEchoTool() *echoTool { return &echoTool{} }

func (t *echoTool) Name() string { return "echo" }

func (t *echoTool) Description() string {
	return "Echoes back the input message. Useful for testing."
}

func (t *echoTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"message": {
				Type:        "string",
				Description: "The message to echo back",
			},
		},
		Required: []string{"message"},
	}
}

func (t *echoTool) Execute(_ context.Context, args map[string]any) (any, error) {
	message, _ := args["message"].(string)
	return map[string]string{"echoed": message}, nil
}
