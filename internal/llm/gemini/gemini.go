package gemini

import (
	"context"
	"fmt"
	"os"

	"github.com/google/generative-ai-go/genai"
	"github.com/jaimegago/joe/internal/llm"
	"google.golang.org/api/option"
)

// Client implements the LLMAdapter interface using Google's Gemini API
type Client struct {
	client *genai.Client
	model  string
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
		model = "gemini-2.0-flash-exp"
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
	var lastUserMessage string

	for i, msg := range req.Messages {
		role := "user"
		if msg.Role == "assistant" {
			role = "model"
		}

		// Gemini API wants the last user message separate for GenerateContent
		if i == len(req.Messages)-1 && msg.Role == "user" {
			lastUserMessage = msg.Content
			break
		}

		history = append(history, &genai.Content{
			Parts: []genai.Part{genai.Text(msg.Content)},
			Role:  role,
		})
	}

	// Start chat session with history
	chat := model.StartChat()
	chat.History = history

	// Send the message
	resp, err := chat.SendMessage(ctx, genai.Text(lastUserMessage))
	if err != nil {
		return nil, fmt.Errorf("gemini API call failed: %w", err)
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

// Close closes the Gemini client
func (c *Client) Close() error {
	return c.client.Close()
}
