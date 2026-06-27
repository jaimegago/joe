package openaicompat

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jaimegago/joe/internal/llm"
)

// geminiCompatBaseURL is Google's OpenAI-compatible endpoint base. The adapter
// appends "/chat/completions", yielding the documented
// .../v1beta/openai/chat/completions path.
const geminiCompatBaseURL = "https://generativelanguage.googleapis.com/v1beta/openai"

// geminiCompatModel is a small, fast Gemini model exposed over the compat API.
const geminiCompatModel = "gemini-2.5-flash"

// newLiveClient builds a Client pointed at the Gemini OpenAI-compatible endpoint.
//
// Key-mapping note: the openai-compat adapter reads its key SOLELY from
// OPENAI_API_KEY (env.OpenAIAPIKey). To drive it against Gemini, the Gemini API
// key must be placed in OPENAI_API_KEY — there is intentionally no Gemini-specific
// key reading on this code path. The whole test skips unless OPENAI_API_KEY is
// set, so CI without a key stays green.
func newLiveClient(t *testing.T) *Client {
	t.Helper()
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("OPENAI_API_KEY not set; skipping live openai-compat integration test")
	}
	c, err := NewClient(geminiCompatModel, geminiCompatBaseURL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

// TestLive_Gemini_Chat proves a plain chat request round-trips through the
// adapter against a real OpenAI-compatible server: non-empty Content and
// non-zero token usage.
func TestLive_Gemini_Chat(t *testing.T) {
	c := newLiveClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	resp, err := c.Chat(ctx, llm.ChatRequest{
		Messages: []llm.Message{
			{Role: "user", Content: "Reply with a single short sentence saying hello."},
		},
		MaxTokens: 256,
	})
	if err != nil {
		t.Fatalf("Chat returned error: %v", err)
	}
	if resp.Content == "" {
		t.Errorf("expected non-empty Content, got empty (usage=%+v)", resp.Usage)
	}
	if resp.Usage.TotalTokens == 0 {
		t.Errorf("expected non-zero TotalTokens, got %+v", resp.Usage)
	}
	t.Logf("live chat ok: content=%q usage=%+v", resp.Content, resp.Usage)
}

// TestLive_Gemini_ToolCall proves tool_calls round-trip through the adapter
// against a real server: a get_weather tool plus a weather question should
// produce at least one ToolCall whose name and decoded args match.
//
// If the model declines to call the tool, the test retries once with a more
// directive prompt. If it still does not call, the test fails with a message
// that distinguishes "model chose not to call the tool" from "adapter failed to
// parse a tool call," and states which occurred.
func TestLive_Gemini_ToolCall(t *testing.T) {
	c := newLiveClient(t)

	tools := []llm.ToolDefinition{
		{
			Name:        "get_weather",
			Description: "Get the current weather for a location.",
			Parameters: llm.ParameterSchema{
				Type: "object",
				Properties: map[string]llm.Property{
					"location": {
						Type:        "string",
						Description: "The city to get the weather for, e.g. 'Paris'.",
					},
				},
				Required: []string{"location"},
			},
		},
	}

	prompts := []string{
		"What is the weather like in Paris right now?",
		"Call the get_weather tool for the location Paris. Do not answer in prose; use the tool.",
	}

	var lastResp *llm.ChatResponse
	for attempt, prompt := range prompts {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		resp, err := c.Chat(ctx, llm.ChatRequest{
			Messages:  []llm.Message{{Role: "user", Content: prompt}},
			Tools:     tools,
			MaxTokens: 256,
		})
		cancel()
		if err != nil {
			t.Fatalf("Chat (attempt %d) returned error: %v", attempt+1, err)
		}
		lastResp = resp

		if len(resp.ToolCalls) == 0 {
			t.Logf("attempt %d: model returned no tool call (content=%q); retrying with directive prompt", attempt+1, resp.Content)
			continue
		}

		// A tool call came back. Verify the adapter parsed it correctly.
		tc := resp.ToolCalls[0]
		if tc.Name != "get_weather" {
			t.Fatalf("adapter parsed a tool call with unexpected name %q (want get_weather); args=%+v", tc.Name, tc.Args)
		}
		if perr, ok := tc.Args["_parse_error"]; ok {
			t.Fatalf("ADAPTER FAILED TO PARSE TOOL CALL: get_weather args did not decode: %v", perr)
		}
		loc, ok := tc.Args["location"]
		if !ok {
			t.Fatalf("ADAPTER FAILED TO PARSE TOOL CALL: get_weather called but decoded args missing 'location' (args=%+v)", tc.Args)
		}
		t.Logf("live tool call ok: name=%s location=%v id=%s usage=%+v", tc.Name, loc, tc.ID, resp.Usage)
		return
	}

	// Both attempts returned without a tool call. Distinguish the two failure
	// modes explicitly. We reached here only because every response had zero
	// ToolCalls, which means the model chose not to call the tool (a parse
	// failure would have surfaced as a ToolCall carrying _parse_error above).
	t.Fatalf("MODEL CHOSE NOT TO CALL THE TOOL after %d attempts: every response parsed cleanly through the adapter but contained zero tool calls (last content=%q). This is a model-behavior outcome, NOT an adapter parse failure.", len(prompts), lastResp.Content)
}
