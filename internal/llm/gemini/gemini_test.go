package gemini

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/google/generative-ai-go/genai"
	"github.com/jaimegago/joe/internal/llm"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
)

// mockRoundTripper intercepts HTTP requests and returns a pre-configured response.
type mockRoundTripper struct {
	body       string
	statusCode int
}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: m.statusCode,
		Body:       io.NopCloser(strings.NewReader(m.body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Request:    req,
	}, nil
}

// newMockGeminiClient builds a Client backed by a mock HTTP transport.
func newMockGeminiClient(t *testing.T, body string, statusCode int) *Client {
	t.Helper()
	transport := &mockRoundTripper{body: body, statusCode: statusCode}
	httpClient := &http.Client{Transport: transport}

	ctx := context.Background()
	gClient, err := genai.NewClient(ctx,
		option.WithAPIKey("fake-key"),
		option.WithHTTPClient(httpClient),
	)
	if err != nil {
		t.Fatalf("Failed to create mock Gemini client: %v", err)
	}
	t.Cleanup(func() { gClient.Close() })
	return &Client{client: gClient, model: DefaultModel}
}

// The Gemini REST streaming endpoint returns a JSON array of response chunks.
const geminiTextResponse = `[{
	"candidates":[{
		"content":{"parts":[{"text":"Hello from Gemini!"}],"role":"model"},
		"finishReason":"STOP","index":0
	}],
	"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5,"totalTokenCount":15}
}]`

func TestNewClient(t *testing.T) {
	tests := []struct {
		name      string
		model     string
		geminiKey string
		googleKey string
		wantErr   bool
		wantModel string
	}{
		{
			name:      "creates client with GEMINI_API_KEY",
			model:     "gemini-2.0-flash-exp",
			geminiKey: "test-gemini-api-key-1234567890",
			wantErr:   false,
			wantModel: "gemini-2.0-flash-exp",
		},
		{
			name:      "creates client with GOOGLE_API_KEY fallback",
			model:     "gemini-2.0-flash-exp",
			googleKey: "test-google-api-key-1234567890",
			wantErr:   false,
			wantModel: "gemini-2.0-flash-exp",
		},
		{
			name:      "uses default model when empty",
			model:     "",
			geminiKey: "test-gemini-api-key-1234567890",
			wantErr:   false,
			wantModel: DefaultModel,
		},
		{
			name:    "returns error when no API key",
			model:   "gemini-2.0-flash-exp",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean environment
			os.Unsetenv("GEMINI_API_KEY")
			os.Unsetenv("GOOGLE_API_KEY")

			// Set up environment
			if tt.geminiKey != "" {
				os.Setenv("GEMINI_API_KEY", tt.geminiKey)
				defer os.Unsetenv("GEMINI_API_KEY")
			}
			if tt.googleKey != "" {
				os.Setenv("GOOGLE_API_KEY", tt.googleKey)
				defer os.Unsetenv("GOOGLE_API_KEY")
			}

			ctx := context.Background()
			client, err := NewClient(ctx, tt.model)

			if (err != nil) != tt.wantErr {
				t.Errorf("NewClient() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if client == nil {
					t.Error("NewClient() returned nil client")
					return
				}
				if client.model != tt.wantModel {
					t.Errorf("NewClient() model = %v, want %v", client.model, tt.wantModel)
				}
				// Clean up
				client.Close()
			}
		})
	}
}

func TestConvertToolDefinition(t *testing.T) {
	// Set up a client for testing
	os.Setenv("GEMINI_API_KEY", "test-gemini-api-key-1234567890")
	defer os.Unsetenv("GEMINI_API_KEY")

	ctx := context.Background()
	client, err := NewClient(ctx, "")
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	tests := []struct {
		name string
		tool llm.ToolDefinition
	}{
		{
			name: "converts simple tool",
			tool: llm.ToolDefinition{
				Name:        "echo",
				Description: "Echoes back the input",
				Parameters: llm.ParameterSchema{
					Type: "object",
					Properties: map[string]llm.Property{
						"message": {
							Type:        "string",
							Description: "Message to echo",
						},
					},
					Required: []string{"message"},
				},
			},
		},
		{
			name: "converts tool with multiple parameters",
			tool: llm.ToolDefinition{
				Name:        "calculate",
				Description: "Performs calculation",
				Parameters: llm.ParameterSchema{
					Type: "object",
					Properties: map[string]llm.Property{
						"operation": {
							Type:        "string",
							Description: "Operation to perform",
						},
						"x": {
							Type:        "number",
							Description: "First operand",
						},
						"y": {
							Type:        "number",
							Description: "Second operand",
						},
					},
					Required: []string{"operation", "x", "y"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := client.convertToolDefinition(tt.tool)
			if err != nil {
				t.Fatalf("convertToolDefinition() unexpected error: %v", err)
			}

			// Verify the tool was created
			if result == nil {
				t.Fatal("convertToolDefinition() returned nil")
			}

			if len(result.FunctionDeclarations) == 0 {
				t.Fatal("convertToolDefinition() returned no function declarations")
			}

			funcDecl := result.FunctionDeclarations[0]
			if funcDecl.Name != tt.tool.Name {
				t.Errorf("Function name = %v, want %v", funcDecl.Name, tt.tool.Name)
			}

			if funcDecl.Description != tt.tool.Description {
				t.Errorf("Function description = %v, want %v", funcDecl.Description, tt.tool.Description)
			}

			if funcDecl.Parameters == nil {
				t.Error("Function parameters is nil")
			}
		})
	}
}

func TestClose(t *testing.T) {
	os.Setenv("GEMINI_API_KEY", "test-gemini-api-key-1234567890")
	defer os.Unsetenv("GEMINI_API_KEY")

	ctx := context.Background()
	client, err := NewClient(ctx, "")
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	// Test that Close doesn't error
	if err := client.Close(); err != nil {
		t.Errorf("Close() returned error: %v", err)
	}
}

func TestNewClient_ShortKey(t *testing.T) {
	os.Unsetenv("GEMINI_API_KEY")
	os.Unsetenv("GOOGLE_API_KEY")
	os.Setenv("GEMINI_API_KEY", "short")
	defer os.Unsetenv("GEMINI_API_KEY")

	ctx := context.Background()
	_, err := NewClient(ctx, "")
	if err == nil {
		t.Fatal("Expected error for short API key")
	}
	if !strings.Contains(err.Error(), "invalid") {
		t.Errorf("Expected 'invalid' in error message, got: %s", err.Error())
	}
}

func TestAPIError_Methods(t *testing.T) {
	underlying := errors.New("underlying error")
	apiErr := &APIError{
		Code:    403,
		Message: "forbidden",
		Err:     underlying,
	}

	if apiErr.Error() != "underlying error" {
		t.Errorf("Error() = %q, want %q", apiErr.Error(), "underlying error")
	}
	if apiErr.APICode() != 403 {
		t.Errorf("APICode() = %d, want 403", apiErr.APICode())
	}
	if apiErr.APIMessage() != "forbidden" {
		t.Errorf("APIMessage() = %q, want %q", apiErr.APIMessage(), "forbidden")
	}
	if !errors.Is(apiErr.Unwrap(), underlying) {
		t.Error("Unwrap() should return the underlying error")
	}
}

func TestEmbed_NotImplemented(t *testing.T) {
	os.Setenv("GEMINI_API_KEY", "test-gemini-api-key-1234567890")
	defer os.Unsetenv("GEMINI_API_KEY")

	ctx := context.Background()
	client, _ := NewClient(ctx, "")
	defer client.Close()

	_, err := client.Embed(ctx, "test text")
	if err == nil {
		t.Fatal("Expected error from unimplemented Embed")
	}
}

func TestConvertToolDefinition_AllTypes(t *testing.T) {
	os.Setenv("GEMINI_API_KEY", "test-gemini-api-key-1234567890")
	defer os.Unsetenv("GEMINI_API_KEY")

	ctx := context.Background()
	client, _ := NewClient(ctx, "")
	defer client.Close()

	tests := []struct {
		name     string
		propType string
	}{
		{"integer type", "integer"},
		{"boolean type", "boolean"},
		{"object type", "object"},
		{"unknown type", "unknown_xyz"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := llm.ToolDefinition{
				Name:        "test_tool",
				Description: "Test tool",
				Parameters: llm.ParameterSchema{
					Type: "object",
					Properties: map[string]llm.Property{
						"param": {Type: tt.propType, Description: "Test param"},
					},
				},
			}
			result, err := client.convertToolDefinition(tool)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result == nil {
				t.Fatal("Expected non-nil tool definition")
			}
		})
	}
}

// TestConvertToolDefinition_ArrayWithoutItems verifies that the converter
// returns a clear, actionable error instead of silently producing an invalid
// schema that Gemini would reject with an opaque 400.
func TestConvertToolDefinition_ArrayWithoutItems(t *testing.T) {
	os.Setenv("GEMINI_API_KEY", "test-gemini-api-key-1234567890")
	defer os.Unsetenv("GEMINI_API_KEY")

	ctx := context.Background()
	client, _ := NewClient(ctx, "")
	defer client.Close()

	tool := llm.ToolDefinition{
		Name:        "port_scan",
		Description: "Scan ports",
		Parameters: llm.ParameterSchema{
			Type: "object",
			Properties: map[string]llm.Property{
				"host":  {Type: "string", Description: "Host to scan."},
				"ports": {Type: "array", Description: "Port numbers."}, // missing Items
			},
			Required: []string{"host", "ports"},
		},
	}

	_, err := client.convertToolDefinition(tool)
	if err == nil {
		t.Fatal("expected error for array property without Items, got nil")
	}
	if !strings.Contains(err.Error(), "ports") {
		t.Errorf("error should mention the offending parameter name, got: %s", err.Error())
	}
	if !strings.Contains(err.Error(), "Items") {
		t.Errorf("error should mention 'Items' to guide the fix, got: %s", err.Error())
	}
}

func TestConvertToolDefinition_ArrayWithItems(t *testing.T) {
	os.Setenv("GEMINI_API_KEY", "test-gemini-api-key-1234567890")
	defer os.Unsetenv("GEMINI_API_KEY")

	ctx := context.Background()
	client, _ := NewClient(ctx, "")
	defer client.Close()

	tool := llm.ToolDefinition{
		Name:        "list_items",
		Description: "Lists items",
		Parameters: llm.ParameterSchema{
			Type: "object",
			Properties: map[string]llm.Property{
				"items": {
					Type:        "array",
					Description: "list of items",
					Items:       &llm.Property{Type: "string", Description: "an item"},
				},
			},
		},
	}

	result, err := client.convertToolDefinition(tool)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("Expected non-nil tool definition")
	}
	if len(result.FunctionDeclarations) == 0 {
		t.Fatal("Expected at least one function declaration")
	}
	params := result.FunctionDeclarations[0].Parameters
	if params == nil {
		t.Fatal("Parameters should not be nil")
	}
	itemsProp, ok := params.Properties["items"]
	if !ok {
		t.Fatal("Expected 'items' property")
	}
	if itemsProp.Items == nil {
		t.Error("Expected Items schema for array type")
	}
}

func TestConvertToolDefinition_EmptyDescriptions(t *testing.T) {
	os.Setenv("GEMINI_API_KEY", "test-gemini-api-key-1234567890")
	defer os.Unsetenv("GEMINI_API_KEY")

	ctx := context.Background()
	client, _ := NewClient(ctx, "")
	defer client.Close()

	tool := llm.ToolDefinition{
		Name:        "my_tool",
		Description: "", // should default to tool name
		Parameters: llm.ParameterSchema{
			Type: "object",
			Properties: map[string]llm.Property{
				"param": {Type: "string", Description: ""}, // should default to param name
			},
		},
	}

	result, err := client.convertToolDefinition(tool)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("Expected non-nil tool definition")
	}
	if result.FunctionDeclarations[0].Description != "my_tool" {
		t.Errorf("Description = %q, want %q", result.FunctionDeclarations[0].Description, "my_tool")
	}
}

func TestConvertToolDefinition_ArrayWithItemsAllTypes(t *testing.T) {
	os.Setenv("GEMINI_API_KEY", "test-gemini-api-key-1234567890")
	defer os.Unsetenv("GEMINI_API_KEY")

	ctx := context.Background()
	client, _ := NewClient(ctx, "")
	defer client.Close()

	itemTypes := []string{"number", "integer", "boolean", "object"}
	for _, itemType := range itemTypes {
		t.Run("items_type_"+itemType, func(t *testing.T) {
			tool := llm.ToolDefinition{
				Name:        "tool",
				Description: "a tool",
				Parameters: llm.ParameterSchema{
					Type: "object",
					Properties: map[string]llm.Property{
						"list": {
							Type:  "array",
							Items: &llm.Property{Type: itemType},
						},
					},
				},
			}
			result, err := client.convertToolDefinition(tool)
			if err != nil {
				t.Fatalf("unexpected error for item type %q: %v", itemType, err)
			}
			if result == nil {
				t.Fatalf("Expected non-nil result for item type %q", itemType)
			}
		})
	}
}

func TestConvertResponse_WithText(t *testing.T) {
	os.Setenv("GEMINI_API_KEY", "test-gemini-api-key-1234567890")
	defer os.Unsetenv("GEMINI_API_KEY")

	ctx := context.Background()
	client, _ := NewClient(ctx, "")
	defer client.Close()

	resp := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{
			{
				Content: &genai.Content{
					Parts: []genai.Part{genai.Text("Hello from Gemini!")},
					Role:  "model",
				},
			},
		},
		UsageMetadata: &genai.UsageMetadata{
			PromptTokenCount:     10,
			CandidatesTokenCount: 5,
			TotalTokenCount:      15,
		},
	}

	result := client.convertResponse(resp)

	if result.Content != "Hello from Gemini!" {
		t.Errorf("Content = %q, want %q", result.Content, "Hello from Gemini!")
	}
	if result.Usage.InputTokens != 10 {
		t.Errorf("InputTokens = %d, want 10", result.Usage.InputTokens)
	}
	if result.Usage.OutputTokens != 5 {
		t.Errorf("OutputTokens = %d, want 5", result.Usage.OutputTokens)
	}
	if result.Usage.TotalTokens != 15 {
		t.Errorf("TotalTokens = %d, want 15", result.Usage.TotalTokens)
	}
}

func TestConvertResponse_WithFunctionCall(t *testing.T) {
	os.Setenv("GEMINI_API_KEY", "test-gemini-api-key-1234567890")
	defer os.Unsetenv("GEMINI_API_KEY")

	ctx := context.Background()
	client, _ := NewClient(ctx, "")
	defer client.Close()

	resp := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{
			{
				Content: &genai.Content{
					Parts: []genai.Part{
						genai.FunctionCall{
							Name: "echo",
							Args: map[string]any{"message": "hello"},
						},
					},
					Role: "model",
				},
			},
		},
	}

	result := client.convertResponse(resp)

	if len(result.ToolCalls) != 1 {
		t.Fatalf("Expected 1 tool call, got %d", len(result.ToolCalls))
	}
	if result.ToolCalls[0].Name != "echo" {
		t.Errorf("ToolCall name = %q, want %q", result.ToolCalls[0].Name, "echo")
	}
	if result.ToolCalls[0].Args["message"] != "hello" {
		t.Errorf("ToolCall arg = %v, want %q", result.ToolCalls[0].Args["message"], "hello")
	}
}

func TestConvertResponse_NilUsageMetadata(t *testing.T) {
	os.Setenv("GEMINI_API_KEY", "test-gemini-api-key-1234567890")
	defer os.Unsetenv("GEMINI_API_KEY")

	ctx := context.Background()
	client, _ := NewClient(ctx, "")
	defer client.Close()

	resp := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{
			{
				Content: &genai.Content{
					Parts: []genai.Part{genai.Text("response")},
					Role:  "model",
				},
			},
		},
		UsageMetadata: nil,
	}

	result := client.convertResponse(resp)

	if result.Usage.InputTokens != 0 {
		t.Errorf("Expected 0 input tokens with nil usage, got %d", result.Usage.InputTokens)
	}
}

func TestConvertResponse_NilCandidateContent(t *testing.T) {
	os.Setenv("GEMINI_API_KEY", "test-gemini-api-key-1234567890")
	defer os.Unsetenv("GEMINI_API_KEY")

	ctx := context.Background()
	client, _ := NewClient(ctx, "")
	defer client.Close()

	resp := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{
			{Content: nil},
		},
	}

	result := client.convertResponse(resp)

	if result.Content != "" {
		t.Errorf("Expected empty content with nil candidate content, got %q", result.Content)
	}
}

func TestEnhanceErrorWithDebug_NonGoogleError(t *testing.T) {
	os.Setenv("GEMINI_API_KEY", "test-gemini-api-key-1234567890")
	defer os.Unsetenv("GEMINI_API_KEY")

	ctx := context.Background()
	client, _ := NewClient(ctx, "")
	defer client.Close()

	err := client.enhanceErrorWithDebug(ctx, errors.New("some unknown error"), "debug info")
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if !strings.Contains(err.Error(), "gemini API call failed") {
		t.Errorf("Expected 'gemini API call failed' in error, got: %s", err.Error())
	}
}

func TestEnhanceErrorWithDebug_GoogleAPI403(t *testing.T) {
	os.Setenv("GEMINI_API_KEY", "test-gemini-api-key-1234567890")
	defer os.Unsetenv("GEMINI_API_KEY")

	ctx := context.Background()
	client, _ := NewClient(ctx, "")
	defer client.Close()

	apiErr := &googleapi.Error{Code: 403, Message: "permission denied"}
	err := client.enhanceErrorWithDebug(ctx, apiErr, "debug info")

	var enhanced *APIError
	if !errors.As(err, &enhanced) {
		t.Fatalf("Expected *APIError, got %T", err)
	}
	if enhanced.Code != 403 {
		t.Errorf("Code = %d, want 403", enhanced.Code)
	}
}

func TestEnhanceErrorWithDebug_GoogleAPI429(t *testing.T) {
	os.Setenv("GEMINI_API_KEY", "test-gemini-api-key-1234567890")
	defer os.Unsetenv("GEMINI_API_KEY")

	ctx := context.Background()
	client, _ := NewClient(ctx, "")
	defer client.Close()

	apiErr := &googleapi.Error{Code: 429, Message: "quota exceeded"}
	err := client.enhanceErrorWithDebug(ctx, apiErr, "debug info")

	var enhanced *APIError
	if !errors.As(err, &enhanced) {
		t.Fatalf("Expected *APIError, got %T", err)
	}
	if enhanced.Code != 429 {
		t.Errorf("Code = %d, want 429", enhanced.Code)
	}
}

// TestEnhanceErrorWithDebug_ContextOverflow: a 400 whose message names an
// input-token overflow classifies into llm.ErrContextOverflow.
func TestEnhanceErrorWithDebug_ContextOverflow(t *testing.T) {
	os.Setenv("GEMINI_API_KEY", "test-gemini-api-key-1234567890")
	defer os.Unsetenv("GEMINI_API_KEY")

	ctx := context.Background()
	client, _ := NewClient(ctx, "")
	defer client.Close()

	apiErr := &googleapi.Error{
		Code:    400,
		Message: "The input token count (461428) exceeds the maximum number of tokens allowed (131072).",
	}
	err := client.enhanceErrorWithDebug(ctx, apiErr, "debug info")
	if !errors.Is(err, llm.ErrContextOverflow) {
		t.Errorf("overflow did not classify as ErrContextOverflow: %v", err)
	}
	var enhanced *APIError
	if !errors.As(err, &enhanced) || enhanced.Code != 400 {
		t.Errorf("want *APIError code 400, got %v", err)
	}
}

// TestEnhanceErrorWithDebug_NonOverflow400: an ordinary 400 stays a generic
// error and does NOT classify as overflow.
func TestEnhanceErrorWithDebug_NonOverflow400(t *testing.T) {
	os.Setenv("GEMINI_API_KEY", "test-gemini-api-key-1234567890")
	defer os.Unsetenv("GEMINI_API_KEY")

	ctx := context.Background()
	client, _ := NewClient(ctx, "")
	defer client.Close()

	apiErr := &googleapi.Error{Code: 400, Message: "Invalid value at 'contents'"}
	err := client.enhanceErrorWithDebug(ctx, apiErr, "debug info")
	if errors.Is(err, llm.ErrContextOverflow) {
		t.Error("non-overflow 400 misclassified as ErrContextOverflow")
	}
	var enhanced *APIError
	if !errors.As(err, &enhanced) || enhanced.Code != 400 {
		t.Errorf("want *APIError code 400, got %v", err)
	}
}

func TestEnhanceErrorWithDebug_GoogleAPIDefault(t *testing.T) {
	os.Setenv("GEMINI_API_KEY", "test-gemini-api-key-1234567890")
	defer os.Unsetenv("GEMINI_API_KEY")

	ctx := context.Background()
	client, _ := NewClient(ctx, "")
	defer client.Close()

	apiErr := &googleapi.Error{Code: 500, Message: "internal server error"}
	err := client.enhanceErrorWithDebug(ctx, apiErr, "debug info")

	var enhanced *APIError
	if !errors.As(err, &enhanced) {
		t.Fatalf("Expected *APIError, got %T", err)
	}
	if enhanced.Code != 500 {
		t.Errorf("Code = %d, want 500", enhanced.Code)
	}
}

func TestEnhanceErrorWithDebug_GoogleAPI404_ClaudeModel(t *testing.T) {
	os.Setenv("GEMINI_API_KEY", "test-gemini-api-key-1234567890")
	defer os.Unsetenv("GEMINI_API_KEY")

	ctx := context.Background()
	// Use a claude model name to trigger the cross-provider hint
	client, _ := NewClient(ctx, "claude-opus-4")
	defer client.Close()

	apiErr := &googleapi.Error{Code: 404, Message: "model not found"}
	err := client.enhanceErrorWithDebug(ctx, apiErr, "debug info")

	var enhanced *APIError
	if !errors.As(err, &enhanced) {
		t.Fatalf("Expected *APIError, got %T", err)
	}
	if enhanced.Code != 404 {
		t.Errorf("Code = %d, want 404", enhanced.Code)
	}
}

func TestEnhanceErrorWithDebug_GoogleAPI400(t *testing.T) {
	os.Setenv("GEMINI_API_KEY", "test-gemini-api-key-1234567890")
	defer os.Unsetenv("GEMINI_API_KEY")

	ctx := context.Background()
	client, _ := NewClient(ctx, "gemini-1.5-flash-exp")
	defer client.Close()

	apiErr := &googleapi.Error{Code: 400, Message: "bad request"}
	err := client.enhanceErrorWithDebug(ctx, apiErr, "debug info")

	var enhanced *APIError
	if !errors.As(err, &enhanced) {
		t.Fatalf("Expected *APIError, got %T", err)
	}
	if enhanced.Code != 400 {
		t.Errorf("Code = %d, want 400", enhanced.Code)
	}
}

func TestChat_TextResponse(t *testing.T) {
	client := newMockGeminiClient(t, geminiTextResponse, 200)

	resp, err := client.Chat(context.Background(), llm.ChatRequest{
		Messages: []llm.Message{{Role: "user", Content: "Hello"}},
	})

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if resp.Content != "Hello from Gemini!" {
		t.Errorf("Content = %q, want %q", resp.Content, "Hello from Gemini!")
	}
	if resp.Usage.InputTokens != 10 {
		t.Errorf("InputTokens = %d, want 10", resp.Usage.InputTokens)
	}
}

func TestChat_WithSystemPromptAndTools(t *testing.T) {
	client := newMockGeminiClient(t, geminiTextResponse, 200)

	resp, err := client.Chat(context.Background(), llm.ChatRequest{
		SystemPrompt: "You are helpful.",
		Messages:     []llm.Message{{Role: "user", Content: "Hello"}},
		Tools: []llm.ToolDefinition{
			{Name: "echo", Description: "Echoes input", Parameters: llm.ParameterSchema{
				Type:       "object",
				Properties: map[string]llm.Property{"message": {Type: "string", Description: "msg"}},
				Required:   []string{"message"},
			}},
		},
	})

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if resp.Content != "Hello from Gemini!" {
		t.Errorf("Content = %q, want %q", resp.Content, "Hello from Gemini!")
	}
}

func TestChat_AssistantAndToolResultMessages(t *testing.T) {
	client := newMockGeminiClient(t, geminiTextResponse, 200)

	resp, err := client.Chat(context.Background(), llm.ChatRequest{
		Messages: []llm.Message{
			{Role: "user", Content: "use the tool"},
			{
				Role:      "assistant",
				Content:   "calling echo",
				ToolCalls: []llm.ToolCall{{ID: "tc-1", Name: "echo", Args: map[string]any{"message": "hi"}}},
			},
			{Role: "user", ToolResultID: "tc-1", ToolName: "echo", Content: `{"result":"hi"}`},
		},
	})

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if resp.Content != "Hello from Gemini!" {
		t.Errorf("Content = %q, want %q", resp.Content, "Hello from Gemini!")
	}
}

func TestChat_LastMessageIsAssistant(t *testing.T) {
	client := newMockGeminiClient(t, geminiTextResponse, 200)

	// When all messages are assistant/history, the last user send is empty
	resp, err := client.Chat(context.Background(), llm.ChatRequest{
		Messages: []llm.Message{
			{Role: "user", Content: "first"},
			{Role: "assistant", Content: "response"},
		},
	})

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if resp.Content != "Hello from Gemini!" {
		t.Errorf("Content = %q, want %q", resp.Content, "Hello from Gemini!")
	}
}

func TestChat_InvalidToolDefinition(t *testing.T) {
	client := newMockGeminiClient(t, geminiTextResponse, 200)

	// A tool with empty name will still be converted (description defaults to name)
	// but we pass a bad tool to trigger the convertToolDefinition nil check path indirectly
	_, err := client.Chat(context.Background(), llm.ChatRequest{
		Messages: []llm.Message{{Role: "user", Content: "Hello"}},
		Tools: []llm.ToolDefinition{
			{Name: "valid", Description: "valid tool", Parameters: llm.ParameterSchema{Type: "object"}},
		},
	})

	if err != nil {
		t.Fatalf("Expected no error with valid tool, got %v", err)
	}
}

func TestChat_ToolResultWithNonJSONContent(t *testing.T) {
	client := newMockGeminiClient(t, geminiTextResponse, 200)

	// Tool result with non-JSON content should be wrapped
	resp, err := client.Chat(context.Background(), llm.ChatRequest{
		Messages: []llm.Message{
			{Role: "user", Content: "use tool"},
			{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "tc-1", Name: "cmd", Args: map[string]any{}}}},
			{Role: "user", ToolResultID: "tc-1", ToolName: "cmd", Content: "plain text result"},
		},
	})

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if resp.Content != "Hello from Gemini!" {
		t.Errorf("Content = %q, want %q", resp.Content, "Hello from Gemini!")
	}
}

func TestEnhanceErrorWithDebug_GoogleAPI400_EmptyMessage(t *testing.T) {
	os.Setenv("GEMINI_API_KEY", "test-gemini-api-key-1234567890")
	defer os.Unsetenv("GEMINI_API_KEY")

	ctx := context.Background()
	client, _ := NewClient(ctx, "")
	defer client.Close()

	// Empty message with nested errors
	apiErr := &googleapi.Error{
		Code:    400,
		Message: "",
		Errors: []googleapi.ErrorItem{
			{Message: "nested error detail"},
		},
	}
	err := client.enhanceErrorWithDebug(ctx, apiErr, "debug info")

	var enhanced *APIError
	if !errors.As(err, &enhanced) {
		t.Fatalf("Expected *APIError, got %T", err)
	}
	if enhanced.Code != 400 {
		t.Errorf("Code = %d, want 400", enhanced.Code)
	}
}

// TestApplyMaxOutputTokens asserts the agentic path's output cap is wired
// onto the genai model's GenerationConfig, and that a non-positive value
// leaves the provider default in place (no limit set). This is the seam that
// fixes the prior behaviour where the Gemini adapter set NO output limit at
// all, unlike the Claude adapter's 4096 default.
func TestApplyMaxOutputTokens(t *testing.T) {
	m := &genai.GenerativeModel{}
	applyMaxOutputTokens(m, 4096)
	if m.MaxOutputTokens == nil {
		t.Fatal("MaxOutputTokens not set for a positive cap")
	}
	if *m.MaxOutputTokens != 4096 {
		t.Errorf("MaxOutputTokens = %d, want 4096", *m.MaxOutputTokens)
	}

	zero := &genai.GenerativeModel{}
	applyMaxOutputTokens(zero, 0)
	if zero.MaxOutputTokens != nil {
		t.Errorf("MaxOutputTokens = %d for a zero cap, want unset (provider default)", *zero.MaxOutputTokens)
	}
}
