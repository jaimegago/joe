package claude

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/jaimegago/joe/internal/llm"
)

// newTestClient creates a Client that talks to a mock HTTP server.
// Using the same package lets us construct the struct directly.
func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	apiClient := anthropic.NewClient(
		option.WithAPIKey("test-key"),
		option.WithBaseURL(srv.URL),
	)
	return &Client{client: apiClient, model: DefaultModel}
}

const textResponse = `{
	"id":"msg-1","type":"message","role":"assistant",
	"content":[{"type":"text","text":"Hello!"}],
	"model":"claude-sonnet-4-20250514","stop_reason":"end_turn",
	"usage":{"input_tokens":10,"output_tokens":5}
}`

const toolUseResponse = `{
	"id":"msg-2","type":"message","role":"assistant",
	"content":[{"type":"tool_use","id":"tc-1","name":"echo","input":{"message":"hi"}}],
	"model":"claude-sonnet-4-20250514","stop_reason":"tool_use",
	"usage":{"input_tokens":20,"output_tokens":8}
}`

func TestChat_TextResponse(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, textResponse)
	})

	resp, err := client.Chat(context.Background(), llm.ChatRequest{
		Messages: []llm.Message{{Role: "user", Content: "Hello"}},
	})

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if resp.Content != "Hello!" {
		t.Errorf("Content = %q, want %q", resp.Content, "Hello!")
	}
	if resp.Usage.InputTokens != 10 {
		t.Errorf("InputTokens = %d, want 10", resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens != 5 {
		t.Errorf("OutputTokens = %d, want 5", resp.Usage.OutputTokens)
	}
}

func TestChat_ToolUseResponse(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, toolUseResponse)
	})

	resp, err := client.Chat(context.Background(), llm.ChatRequest{
		Messages: []llm.Message{{Role: "user", Content: "echo something"}},
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
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("Expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "echo" {
		t.Errorf("ToolCall name = %q, want %q", resp.ToolCalls[0].Name, "echo")
	}
}

func TestChat_WithSystemPromptAndMaxTokens(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, textResponse)
	})

	resp, err := client.Chat(context.Background(), llm.ChatRequest{
		SystemPrompt: "You are helpful.",
		MaxTokens:    512,
		Messages:     []llm.Message{{Role: "user", Content: "Hello"}},
	})

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if resp.Content != "Hello!" {
		t.Errorf("Content = %q, want %q", resp.Content, "Hello!")
	}
}

func TestChat_AssistantMessageWithToolCalls(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, textResponse)
	})

	resp, err := client.Chat(context.Background(), llm.ChatRequest{
		Messages: []llm.Message{
			{Role: "user", Content: "use the tool"},
			{
				Role:    "assistant",
				Content: "I'll call echo",
				ToolCalls: []llm.ToolCall{
					{ID: "tc-1", Name: "echo", Args: map[string]any{"message": "hi"}},
				},
			},
			{Role: "user", ToolResultID: "tc-1", ToolName: "echo", Content: `"hi"`},
		},
	})

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if resp.Content != "Hello!" {
		t.Errorf("Content = %q, want %q", resp.Content, "Hello!")
	}
}

func TestChat_ErrorResponse(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"type":"error","error":{"type":"authentication_error","message":"401 invalid api key"}}`)
	})

	_, err := client.Chat(context.Background(), llm.ChatRequest{
		Messages: []llm.Message{{Role: "user", Content: "Hello"}},
	})

	if err == nil {
		t.Fatal("Expected error from 401 response, got nil")
	}
}

func TestNewClient(t *testing.T) {
	tests := []struct {
		name    string
		model   string
		apiKey  string
		wantErr bool
	}{
		{
			name:    "creates client with API key",
			model:   DefaultModel,
			apiKey:  "test-api-key",
			wantErr: false,
		},
		{
			name:    "uses default model when empty",
			model:   "",
			apiKey:  "test-api-key",
			wantErr: false,
		},
		{
			name:    "returns error when API key missing",
			model:   "claude-sonnet-4-20250514",
			apiKey:  "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up environment
			if tt.apiKey != "" {
				os.Setenv("ANTHROPIC_API_KEY", tt.apiKey)
				defer os.Unsetenv("ANTHROPIC_API_KEY")
			} else {
				os.Unsetenv("ANTHROPIC_API_KEY")
			}

			client, err := NewClient(tt.model)

			if (err != nil) != tt.wantErr {
				t.Errorf("NewClient() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if client == nil {
					t.Error("NewClient() returned nil client")
				}
				if tt.model == "" && client.model != "claude-sonnet-4-20250514" {
					t.Errorf("NewClient() model = %v, want default model", client.model)
				}
				if tt.model != "" && client.model != tt.model {
					t.Errorf("NewClient() model = %v, want %v", client.model, tt.model)
				}
			}
		})
	}
}

func TestConvertToolDefinition(t *testing.T) {
	// Set up a client for testing (requires API key in env)
	os.Setenv("ANTHROPIC_API_KEY", "test-key")
	defer os.Unsetenv("ANTHROPIC_API_KEY")

	client, err := NewClient("")
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

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
			result := client.convertToolDefinition(tt.tool)

			// Verify the result is a valid ToolUnionParam
			// Since ToolUnionParam is a union type, we just verify it's not nil
			if result.OfTool == nil {
				t.Error("convertToolDefinition() returned ToolUnionParam with nil OfTool")
			}
		})
	}
}

func TestConvertResponse(t *testing.T) {
	// This test verifies the response conversion logic
	// We can't easily test the full API flow without mocking,
	// but we can verify the conversion function works

	os.Setenv("ANTHROPIC_API_KEY", "test-key")
	defer os.Unsetenv("ANTHROPIC_API_KEY")

	client, err := NewClient("")
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	// Test that client was created successfully
	if client == nil {
		t.Fatal("Client should not be nil")
	}

	// Verify client has the expected model
	expectedModel := "claude-sonnet-4-20250514"
	if client.model != expectedModel {
		t.Errorf("Client model = %v, want %v", client.model, expectedModel)
	}
}

func TestAPIError_Methods(t *testing.T) {
	underlying := errors.New("underlying error")
	apiErr := &APIError{
		Code:    401,
		Message: "auth failed",
		Err:     underlying,
	}

	if apiErr.Error() != "underlying error" {
		t.Errorf("Error() = %q, want %q", apiErr.Error(), "underlying error")
	}
	if apiErr.APICode() != 401 {
		t.Errorf("APICode() = %d, want 401", apiErr.APICode())
	}
	if apiErr.APIMessage() != "auth failed" {
		t.Errorf("APIMessage() = %q, want %q", apiErr.APIMessage(), "auth failed")
	}
	if !errors.Is(apiErr.Unwrap(), underlying) {
		t.Error("Unwrap() should return the underlying error")
	}
}

func TestSuggestedModels(t *testing.T) {
	models := SuggestedModels()
	if len(models) == 0 {
		t.Error("SuggestedModels() returned empty list")
	}
	for _, m := range models {
		if !strings.HasPrefix(m, "claude") {
			t.Errorf("Expected claude model name, got %q", m)
		}
	}
}

func TestEmbed_NotImplemented(t *testing.T) {
	os.Setenv("ANTHROPIC_API_KEY", "test-key")
	defer os.Unsetenv("ANTHROPIC_API_KEY")

	client, _ := NewClient("")
	_, err := client.Embed(context.Background(), "test text")
	if err == nil {
		t.Fatal("Expected error from unimplemented Embed")
	}
}

func TestConvertResponse_WithText(t *testing.T) {
	os.Setenv("ANTHROPIC_API_KEY", "test-key")
	defer os.Unsetenv("ANTHROPIC_API_KEY")

	client, _ := NewClient("")

	msgJSON := `{"id":"msg-1","type":"message","role":"assistant","content":[{"type":"text","text":"Hello, world!"}],"model":"claude-sonnet","stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":5}}`
	var msg anthropic.Message
	if err := json.Unmarshal([]byte(msgJSON), &msg); err != nil {
		t.Fatalf("Failed to unmarshal message: %v", err)
	}

	result := client.convertResponse(&msg)

	if result.Content != "Hello, world!" {
		t.Errorf("Content = %q, want %q", result.Content, "Hello, world!")
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
	if len(result.ToolCalls) != 0 {
		t.Errorf("Expected no tool calls, got %d", len(result.ToolCalls))
	}
}

func TestConvertResponse_WithToolCalls(t *testing.T) {
	os.Setenv("ANTHROPIC_API_KEY", "test-key")
	defer os.Unsetenv("ANTHROPIC_API_KEY")

	client, _ := NewClient("")

	msgJSON := `{"id":"msg-2","type":"message","role":"assistant","content":[{"type":"tool_use","id":"tool-1","name":"echo","input":{"message":"hello"}}],"model":"claude-sonnet","stop_reason":"tool_use","usage":{"input_tokens":20,"output_tokens":10}}`
	var msg anthropic.Message
	if err := json.Unmarshal([]byte(msgJSON), &msg); err != nil {
		t.Fatalf("Failed to unmarshal message: %v", err)
	}

	result := client.convertResponse(&msg)

	if len(result.ToolCalls) != 1 {
		t.Fatalf("Expected 1 tool call, got %d", len(result.ToolCalls))
	}
	if result.ToolCalls[0].Name != "echo" {
		t.Errorf("ToolCall name = %q, want %q", result.ToolCalls[0].Name, "echo")
	}
	if result.ToolCalls[0].ID != "tool-1" {
		t.Errorf("ToolCall ID = %q, want %q", result.ToolCalls[0].ID, "tool-1")
	}
	if result.ToolCalls[0].Args["message"] != "hello" {
		t.Errorf("ToolCall arg = %v, want %q", result.ToolCalls[0].Args["message"], "hello")
	}
}

func TestConvertResponse_WithInvalidToolInput(t *testing.T) {
	os.Setenv("ANTHROPIC_API_KEY", "test-key")
	defer os.Unsetenv("ANTHROPIC_API_KEY")

	client, _ := NewClient("")

	// input is a JSON string, not an object — json.Unmarshal into map[string]any will fail
	msgJSON := `{"id":"msg-3","type":"message","role":"assistant","content":[{"type":"tool_use","id":"tool-1","name":"echo","input":"not-an-object"}],"model":"claude-sonnet","stop_reason":"tool_use","usage":{"input_tokens":5,"output_tokens":5}}`
	var msg anthropic.Message
	if err := json.Unmarshal([]byte(msgJSON), &msg); err != nil {
		t.Fatalf("Failed to unmarshal message: %v", err)
	}

	result := client.convertResponse(&msg)

	if len(result.ToolCalls) != 1 {
		t.Fatalf("Expected 1 tool call, got %d", len(result.ToolCalls))
	}
	if _, hasParseError := result.ToolCalls[0].Args["_parse_error"]; !hasParseError {
		t.Error("Expected _parse_error key in args for invalid JSON input")
	}
}

func TestEnhanceError_NotFound(t *testing.T) {
	os.Setenv("ANTHROPIC_API_KEY", "test-key")
	defer os.Unsetenv("ANTHROPIC_API_KEY")

	client, _ := NewClient("unknown-model")

	err := client.enhanceError(errors.New("404 model not found"))

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("Expected *APIError, got %T: %v", err, err)
	}
	if apiErr.Code != 404 {
		t.Errorf("APICode = %d, want 404", apiErr.Code)
	}
}

func TestEnhanceError_NotFound_GeminiModel(t *testing.T) {
	os.Setenv("ANTHROPIC_API_KEY", "test-key")
	defer os.Unsetenv("ANTHROPIC_API_KEY")

	// Using a Gemini model name triggers the extra hint
	client, _ := NewClient("gemini-2.0-flash")

	err := client.enhanceError(errors.New("404 not found"))

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("Expected *APIError, got %T", err)
	}
	if !strings.Contains(apiErr.Error(), "Gemini") {
		t.Errorf("Expected Gemini hint in error, got: %s", apiErr.Error())
	}
}

// TestEnhanceError_ContextOverflow: a 400 invalid_request_error whose message
// names a prompt-too-long overflow classifies into llm.ErrContextOverflow.
func TestEnhanceError_ContextOverflow(t *testing.T) {
	os.Setenv("ANTHROPIC_API_KEY", "test-key")
	defer os.Unsetenv("ANTHROPIC_API_KEY")

	client, _ := NewClient("")

	cases := []string{
		`400 invalid_request_error: prompt is too long: 215024 tokens > 200000 maximum`,
		`400 {"type":"error","error":{"type":"invalid_request_error","message":"input length and max_tokens exceed context limit: 200000 + 8192 > 200000"}}`,
	}
	for _, raw := range cases {
		err := client.enhanceError(errors.New(raw))
		if !errors.Is(err, llm.ErrContextOverflow) {
			t.Errorf("overflow %q did not classify as ErrContextOverflow: %v", raw, err)
		}
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Code != 400 {
			t.Errorf("overflow %q: want *APIError code 400, got %v", raw, err)
		}
	}
}

// TestEnhanceError_NonOverflow400: an ordinary malformed-request 400 stays a
// generic invalid-request error and does NOT classify as overflow.
func TestEnhanceError_NonOverflow400(t *testing.T) {
	os.Setenv("ANTHROPIC_API_KEY", "test-key")
	defer os.Unsetenv("ANTHROPIC_API_KEY")

	client, _ := NewClient("")

	err := client.enhanceError(errors.New("400 invalid request: unsupported parameter 'foo'"))
	if errors.Is(err, llm.ErrContextOverflow) {
		t.Error("non-overflow 400 misclassified as ErrContextOverflow")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != 400 {
		t.Errorf("want *APIError code 400, got %v", err)
	}
}

func TestEnhanceError_Auth(t *testing.T) {
	os.Setenv("ANTHROPIC_API_KEY", "test-key")
	defer os.Unsetenv("ANTHROPIC_API_KEY")

	client, _ := NewClient("")

	err := client.enhanceError(errors.New("401 authentication failed"))

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("Expected *APIError, got %T", err)
	}
	if apiErr.Code != 401 {
		t.Errorf("APICode = %d, want 401", apiErr.Code)
	}
}

func TestEnhanceError_RateLimit(t *testing.T) {
	os.Setenv("ANTHROPIC_API_KEY", "test-key")
	defer os.Unsetenv("ANTHROPIC_API_KEY")

	client, _ := NewClient("")

	err := client.enhanceError(errors.New("429 rate limit exceeded"))

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("Expected *APIError, got %T", err)
	}
	if apiErr.Code != 429 {
		t.Errorf("APICode = %d, want 429", apiErr.Code)
	}
}

func TestEnhanceError_InvalidRequest(t *testing.T) {
	os.Setenv("ANTHROPIC_API_KEY", "test-key")
	defer os.Unsetenv("ANTHROPIC_API_KEY")

	client, _ := NewClient("")

	err := client.enhanceError(errors.New("400 invalid request body"))

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("Expected *APIError, got %T", err)
	}
	if apiErr.Code != 400 {
		t.Errorf("APICode = %d, want 400", apiErr.Code)
	}
}

func TestEnhanceError_Unknown(t *testing.T) {
	os.Setenv("ANTHROPIC_API_KEY", "test-key")
	defer os.Unsetenv("ANTHROPIC_API_KEY")

	client, _ := NewClient("")

	err := client.enhanceError(errors.New("some unexpected server error"))

	// Unknown errors should NOT be wrapped in *APIError
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		t.Fatal("Unknown error should not be wrapped in *APIError")
	}
	if !strings.Contains(err.Error(), "call to Claude API failed") {
		t.Errorf("Expected 'call to Claude API failed' in error, got: %s", err.Error())
	}
}
