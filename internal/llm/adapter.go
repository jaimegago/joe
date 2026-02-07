package llm

import "context"

// LLMAdapter is the interface for AI providers (Claude, OpenAI, Ollama, etc.)
// Joe is AI-agnostic - different providers implement this interface
type LLMAdapter interface {
	// Chat sends a chat request and returns a response
	Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)

	// ChatStream sends a chat request and returns a channel for streaming chunks
	ChatStream(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error)

	// Embed generates an embedding vector for the given text
	Embed(ctx context.Context, text string) ([]float32, error)
}

// ChatRequest represents a request to the LLM
type ChatRequest struct {
	SystemPrompt string
	Messages     []Message
	Tools        []ToolDefinition
	MaxTokens    int
}

// ChatResponse represents a response from the LLM
type ChatResponse struct {
	Content   string
	ToolCalls []ToolCall
	Usage     TokenUsage
}

// StreamChunk represents a chunk of streaming response
type StreamChunk struct {
	Content   string
	ToolCalls []ToolCall
	Done      bool
	Error     error
}

// Message represents a message in the conversation
type Message struct {
	Role         string     // "user", "assistant"
	Content      string     // Text content
	ToolCalls    []ToolCall // For assistant messages: the tool calls made
	ToolResultID string     // For tool result messages: references the tool call ID
	ToolName     string     // For tool result messages: the tool name (needed by Gemini)
	IsError      bool       // For tool result messages: whether the result is an error
}

// ToolDefinition describes a tool available to the LLM
type ToolDefinition struct {
	Name        string
	Description string
	Parameters  ParameterSchema
}

// ParameterSchema defines the structure of tool parameters
type ParameterSchema struct {
	Type       string
	Properties map[string]Property
	Required   []string
}

// Property defines a single parameter property
type Property struct {
	Type        string
	Description string
	Items       *Property // For array types: describes array items
}

// ToolCall represents a tool call from the LLM
type ToolCall struct {
	ID   string
	Name string
	Args map[string]any
}

// TokenUsage tracks token consumption
type TokenUsage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}
