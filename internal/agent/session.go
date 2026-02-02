package agent

import "github.com/jaimegago/joe/internal/llm"

// Session holds the conversation history for an agentic interaction
type Session struct {
	Messages []llm.Message

	// Token usage tracking
	TotalInputTokens  int
	TotalOutputTokens int
	TotalTokens       int

	// Per-run token tracking (reset at start of each Run)
	RunInputTokens  int
	RunOutputTokens int
	RunTokens       int
	RunLLMCalls     int
}

// NewSession creates a new session with empty conversation history
func NewSession() *Session {
	return &Session{
		Messages: make([]llm.Message, 0),
	}
}

// AddMessage adds a message to the conversation history
func (s *Session) AddMessage(message llm.Message) {
	s.Messages = append(s.Messages, message)
}

// AddMessages adds multiple messages to the conversation history
func (s *Session) AddMessages(messages []llm.Message) {
	s.Messages = append(s.Messages, messages...)
}

// Clear clears the conversation history
func (s *Session) Clear() {
	s.Messages = make([]llm.Message, 0)
}

// ResetRunStats resets per-run token tracking (called at start of each Run)
func (s *Session) ResetRunStats() {
	s.RunInputTokens = 0
	s.RunOutputTokens = 0
	s.RunTokens = 0
	s.RunLLMCalls = 0
}

// AddTokenUsage adds token usage from an LLM response
func (s *Session) AddTokenUsage(usage llm.TokenUsage) {
	// Update per-run stats
	s.RunInputTokens += usage.InputTokens
	s.RunOutputTokens += usage.OutputTokens
	s.RunTokens += usage.TotalTokens
	s.RunLLMCalls++

	// Update total session stats
	s.TotalInputTokens += usage.InputTokens
	s.TotalOutputTokens += usage.OutputTokens
	s.TotalTokens += usage.TotalTokens
}
