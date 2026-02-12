package useragent

import (
	"context"

	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/observability"
)

// Session holds the conversation history for an agentic interaction
type Session struct {
	Messages []llm.Message
	metrics  *observability.Metrics

	// Token usage tracking
	TotalInputTokens  int
	TotalOutputTokens int
	TotalTokens       int

	// Per-run token tracking (reset at start of each Run)
	RunInputTokens  int
	RunOutputTokens int
	RunTokens       int
	RunLLMCalls     int

	// MaxMessages limits conversation history size to prevent unbounded growth
	// When 0, no limit is applied. Recommended: 100-200 for typical conversations.
	MaxMessages int
}

// NewSession creates a new session with empty conversation history
func NewSession(metrics *observability.Metrics) *Session {
	metrics = observability.EnsureMetrics(metrics)
	metrics.RecordSessionStart()
	return &Session{
		Messages: make([]llm.Message, 0),
		metrics:  metrics,
	}
}

// Close releases session tracking metrics.
func (s *Session) Close() {
	s.metrics.RecordSessionEnd()
}

// AddMessage adds a message to the conversation history.
// If MaxMessages is set and exceeded, older messages are pruned while
// preserving the most recent messages for context.
func (s *Session) AddMessage(message llm.Message) {
	s.Messages = append(s.Messages, message)
	s.metrics.RecordSessionMessage(context.Background(), message.Role)

	// Prune old messages if we've exceeded the limit
	if s.MaxMessages > 0 && len(s.Messages) > s.MaxMessages {
		// Keep the most recent MaxMessages/2 messages
		// This aggressive pruning ensures we don't slowly grow near the limit
		keepCount := s.MaxMessages / 2
		if keepCount < 10 {
			keepCount = 10 // Always keep at least 10 messages for context
		}
		s.Messages = s.Messages[len(s.Messages)-keepCount:]
	}
}

// AddMessages adds multiple messages to the conversation history
func (s *Session) AddMessages(messages []llm.Message) {
	s.Messages = append(s.Messages, messages...)
	ctx := context.Background()
	for _, msg := range messages {
		s.metrics.RecordSessionMessage(ctx, msg.Role)
	}
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
	s.metrics.RecordSessionTokens(context.Background(), usage.TotalTokens)
}
