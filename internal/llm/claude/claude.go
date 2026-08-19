package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/jaimegago/joe/internal/env"
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
	apiKey := os.Getenv(env.AnthropicAPIKey)
	if apiKey == "" {
		return nil, fmt.Errorf("%s environment variable not set", env.AnthropicAPIKey)
	}

	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	if model == "" {
		model = DefaultModel
	}

	return &Client{
		client: client,
		model:  model,
	}, nil
}

// Chat sends a chat request and returns a response
func (c *Client) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	// Build messages for Anthropic API
	messages := convertMessages(req.Messages)

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
		maxTokens = defaultMaxTokens
	}

	// Build the request
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(c.model),
		MaxTokens: int64(maxTokens),
		Messages:  messages,
	}

	// Add system prompt if provided
	params.System = systemBlocks(req.System)

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

// convertMessages renders joe's neutral message history as Anthropic message
// params.
//
// Extracted from Chat so the block count a turn appends can be measured
// without a network call. That count is what the provider's cache-breakpoint
// lookback window is expressed in, so it is a property worth being able to
// observe rather than derive by reading this loop.
//
// Note the shape it produces: EVERY tool result becomes its own user message
// carrying exactly one tool_result block. So one loop iteration making K tool
// calls appends 1 + 2K content blocks — one assistant text block, K tool_use
// blocks, and K tool_result blocks.
func convertMessages(msgs []llm.Message) []anthropic.MessageParam {
	messages := make([]anthropic.MessageParam, 0, len(msgs))
	for _, msg := range msgs {
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
	return messages
}

// systemBlocks renders the segmented system prompt as Anthropic system content
// blocks, placing a single cache_control breakpoint at the stable/volatile
// boundary the segments declare.
//
// Anthropic renders the prefix as tools → system → messages, and a breakpoint
// caches everything AHEAD of it. One breakpoint at the end of the stable
// system region therefore covers the tool definitions and the static system
// text together, which is exactly the region the stability invariant is scoped
// to — and it stays well inside the four-breakpoint-per-request limit.
//
// The breakpoint is deliberately NOT placed at the end of the system prompt.
// joe appends query-derived skills last, so an end-of-system breakpoint would
// write a fresh entry on every turn and read one on none, paying the write
// premium for zero reads. That trap is the reason the seam carries segments at
// all.
//
// At most two blocks are emitted and their concatenation is byte-identical to
// SystemPrompt.String(), so segmenting changes what is cached and nothing about
// what the model reads.
func systemBlocks(sys llm.SystemPrompt) []anthropic.TextBlockParam {
	text := sys.String()
	if text == "" {
		return nil
	}

	stable := sys.StablePrefix()
	if stable == "" {
		// Nothing cacheable leads the prompt: emit it whole, unmarked.
		return []anthropic.TextBlockParam{{Text: text}}
	}

	head := anthropic.TextBlockParam{
		Text:         stable,
		CacheControl: anthropic.NewCacheControlEphemeralParam(),
	}
	if len(stable) == len(text) {
		// Every segment is stable, so the boundary genuinely IS the end of
		// the system prompt. That is the boundary the segments declared, not
		// the end-of-prompt default this function exists to avoid.
		return []anthropic.TextBlockParam{head}
	}
	// The remainder opens with the separator that joined the two regions, so
	// head.Text + tail.Text == text exactly.
	return []anthropic.TextBlockParam{head, {Text: text[len(stable):]}}
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
			// The only observable of cache efficacy. Anthropic reports these
			// separately from InputTokens, so they are not folded into
			// TotalTokens — see llm.TokenUsage. A read of zero on a repeat
			// turn means the cache did not engage, which a byte-equality
			// argument alone cannot detect.
			CacheReadTokens:  int(response.Usage.CacheReadInputTokens),
			CacheWriteTokens: int(response.Usage.CacheCreationInputTokens),
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
		suggestions := SuggestedModels()

		// Check if they're using a Gemini model by mistake
		hint := ""
		if strings.HasPrefix(modelName, "gemini") {
			hint = fmt.Sprintf("\n\nNote: '%s' appears to be a Gemini model name, not a Claude model.", modelName)
		}

		enhancedErr = fmt.Errorf("model '%s' not found for Claude provider.%s\n\nValid Claude models include:\n  - %s\n\nUpdate your config file or use:\n  export JOE_LLM_MODEL=%s",
			modelName, hint, strings.Join(suggestions, "\n  - "), DefaultModel)
	} else if strings.Contains(errMsg, "401") || strings.Contains(errMsg, "authentication") {
		code = 401
		enhancedErr = fmt.Errorf("authentication failed with Claude API.\n\nCheck that your %s is valid:\n  %s", env.AnthropicAPIKey, errMsg)
	} else if strings.Contains(errMsg, "429") || strings.Contains(errMsg, "rate limit") {
		code = 429
		enhancedErr = fmt.Errorf("rate limit exceeded for Claude API.\n\nPlease wait a moment before retrying:\n  %s", errMsg)
	} else if isContextOverflowMessage(errMsg) {
		// Context overflow: a 400 invalid_request_error whose message names a
		// prompt/input length over the model's maximum. Classified BEFORE the
		// generic 400 branch (overflow messages also carry "400") so it maps
		// to the context_overflow terminal status rather than the generic
		// error bucket. Wraps llm.ErrContextOverflow for errors.Is.
		code = 400
		enhancedErr = fmt.Errorf("the Claude API rejected the request: the prompt or a tool output exceeds the model's maximum context length:\n  %s\n\n%w", errMsg, llm.ErrContextOverflow)
	} else if strings.Contains(errMsg, "400") || strings.Contains(errMsg, "invalid") {
		code = 400
		enhancedErr = fmt.Errorf("invalid request to Claude API.\n\nThis might indicate unsupported parameters:\n  %s", errMsg)
	} else {
		// Return original error with context if we can't enhance it
		return fmt.Errorf("call to Claude API failed: %w", err)
	}

	return &APIError{
		Code:    code,
		Message: errMsg,
		Err:     enhancedErr,
	}
}

// isContextOverflowMessage reports whether a Claude API error message is
// clearly an input/context-length overflow (a 400 invalid_request_error).
// Conservative: only the documented overflow phrasings map true, so an
// ordinary malformed-request 400 stays a generic invalid-request error.
//
// Matched shapes (verified against real Anthropic 400 errors / docs):
//   - "prompt is too long: 215024 tokens > 200000 maximum" — input exceeds
//     the model's context window. Confirmed (anthropic API errors; portkey.ai
//     error library; multiple anthropics/claude-code issue reports).
//   - "input length and max_tokens exceed context limit: ..." — input plus the
//     reserved output cap exceeds the window. Best-effort: documented variant,
//     matched on the stable "exceed(s) context limit" fragment.
func isContextOverflowMessage(msg string) bool {
	m := strings.ToLower(msg)
	return strings.Contains(m, "prompt is too long") ||
		strings.Contains(m, "exceed context limit") ||
		strings.Contains(m, "exceeds context limit")
}
