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

	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini client: %w", err)
	}

	if model == "" {
		model = "gemini-1.5-flash"
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
		for _, tool := range req.Tools {
			tools = append(tools, c.convertToolDefinition(tool))
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
		return nil, c.enhanceError(ctx, err)
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
		}

		properties[name] = &genai.Schema{
			Type:        schemaType,
			Description: prop.Description,
		}
	}

	return &genai.Tool{
		FunctionDeclarations: []*genai.FunctionDeclaration{
			{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters: &genai.Schema{
					Type:       genai.TypeObject,
					Properties: properties,
					Required:   tool.Parameters.Required,
				},
			},
		},
	}
}

// convertResponse converts Gemini response to our response format
func (c *Client) convertResponse(resp *genai.GenerateContentResponse) *llm.ChatResponse {
	result := &llm.ChatResponse{
		Usage: llm.TokenUsage{
			InputTokens:  int(resp.UsageMetadata.PromptTokenCount),
			OutputTokens: int(resp.UsageMetadata.CandidatesTokenCount),
			TotalTokens:  int(resp.UsageMetadata.TotalTokenCount),
		},
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
	// Check if it's a Google API error (need to unwrap)
	var apiErr *googleapi.Error
	if errors.As(err, &apiErr) {
		var enhancedErr error
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
				// Fallback to hardcoded suggestions if API call fails
				suggestions := []string{
					"gemini-1.5-flash (recommended)",
					"gemini-1.5-pro",
					"gemini-2.0-flash-exp (experimental)",
				}
				enhancedErr = fmt.Errorf("model '%s' not found for Gemini provider.%s\n\nTry these models:\n  - %s\n\nUpdate your config file or use:\n  export JOE_LLM_MODEL=gemini-1.5-flash",
					modelName, hint, strings.Join(suggestions, "\n  - "))
			}
		case 400:
			enhancedErr = fmt.Errorf("invalid request to Gemini API: %s\n\nThis might indicate an unsupported model or invalid parameters.", apiErr.Message)
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
