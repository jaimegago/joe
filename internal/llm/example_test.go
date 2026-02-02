package llm_test

import (
	"context"
	"fmt"
	"os"

	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/llm/claude"
	"github.com/jaimegago/joe/internal/llm/gemini"
)

// ExampleLLMAdapter demonstrates how Joe is LLM-agnostic
// The same code works with Claude, Gemini, or any other provider
func Example_llmAgnostic() {
	ctx := context.Background()

	// Define a simple tool that both providers can use
	tools := []llm.ToolDefinition{
		{
			Name:        "get_weather",
			Description: "Get the current weather for a location",
			Parameters: llm.ParameterSchema{
				Type: "object",
				Properties: map[string]llm.Property{
					"location": {
						Type:        "string",
						Description: "The city and state, e.g. San Francisco, CA",
					},
				},
				Required: []string{"location"},
			},
		},
	}

	// Example with Claude
	if os.Getenv("ANTHROPIC_API_KEY") != "" {
		claudeClient, err := claude.NewClient("claude-sonnet-4-20250514")
		if err != nil {
			fmt.Printf("Claude error: %v\n", err)
			return
		}

		// Use the LLMAdapter interface - same code works for any provider
		var llmAdapter llm.LLMAdapter = claudeClient

		response, err := llmAdapter.Chat(ctx, llm.ChatRequest{
			SystemPrompt: "You are a helpful assistant",
			Messages: []llm.Message{
				{Role: "user", Content: "What's the weather in SF?"},
			},
			Tools: tools,
		})

		if err != nil {
			fmt.Printf("Claude chat error: %v\n", err)
			return
		}

		fmt.Printf("Claude response: %s\n", response.Content)
		if len(response.ToolCalls) > 0 {
			fmt.Printf("Claude wants to call tool: %s\n", response.ToolCalls[0].Name)
		}
	}

	// Example with Gemini - SAME CODE, different adapter
	if os.Getenv("GEMINI_API_KEY") != "" || os.Getenv("GOOGLE_API_KEY") != "" {
		geminiClient, err := gemini.NewClient(ctx, "gemini-2.0-flash-exp")
		if err != nil {
			fmt.Printf("Gemini error: %v\n", err)
			return
		}
		defer geminiClient.Close()

		// Use the SAME LLMAdapter interface
		var llmAdapter llm.LLMAdapter = geminiClient

		response, err := llmAdapter.Chat(ctx, llm.ChatRequest{
			SystemPrompt: "You are a helpful assistant",
			Messages: []llm.Message{
				{Role: "user", Content: "What's the weather in SF?"},
			},
			Tools: tools,
		})

		if err != nil {
			fmt.Printf("Gemini chat error: %v\n", err)
			return
		}

		fmt.Printf("Gemini response: %s\n", response.Content)
		if len(response.ToolCalls) > 0 {
			fmt.Printf("Gemini wants to call tool: %s\n", response.ToolCalls[0].Name)
		}
	}

	// This demonstrates Joe's LLM-agnostic design:
	// - Same interface (llm.LLMAdapter)
	// - Same request/response types
	// - Same tool definitions
	// - Different providers are swappable
	// - Joe Core doesn't care which provider is used
}

// Example_switchingProviders shows how to switch providers at runtime
func Example_switchingProviders() {
	ctx := context.Background()

	// Function that works with ANY LLM provider
	askQuestion := func(adapter llm.LLMAdapter, question string) error {
		response, err := adapter.Chat(ctx, llm.ChatRequest{
			Messages: []llm.Message{
				{Role: "user", Content: question},
			},
		})
		if err != nil {
			return err
		}
		fmt.Printf("Answer: %s\n", response.Content)
		return nil
	}

	// Use with Claude
	if os.Getenv("ANTHROPIC_API_KEY") != "" {
		claudeClient, _ := claude.NewClient("")
		askQuestion(claudeClient, "What is 2+2?")
	}

	// Use with Gemini - same function!
	if os.Getenv("GEMINI_API_KEY") != "" {
		geminiClient, _ := gemini.NewClient(ctx, "")
		defer geminiClient.Close()
		askQuestion(geminiClient, "What is 2+2?")
	}

	// This is the power of interface-based design:
	// Joe Core can accept llm.LLMAdapter and work with ANY provider
}
