package openaicompat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/llm"
)

// newTestClient builds a Client pointed at the given base URL with a fixed
// model and no API key (keyless), mirroring a local compatible endpoint.
func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	c, err := NewClient("test-model", baseURL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

func TestNewClient_RequiresBaseURL(t *testing.T) {
	if _, err := NewClient("m", ""); err == nil {
		t.Fatal("expected error when base_url is empty")
	}
	if _, err := NewClient("m", "  /  "); err == nil {
		t.Fatal("expected error when base_url trims to empty")
	}
}

// TestChat_RequestMapping asserts the outbound request shape: system prompt
// becomes a leading system message, roles map through, tool results become
// tool-role messages, tools serialize as function tools, and max_tokens is set.
func TestChat_RequestMapping(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test")

	var captured chatRequest
	var authHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":5,"completion_tokens":7,"total_tokens":12}}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL+"/v1")
	req := llm.ChatRequest{
		SystemPrompt: "you are joe",
		MaxTokens:    256,
		Messages: []llm.Message{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "calling tool", ToolCalls: []llm.ToolCall{
				{ID: "call_1", Name: "get_metrics", Args: map[string]any{"service": "api"}},
			}},
			{ToolResultID: "call_1", ToolName: "get_metrics", Content: "cpu=80%"},
		},
		Tools: []llm.ToolDefinition{
			{
				Name:        "get_metrics",
				Description: "fetch metrics",
				Parameters: llm.ParameterSchema{
					Type:       "object",
					Properties: map[string]llm.Property{"service": {Type: "string", Description: "svc name"}},
					Required:   []string{"service"},
				},
			},
		},
	}

	resp, err := c.Chat(context.Background(), req)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	// Optional key, when set, is sent as a Bearer token.
	if authHeader != "Bearer sk-test" {
		t.Errorf("Authorization = %q, want Bearer sk-test", authHeader)
	}

	if captured.Model != "test-model" {
		t.Errorf("model = %q, want test-model", captured.Model)
	}
	if captured.MaxTokens != 256 {
		t.Errorf("max_tokens = %d, want 256", captured.MaxTokens)
	}

	// system + user + assistant + tool = 4 messages, system leading.
	if len(captured.Messages) != 4 {
		t.Fatalf("got %d messages, want 4: %+v", len(captured.Messages), captured.Messages)
	}
	if captured.Messages[0].Role != "system" || captured.Messages[0].Content != "you are joe" {
		t.Errorf("message[0] = %+v, want leading system prompt", captured.Messages[0])
	}
	if captured.Messages[1].Role != "user" || captured.Messages[1].Content != "hello" {
		t.Errorf("message[1] = %+v, want user/hello", captured.Messages[1])
	}

	// Assistant message replays its tool call (outbound tool mapping).
	asst := captured.Messages[2]
	if asst.Role != "assistant" || len(asst.ToolCalls) != 1 {
		t.Fatalf("message[2] = %+v, want assistant with 1 tool call", asst)
	}
	if asst.ToolCalls[0].ID != "call_1" || asst.ToolCalls[0].Type != "function" || asst.ToolCalls[0].Function.Name != "get_metrics" {
		t.Errorf("assistant tool call = %+v", asst.ToolCalls[0])
	}
	if !strings.Contains(asst.ToolCalls[0].Function.Arguments, `"service":"api"`) {
		t.Errorf("assistant tool call args = %q, want JSON-encoded service=api", asst.ToolCalls[0].Function.Arguments)
	}

	// Tool result becomes a tool-role message referencing the call id.
	tool := captured.Messages[3]
	if tool.Role != "tool" || tool.ToolCallID != "call_1" || tool.Content != "cpu=80%" {
		t.Errorf("message[3] = %+v, want tool/call_1/cpu=80%%", tool)
	}

	// Tools serialize as type=function with JSON-schema parameters.
	if len(captured.Tools) != 1 {
		t.Fatalf("got %d tools, want 1", len(captured.Tools))
	}
	ft := captured.Tools[0]
	if ft.Type != "function" || ft.Function.Name != "get_metrics" || ft.Function.Description != "fetch metrics" {
		t.Errorf("tool = %+v", ft)
	}
	if ft.Function.Parameters["type"] != "object" {
		t.Errorf("tool parameters type = %v, want object", ft.Function.Parameters["type"])
	}
	props, ok := ft.Function.Parameters["properties"].(map[string]any)
	if !ok || props["service"] == nil {
		t.Errorf("tool parameters properties = %v, want service property", ft.Function.Parameters["properties"])
	}

	// Inbound response mapping.
	if resp.Content != "ok" {
		t.Errorf("content = %q, want ok", resp.Content)
	}
	if resp.Usage.InputTokens != 5 || resp.Usage.OutputTokens != 7 || resp.Usage.TotalTokens != 12 {
		t.Errorf("usage = %+v, want 5/7/12", resp.Usage)
	}
}

// TestChat_NoKey_OmitsAuthHeader proves the key is optional: a keyless client
// sends no Authorization header (required for local keyless endpoints).
func TestChat_NoKey_OmitsAuthHeader(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")

	var hadAuth bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hadAuth = r.Header["Authorization"]
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"hi"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL+"/v1")
	if _, err := c.Chat(context.Background(), llm.ChatRequest{Messages: []llm.Message{{Role: "user", Content: "x"}}}); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if hadAuth {
		t.Error("Authorization header was sent for a keyless client; want none")
	}
}

// TestChat_ResponseToolCallMapping asserts inbound tool_calls parse into the
// neutral ToolCall shape with arguments decoded from the JSON string.
func TestChat_ResponseToolCallMapping(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"choices":[{"message":{"content":"","tool_calls":[
				{"id":"call_9","type":"function","function":{"name":"github_comment","arguments":"{\"pr_url\":\"https://x/1\",\"body\":\"y\"}"}}
			]}}],
			"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}
		}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL+"/v1")
	resp, err := c.Chat(context.Background(), llm.ChatRequest{Messages: []llm.Message{{Role: "user", Content: "go"}}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("got %d tool calls, want 1", len(resp.ToolCalls))
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "call_9" || tc.Name != "github_comment" {
		t.Errorf("tool call id/name = %q/%q, want call_9/github_comment", tc.ID, tc.Name)
	}
	if tc.Args["pr_url"] != "https://x/1" || tc.Args["body"] != "y" {
		t.Errorf("tool call args = %+v, want pr_url=https://x/1 body=y", tc.Args)
	}
}

func TestChat_ErrorStatus_CarriesCode(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad key"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL+"/v1")
	_, err := c.Chat(context.Background(), llm.ChatRequest{Messages: []llm.Message{{Role: "user", Content: "x"}}})
	if err == nil {
		t.Fatal("expected error on 401")
	}
	var apiErr *APIError
	if !asAPIError(err, &apiErr) || apiErr.APICode() != http.StatusUnauthorized {
		t.Fatalf("expected APIError with code 401, got %T: %v", err, err)
	}
}

func TestEmbed_Success(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/embeddings") {
			t.Errorf("path = %q, want .../embeddings", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.1,0.2,0.3]}]}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL+"/v1")
	vec, err := c.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vec) != 3 || vec[0] != 0.1 {
		t.Errorf("embedding = %v, want [0.1 0.2 0.3]", vec)
	}
}

// TestEmbed_UnsupportedEndpoint asserts a 404 from /v1/embeddings produces a
// clear, actionable error rather than a generic failure or panic.
func TestEmbed_UnsupportedEndpoint(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`not found`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL+"/v1")
	_, err := c.Embed(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error when embeddings endpoint is unsupported")
	}
	if !strings.Contains(err.Error(), "does not support embeddings") {
		t.Errorf("error = %q, want a clear unsupported-embeddings message", err.Error())
	}
}
