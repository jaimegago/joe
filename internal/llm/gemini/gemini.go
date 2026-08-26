package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/jaimegago/joe/internal/env"
	"github.com/jaimegago/joe/internal/llm"
	"google.golang.org/genai"
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
	apiKey := os.Getenv(env.GeminiAPIKey)
	if apiKey == "" {
		apiKey = os.Getenv(env.GoogleAPIKey)
	}
	if apiKey == "" {
		return nil, fmt.Errorf("%s or %s environment variable not set", env.GeminiAPIKey, env.GoogleAPIKey)
	}

	// Check if key appears to be a placeholder or test value
	if len(apiKey) < minAPIKeyLength || apiKey == "test" || apiKey == "your-api-key-here" {
		return nil, fmt.Errorf("%s appears to be invalid (too short or placeholder value). Get a real API key from https://aistudio.google.com/apikey", env.GeminiAPIKey)
	}

	// Backend is pinned explicitly rather than left to inference. A nil
	// ClientConfig makes google.golang.org/genai read GOOGLE_GENAI_USE_VERTEXAI
	// from the environment and switch to Vertex AI, which needs a project and a
	// location joe does not carry — so an unrelated variable in an operator's
	// shell could otherwise redirect every call.
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini client: %w", err)
	}

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
	config := &genai.GenerateContentConfig{}

	// Apply the explicit output cap when the caller set one. Previously the
	// Gemini adapter set NO output limit at all (unlike the Claude adapter's
	// 4096 default), so an agentic turn could generate up to the model's full
	// output ceiling. The agentic path now passes the capabilities table's
	// max-output through ChatRequest.MaxTokens; honour it here.
	applyMaxOutputTokens(config, req.MaxTokens)

	// Set system instruction if provided. Gemini's cached-content resource is
	// a different contract from Anthropic's inline breakpoints, so this
	// adapter renders the segments and ignores their stable/volatile marking —
	// which the seam permits by design.
	if systemPrompt := req.System.String(); systemPrompt != "" {
		config.SystemInstruction = genai.NewContentFromText(systemPrompt, genai.RoleUser)
	}

	// Add tools if provided
	if len(req.Tools) > 0 {
		tools := make([]*genai.Tool, 0, len(req.Tools))
		for i, tool := range req.Tools {
			convertedTool, err := c.convertToolDefinition(tool)
			if err != nil {
				return nil, fmt.Errorf("tool %d (%q): %w", i, tool.Name, err)
			}
			tools = append(tools, convertedTool)
		}
		config.Tools = tools
	}

	contents := buildContents(req.Messages)

	resp, err := c.client.Models.GenerateContent(ctx, c.model, contents, config)
	if err != nil {
		// Add debug info about what we sent
		debugInfo := fmt.Sprintf("\n\nDebug info:\n- Model: %s\n- System prompt: %v\n- Tools count: %d\n- History messages: %d",
			c.model, req.System.String() != "", len(req.Tools), len(contents))
		return nil, c.enhanceErrorWithDebug(ctx, err, debugInfo)
	}

	// Convert response
	return c.convertResponse(resp), nil
}

// buildContents renders joe's provider-neutral message list as the single
// ordered content slice GenerateContent takes.
//
// The whole conversation goes in one slice. The previous SDK's chat session
// wanted the final user message handed to SendMessage separately, so this code
// used to split the last message off the history and synthesise an empty user
// part when the conversation ended on an assistant turn. GenerateContent takes
// the conversation whole, so both the split and the empty-part workaround are
// gone — a trailing assistant turn is now sent as itself.
//
// Extracted as a free function so the history rendering, including the
// signature round-trip below, is testable without a client or a round-trip.
func buildContents(messages []llm.Message) []*genai.Content {
	var contents []*genai.Content

	for _, msg := range messages {
		var parts []*genai.Part
		var role string

		switch {
		case msg.Role == "assistant":
			role = genai.RoleModel
			if msg.Content != "" {
				parts = append(parts, genai.NewPartFromText(msg.Content))
			}
			// Include FunctionCall parts so Gemini sees its own tool calls in
			// history, and replay the provider signature that came back with
			// each one. A Gemini 3 thinking model rejects the turn outright if
			// a functionCall part it previously signed reappears unsigned.
			for _, tc := range msg.ToolCalls {
				part := genai.NewPartFromFunctionCall(tc.Name, tc.Args)
				part.ThoughtSignature = tc.ProviderSignature
				parts = append(parts, part)
			}
		case msg.ToolResultID != "":
			// Tool result message - use FunctionResponse
			role = genai.RoleUser
			var responseData map[string]any
			if err := json.Unmarshal([]byte(msg.Content), &responseData); err != nil {
				// If content isn't valid JSON, wrap it
				responseData = map[string]any{"result": msg.Content}
			}
			parts = append(parts, genai.NewPartFromFunctionResponse(msg.ToolName, responseData))
		default:
			role = genai.RoleUser
			parts = append(parts, genai.NewPartFromText(msg.Content))
		}

		if len(parts) > 0 {
			contents = append(contents, &genai.Content{Parts: parts, Role: role})
		}
	}

	return contents
}

// applyMaxOutputTokens sets the request's output-token cap from the
// ChatRequest's MaxTokens. A value <= 0 leaves the provider default in place
// (matching the prior behaviour for callers that don't set MaxTokens).
// Extracted as a free function so it is testable without a live genai client
// or a network round-trip.
func applyMaxOutputTokens(config *genai.GenerateContentConfig, maxTokens int) {
	if maxTokens > 0 {
		config.MaxOutputTokens = int32(maxTokens)
	}
}

// convertToolDefinition converts our tool definition to Gemini format.
// Returns an error if the schema is invalid — e.g. an array property with no
// Items defined. Gemini rejects such schemas with a silent 400, so we catch
// the problem here with a clear message rather than at API call time.
func (c *Client) convertToolDefinition(tool llm.ToolDefinition) (*genai.Tool, error) {
	// Gemini requires non-empty descriptions
	if tool.Description == "" {
		tool.Description = tool.Name
	}

	// Convert properties to Gemini schema
	properties := make(map[string]*genai.Schema)
	for name, prop := range tool.Parameters.Properties {
		var schemaType genai.Type
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

		// Gemini requires array properties to declare their item type.
		// Without Items, Gemini returns a silent 400 with no helpful message.
		if schemaType == genai.TypeArray && prop.Items == nil {
			return nil, fmt.Errorf(
				"parameter %q has type \"array\" but no Items schema — "+
					"Gemini requires array properties to define their item type "+
					"(set Items in the tool's Parameters() definition)",
				name,
			)
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

		// For array types, add Items schema
		if schemaType == genai.TypeArray {
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
	}, nil
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
		if candidate == nil || candidate.Content == nil {
			continue
		}

		for _, part := range candidate.Content.Parts {
			if part == nil {
				continue
			}

			// A thought part is the model's own reasoning, not an answer. The
			// previous SDK had no way to express one, so every text part was
			// answer text; Gemini 3 thinking models emit both, and folding
			// reasoning into Content would put it in front of the operator.
			if part.Thought {
				continue
			}

			if part.Text != "" {
				result.Content += part.Text
			}

			if fc := part.FunctionCall; fc != nil {
				args := make(map[string]any, len(fc.Args))
				for k, val := range fc.Args {
					args[k] = val
				}

				result.ToolCalls = append(result.ToolCalls, llm.ToolCall{
					ID:   fc.Name, // Gemini doesn't have separate ID, use name
					Name: fc.Name,
					Args: args,
					// Carried so the next turn can replay it. See
					// llm.ToolCall.ProviderSignature.
					ProviderSignature: part.ThoughtSignature,
				})
			}
		}
	}

	return result
}

// enhanceErrorWithDebug provides better error messages for common API errors
// Returns *APIError with structured details for logging
func (c *Client) enhanceErrorWithDebug(ctx context.Context, err error, debugInfo string) error {
	// Check if it's a Gemini API error (need to unwrap). google.golang.org/genai
	// returns genai.APIError BY VALUE, not as a pointer, so the errors.As
	// target is a value of that type.
	var apiErr genai.APIError
	if errors.As(err, &apiErr) {
		var enhancedErr error

		// Extract more detailed error info
		errDetails := apiErr.Message
		if errDetails == "" {
			errDetails = "(no error message provided by API)"
		}

		// Context overflow: a 400-class rejection whose message names an input
		// token count over the model's maximum. Classified BEFORE the generic
		// code switch (which would otherwise bucket it as a generic 400
		// invalid request) so it maps to the context_overflow terminal status.
		// Wraps llm.ErrContextOverflow for errors.Is.
		if isContextOverflowMessage(errDetails) {
			return &APIError{
				Code:    apiErr.Code,
				Message: apiErr.Message,
				Err:     fmt.Errorf("the Gemini API rejected the request: the input exceeds the model's maximum context length:\n  %s\n\n%w", errDetails, llm.ErrContextOverflow),
			}
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
			enhancedErr = fmt.Errorf("authentication failed with Gemini API: %s\n\nCheck that your GEMINI_API_KEY is valid", apiErr.Message)
		case 429:
			enhancedErr = fmt.Errorf("rate limit exceeded for Gemini API: %s\n\nYou've hit your API quota limit. Options:\n  1. Wait a few minutes and try again\n  2. Check your quota at https://aistudio.google.com/apikey\n  3. Upgrade your API plan if needed\n  4. Try a different model (some have separate quotas)", apiErr.Message)
		default:
			enhancedErr = fmt.Errorf("the Gemini API returned an error (%d): %s", apiErr.Code, apiErr.Message)
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

// isContextOverflowMessage reports whether a Gemini API error message is
// clearly an input-token overflow. Conservative: only the documented phrasing
// maps true, so an ordinary malformed-request 400 stays a generic error.
//
// Matched shape (verified against real Gemini 400 errors):
//
//	"The input token count (461428) exceeds the maximum number of tokens
//	 allowed (131072)."
//
// Confirmed from numerous google-gemini/gemini-cli issue reports (e.g. #10393,
// #11507, #12493). Matched on the two stable fragments "input token count" and
// "exceeds the maximum number of tokens"; the parenthesised counts vary.
func isContextOverflowMessage(msg string) bool {
	m := strings.ToLower(msg)
	return strings.Contains(m, "input token count") &&
		strings.Contains(m, "exceeds the maximum number of tokens")
}

// listAvailableModels fetches the list of available models from Gemini API.
//
// The list is a hint and not a statement about servability: Google advertises
// generateContent for models whose generateContent endpoint returns 404. See
// joe-pm queue/gemini-404-discards-api-message.md.
func (c *Client) listAvailableModels(ctx context.Context) []string {
	// Create a context with timeout to avoid blocking too long
	listCtx, cancel := context.WithTimeout(ctx, contextTimeout)
	defer cancel()

	var models []string

	for model, err := range c.client.Models.All(listCtx) {
		if err != nil {
			break
		}

		// Filter to only include generative models (not embedding-only models)
		// and format the name nicely
		if model != nil && strings.Contains(model.Name, "models/") {
			modelName := strings.TrimPrefix(model.Name, "models/")
			// Only include models that support generateContent
			for _, action := range model.SupportedActions {
				if action == "generateContent" {
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

// Close releases the client's resources.
//
// google.golang.org/genai holds none — it has no Close of its own, unlike the
// SDK this replaced. The method stays because it is joe's exported surface and
// callers should not have to know which provider needs closing; it is a
// truthful no-op rather than a stub.
func (c *Client) Close() error {
	return nil
}
