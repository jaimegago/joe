package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/jaimegago/joe/internal/llm"
)

// Client implements the LLMAdapter interface using Anthropic's Claude API
type Client struct {
	client anthropic.Client
	model  string
}

// APIError represents an error from the Claude API with structured details
type APIError struct {
	Code    int    // HTTP status code (inferred from error message)
	Message string // Raw API error message
	Err     error  // Enhanced error with user-friendly message
}

func (e *APIError) Error() string {
	return e.Err.Error()
}

func (e *APIError) Unwrap() error {
	return e.Err
}

// APICode returns the HTTP status code from the API
func (e *APIError) APICode() int {
	return e.Code
}

// APIMessage returns the raw error message from the API
func (e *APIError) APIMessage() string {
	return e.Message
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
			var blocks []anthropic.ContentBlockParamUnion
			if msg.Content != "" {
				blocks = append(blocks, anthropic.NewTextBlock(msg.Content))
			}
			// Include tool_use blocks so Claude sees its own tool calls in history
			for _, tc := range msg.ToolCalls {
				blocks = append(blocks, anthropic.NewToolUseBlock(tc.ID, tc.Args, tc.Name))
			}
			if len(blocks) > 0 {
				messages = append(messages, anthropic.NewAssistantMessage(blocks...))
			}
		} else if msg.ToolResultID != "" {
			// Tool result message - must use tool_result block referencing the tool call ID
			messages = append(messages, anthropic.NewUserMessage(
				anthropic.NewToolResultBlock(msg.ToolResultID, msg.Content, msg.IsError),
			))
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
		return nil, c.enhanceError(err)
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
			if err := json.Unmarshal(toolBlock.Input, &args); err != nil {
				// Log error but continue - use empty args rather than skip the tool call
				// This ensures the LLM knows the tool was called even if args parsing failed
				args = map[string]any{"_parse_error": err.Error()}
			}
			result.ToolCalls = append(result.ToolCalls, llm.ToolCall{
				ID:   toolBlock.ID,
				Name: toolBlock.Name,
				Args: args,
			})
		}
	}

	return result
}

// enhanceError provides better error messages for common API errors
// Returns *APIError with structured details for logging
func (c *Client) enhanceError(err error) error {
	errMsg := err.Error()
	var code int
	var enhancedErr error

	// Check for common error patterns and infer status code
	if strings.Contains(errMsg, "404") || strings.Contains(errMsg, "not found") {
		code = 404
		modelName := c.model
		suggestions := []string{
			"claude-sonnet-4-20250514",
			"claude-opus-4-20241229",
			"claude-3-5-sonnet-20241022",
			"claude-3-5-haiku-20241022",
		}

		// Check if they're using a Gemini model by mistake
		hint := ""
		if strings.HasPrefix(modelName, "gemini") {
			hint = fmt.Sprintf("\n\nNote: '%s' appears to be a Gemini model name, not a Claude model.", modelName)
		}

		enhancedErr = fmt.Errorf("model '%s' not found for Claude provider.%s\n\nValid Claude models include:\n  - %s\n\nUpdate your config file or use:\n  export JOE_LLM_MODEL=claude-sonnet-4-20250514",
			modelName, hint, strings.Join(suggestions, "\n  - "))
	} else if strings.Contains(errMsg, "401") || strings.Contains(errMsg, "authentication") {
		code = 401
		enhancedErr = fmt.Errorf("authentication failed with Claude API.\n\nCheck that your ANTHROPIC_API_KEY is valid:\n  %s", errMsg)
	} else if strings.Contains(errMsg, "429") || strings.Contains(errMsg, "rate limit") {
		code = 429
		enhancedErr = fmt.Errorf("rate limit exceeded for Claude API.\n\nPlease wait a moment before retrying:\n  %s", errMsg)
	} else if strings.Contains(errMsg, "400") || strings.Contains(errMsg, "invalid") {
		code = 400
		enhancedErr = fmt.Errorf("invalid request to Claude API.\n\nThis might indicate unsupported parameters:\n  %s", errMsg)
	} else {
		// Return original error with context if we can't enhance it
		return fmt.Errorf("Claude API call failed: %w", err)
	}

	return &APIError{
		Code:    code,
		Message: errMsg,
		Err:     enhancedErr,
	}
}
