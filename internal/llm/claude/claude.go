package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/jaimegago/joe/internal/llm"
)

// Client implements the LLMAdapter interface using Anthropic's Claude API
type Client struct {
	client anthropic.Client
	model  string
}

// NewClient creates a new Claude client
// API key is read from ANTHROPIC_API_KEY environment variable
func NewClient(model string) (*Client, error) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY environment variable not set")
	}

	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	if model == "" {
		model = "claude-sonnet-4-20250514"
	}

	return &Client{
		client: client,
		model:  model,
	}, nil
}

// Chat sends a chat request and returns a response
func (c *Client) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	// Build messages for Anthropic API
	messages := make([]anthropic.MessageParam, 0, len(req.Messages))
	for _, msg := range req.Messages {
		if msg.Role == "assistant" {
			messages = append(messages, anthropic.NewAssistantMessage(anthropic.NewTextBlock(msg.Content)))
		} else {
			messages = append(messages, anthropic.NewUserMessage(anthropic.NewTextBlock(msg.Content)))
		}
	}

	// Build tool definitions if provided
	var tools []anthropic.ToolUnionParam
	if len(req.Tools) > 0 {
		tools = make([]anthropic.ToolUnionParam, 0, len(req.Tools))
		for _, tool := range req.Tools {
			tools = append(tools, c.convertToolDefinition(tool))
		}
	}

	// Set max tokens
	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 4096
	}

	// Build the request
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(c.model),
		MaxTokens: int64(maxTokens),
		Messages:  messages,
	}

	// Add system prompt if provided
	if req.SystemPrompt != "" {
		params.System = []anthropic.TextBlockParam{
			{
				Text: req.SystemPrompt,
			},
		}
	}

	// Add tools if provided
	if len(tools) > 0 {
		params.Tools = tools
	}

	// Make the API call
	response, err := c.client.Messages.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("anthropic API call failed: %w", err)
	}

	// Convert response
	return c.convertResponse(response), nil
}

// ChatStream is not yet implemented
func (c *Client) ChatStream(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	return nil, fmt.Errorf("streaming not yet implemented")
}

// Embed is not yet implemented
func (c *Client) Embed(ctx context.Context, text string) ([]float32, error) {
	return nil, fmt.Errorf("embeddings not yet implemented")
}

// convertToolDefinition converts our tool definition to Anthropic format
func (c *Client) convertToolDefinition(tool llm.ToolDefinition) anthropic.ToolUnionParam {
	// Convert properties
	properties := make(map[string]interface{})
	for name, prop := range tool.Parameters.Properties {
		properties[name] = map[string]interface{}{
			"type":        prop.Type,
			"description": prop.Description,
		}
	}

	// Build input schema
	inputSchema := anthropic.ToolInputSchemaParam{
		Properties: properties,
	}

	if len(tool.Parameters.Required) > 0 {
		inputSchema.Required = tool.Parameters.Required
	}

	return anthropic.ToolUnionParamOfTool(inputSchema, tool.Name)
}

// convertResponse converts Anthropic response to our response format
func (c *Client) convertResponse(response *anthropic.Message) *llm.ChatResponse {
	result := &llm.ChatResponse{
		Usage: llm.TokenUsage{
			InputTokens:  int(response.Usage.InputTokens),
			OutputTokens: int(response.Usage.OutputTokens),
			TotalTokens:  int(response.Usage.InputTokens + response.Usage.OutputTokens),
		},
	}

	// Extract content and tool calls from response
	for _, block := range response.Content {
		switch block.Type {
		case "text":
			textBlock := block.AsText()
			result.Content += textBlock.Text
		case "tool_use":
			toolBlock := block.AsToolUse()
			// Convert tool call
			args := make(map[string]any)
			if err := json.Unmarshal(toolBlock.Input, &args); err == nil {
				result.ToolCalls = append(result.ToolCalls, llm.ToolCall{
					ID:   toolBlock.ID,
					Name: toolBlock.Name,
					Args: args,
				})
			}
		}
	}

	return result
}
