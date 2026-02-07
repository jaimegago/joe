package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/generative-ai-go/genai"
	"github.com/jaimegago/joe/internal/llm"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
)

// Client implements the LLMAdapter interface using Google's Gemini API
type Client struct {
	client *genai.Client
	model  string
}

// APIError represents an error from the Gemini API with structured details
type APIError struct {
	Code    int    // HTTP status code
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

// NewClient creates a new Gemini client
// API key is read from GEMINI_API_KEY or GOOGLE_API_KEY environment variable
func NewClient(ctx context.Context, model string) (*Client, error) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("GOOGLE_API_KEY")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY or GOOGLE_API_KEY environment variable not set")
	}

	// Check if key appears to be a placeholder or test value
	if len(apiKey) < 20 || apiKey == "test" || apiKey == "your-api-key-here" {
		return nil, fmt.Errorf("GEMINI_API_KEY appears to be invalid (too short or placeholder value). Get a real API key from https://aistudio.google.com/apikey")
	}

	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini client: %w", err)
	}

	if model == "" {
		model = "gemini-2.5-flash"
	}

	return &Client{
		client: client,
		model:  model,
	}, nil
}

// Chat sends a chat request and returns a response
func (c *Client) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	model := c.client.GenerativeModel(c.model)

	// Set system instruction if provided
	if req.SystemPrompt != "" {
		model.SystemInstruction = &genai.Content{
			Parts: []genai.Part{
				genai.Text(req.SystemPrompt),
			},
		}
	}

	// Add tools if provided
	if len(req.Tools) > 0 {
		tools := make([]*genai.Tool, 0, len(req.Tools))
		var toolNames []string
		for i, tool := range req.Tools {
			convertedTool := c.convertToolDefinition(tool)
			// Validate tool has required fields
			if convertedTool == nil || len(convertedTool.FunctionDeclarations) == 0 {
				return nil, fmt.Errorf("tool %d (%s) converted to invalid format", i, tool.Name)
			}
			tools = append(tools, convertedTool)
			toolNames = append(toolNames, tool.Name)
		}

		// Log tool names being registered (for debugging)
		if len(toolNames) > 0 {
			// Tools: [echo, ask_user, read_file, write_file, local_git_status, local_git_diff, run_command]
			_ = toolNames
		}

		model.Tools = tools
	}

	// Build conversation history
	var history []*genai.Content
	var lastParts []genai.Part
	var lastRole string

	for i, msg := range req.Messages {
		// Determine the parts and role for this message
		var parts []genai.Part
		var role string

		if msg.Role == "assistant" {
			role = "model"
			if msg.Content != "" {
				parts = append(parts, genai.Text(msg.Content))
			}
			// Include FunctionCall parts so Gemini sees its own tool calls in history
			for _, tc := range msg.ToolCalls {
				parts = append(parts, genai.FunctionCall{
					Name: tc.Name,
					Args: tc.Args,
				})
			}
		} else if msg.ToolResultID != "" {
			// Tool result message - use FunctionResponse
			role = "user"
			var responseData map[string]any
			if err := json.Unmarshal([]byte(msg.Content), &responseData); err != nil {
				// If content isn't valid JSON, wrap it
				responseData = map[string]any{"result": msg.Content}
			}
			parts = append(parts, genai.FunctionResponse{
				Name:     msg.ToolName,
				Response: responseData,
			})
		} else {
			role = "user"
			parts = append(parts, genai.Text(msg.Content))
		}

		// Gemini API wants the last user message separate for SendMessage
		if i == len(req.Messages)-1 && role == "user" {
			lastParts = parts
			lastRole = role
			break
		}

		if len(parts) > 0 {
			history = append(history, &genai.Content{
				Parts: parts,
				Role:  role,
			})
		}
	}

	// Start chat session with history
	chat := model.StartChat()
	chat.History = history

	// Send the last message
	if lastParts == nil {
		lastParts = []genai.Part{genai.Text("")}
		lastRole = "user"
	}
	_ = lastRole // role is implicit in SendMessage
	resp, err := chat.SendMessage(ctx, lastParts...)
	if err != nil {
		// Add debug info about what we sent
		debugInfo := fmt.Sprintf("\n\nDebug info:\n- Model: %s\n- System prompt: %v\n- Tools count: %d\n- History messages: %d\n- Last message parts: %d",
			c.model, req.SystemPrompt != "", len(req.Tools), len(history), len(lastParts))
		return nil, c.enhanceErrorWithDebug(ctx, err, debugInfo)
	}

	// Convert response
	return c.convertResponse(resp), nil
}

// ChatStream is not yet implemented
func (c *Client) ChatStream(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	return nil, fmt.Errorf("streaming not yet implemented")
}

// Embed is not yet implemented
func (c *Client) Embed(ctx context.Context, text string) ([]float32, error) {
	return nil, fmt.Errorf("embeddings not yet implemented")
}

// convertToolDefinition converts our tool definition to Gemini format
func (c *Client) convertToolDefinition(tool llm.ToolDefinition) *genai.Tool {
	// Gemini requires non-empty descriptions
	if tool.Description == "" {
		tool.Description = tool.Name
	}

	// Convert properties to Gemini schema
	properties := make(map[string]*genai.Schema)
	for name, prop := range tool.Parameters.Properties {
		schemaType := genai.TypeString
		switch prop.Type {
		case "string":
			schemaType = genai.TypeString
		case "number":
			schemaType = genai.TypeNumber
		case "integer":
			schemaType = genai.TypeInteger
		case "boolean":
			schemaType = genai.TypeBoolean
		case "array":
			schemaType = genai.TypeArray
		case "object":
			schemaType = genai.TypeObject
		default:
			// Unknown type, default to string
			schemaType = genai.TypeString
		}

		// Gemini requires property descriptions
		desc := prop.Description
		if desc == "" {
			desc = name
		}

		schema := &genai.Schema{
			Type:        schemaType,
			Description: desc,
		}

		// For array types, add Items schema if specified
		if schemaType == genai.TypeArray && prop.Items != nil {
			itemType := genai.TypeString
			switch prop.Items.Type {
			case "string":
				itemType = genai.TypeString
			case "number":
				itemType = genai.TypeNumber
			case "integer":
				itemType = genai.TypeInteger
			case "boolean":
				itemType = genai.TypeBoolean
			case "object":
				itemType = genai.TypeObject
			}

			itemDesc := prop.Items.Description
			if itemDesc == "" {
				itemDesc = "array item"
			}

			schema.Items = &genai.Schema{
				Type:        itemType,
				Description: itemDesc,
			}
		}

		properties[name] = schema
	}

	// Build parameters schema - Gemini requires this even if empty
	params := &genai.Schema{
		Type:       genai.TypeObject,
		Properties: properties,
	}

	// Only set Required if we have required fields
	if len(tool.Parameters.Required) > 0 {
		params.Required = tool.Parameters.Required
	}

	return &genai.Tool{
		FunctionDeclarations: []*genai.FunctionDeclaration{
			{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  params,
			},
		},
	}
}

// convertResponse converts Gemini response to our response format
func (c *Client) convertResponse(resp *genai.GenerateContentResponse) *llm.ChatResponse {
	result := &llm.ChatResponse{}

	// Safely extract token usage - UsageMetadata can be nil
	if resp.UsageMetadata != nil {
		result.Usage = llm.TokenUsage{
			InputTokens:  int(resp.UsageMetadata.PromptTokenCount),
			OutputTokens: int(resp.UsageMetadata.CandidatesTokenCount),
			TotalTokens:  int(resp.UsageMetadata.TotalTokenCount),
		}
	}

	// Extract content and tool calls from candidates
	for _, candidate := range resp.Candidates {
		if candidate.Content == nil {
			continue
		}

		for _, part := range candidate.Content.Parts {
			switch v := part.(type) {
			case genai.Text:
				result.Content += string(v)
			case genai.FunctionCall:
				// Convert function call to tool call
				args := make(map[string]any)
				for k, val := range v.Args {
					args[k] = val
				}

				result.ToolCalls = append(result.ToolCalls, llm.ToolCall{
					ID:   v.Name, // Gemini doesn't have separate ID, use name
					Name: v.Name,
					Args: args,
				})
			}
		}
	}

	return result
}

// enhanceError provides better error messages for common API errors
// Returns *APIError with structured details for logging
func (c *Client) enhanceError(ctx context.Context, err error) error {
	return c.enhanceErrorWithDebug(ctx, err, "")
}

// enhanceErrorWithDebug is like enhanceError but accepts additional debug info
func (c *Client) enhanceErrorWithDebug(ctx context.Context, err error, debugInfo string) error {
	// Check if it's a Google API error (need to unwrap)
	var apiErr *googleapi.Error
	if errors.As(err, &apiErr) {
		var enhancedErr error

		// Extract more detailed error info
		errDetails := apiErr.Message
		if errDetails == "" && len(apiErr.Errors) > 0 {
			// Try to get message from nested errors
			errDetails = apiErr.Errors[0].Message
		}
		if errDetails == "" {
			errDetails = "(no error message provided by API)"
		}

		switch apiErr.Code {
		case 404:
			// Model not found - fetch available models from API
			modelName := c.model

			// Check if they're using a Claude model by mistake
			hint := ""
			if strings.HasPrefix(modelName, "claude") {
				hint = fmt.Sprintf("\n\nNote: '%s' appears to be a Claude model name, not a Gemini model.", modelName)
			}

			// Try to fetch available models from API (use passed context)
			availableModels := c.listAvailableModels(ctx)
			if len(availableModels) > 0 {
				enhancedErr = fmt.Errorf("model '%s' not found for Gemini provider.%s\n\nAvailable models:\n  - %s\n\nUpdate your config file or use:\n  export JOE_LLM_MODEL=%s",
					modelName, hint, strings.Join(availableModels, "\n  - "), availableModels[0])
			} else {
				// Fallback if API call fails - provide generic message
				enhancedErr = fmt.Errorf("model '%s' not found for Gemini provider.%s\n\nCouldn't fetch available models from API.\nCheck https://ai.google.dev/gemini-api/docs/models/gemini for current model list.\n\nUpdate your config file or use:\n  export JOE_LLM_MODEL=<valid-model-name>",
					modelName, hint)
			}
		case 400:
			// Check for common issues
			modelHint := ""
			if strings.Contains(c.model, "lite") || strings.Contains(c.model, "1.5") || strings.Contains(c.model, "flash-exp") {
				modelHint = fmt.Sprintf("\n\nNote: '%s' may be an outdated Gemini model name.", c.model)
			}

			// Try to fetch available models from API
			availableModels := c.listAvailableModels(ctx)
			var modelsList string
			if len(availableModels) > 0 {
				modelsList = fmt.Sprintf("Valid model names:\n  - %s", strings.Join(availableModels, "\n  - "))
			} else {
				modelsList = "Valid model names: See https://ai.google.dev/gemini-api/docs/models/gemini"
			}

			// Build detailed error message
			errorMsg := fmt.Sprintf("invalid request to Gemini API: %s%s%s\n\nError code: %d\nFull error: %v",
				errDetails, modelHint, debugInfo, apiErr.Code, err)

			enhancedErr = fmt.Errorf("%s\n\nCommon causes:\n  - Invalid model name\n  - Malformed request\n  - Tool/function definition issues\n\n%s", errorMsg, modelsList)
		case 403:
			enhancedErr = fmt.Errorf("authentication failed with Gemini API: %s\n\nCheck that your GEMINI_API_KEY is valid.", apiErr.Message)
		case 429:
			enhancedErr = fmt.Errorf("rate limit exceeded for Gemini API: %s\n\nYou've hit your API quota limit. Options:\n  1. Wait a few minutes and try again\n  2. Check your quota at https://aistudio.google.com/apikey\n  3. Upgrade your API plan if needed\n  4. Try a different model (some have separate quotas)", apiErr.Message)
		default:
			enhancedErr = fmt.Errorf("Gemini API error (%d): %s", apiErr.Code, apiErr.Message)
		}

		return &APIError{
			Code:    apiErr.Code,
			Message: apiErr.Message,
			Err:     enhancedErr,
		}
	}

	// Return original error if we can't enhance it
	return fmt.Errorf("gemini API call failed: %w", err)
}

// listAvailableModels fetches the list of available models from Gemini API
func (c *Client) listAvailableModels(ctx context.Context) []string {
	// Create a context with timeout to avoid blocking too long
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	iter := c.client.ListModels(ctx)
	var models []string

	for {
		model, err := iter.Next()
		if err != nil {
			break
		}

		// Filter to only include generative models (not embedding-only models)
		// and format the name nicely
		if model != nil && strings.Contains(model.Name, "models/") {
			modelName := strings.TrimPrefix(model.Name, "models/")
			// Only include models that support generateContent
			for _, method := range model.SupportedGenerationMethods {
				if method == "generateContent" {
					models = append(models, modelName)
					break
				}
			}
		}

		// Limit to first 10 models to keep error message readable
		if len(models) >= 10 {
			break
		}
	}

	return models
}

// Close closes the Gemini client
func (c *Client) Close() error {
	return c.client.Close()
}
