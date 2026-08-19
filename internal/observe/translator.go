package observe

import (
	"context"
	"fmt"
	"strings"

	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/prompts"
)

// Translator converts natural language questions into native query strings.
type Translator interface {
	Translate(ctx context.Context, question, sourceType string) (string, error)
}

// LLMTranslator implements Translator using an LLM adapter.
type LLMTranslator struct {
	llm llm.LLMAdapter
}

// NewLLMTranslator creates a new LLMTranslator backed by the given LLM adapter.
func NewLLMTranslator(llmAdapter llm.LLMAdapter) *LLMTranslator {
	return &LLMTranslator{llm: llmAdapter}
}

// Translate converts a natural language question into the native query for the given source type.
// sourceType should match a known system identifier: "prometheus", "loki", "datadog", "splunk", etc.
func (t *LLMTranslator) Translate(ctx context.Context, question, sourceType string) (string, error) {
	if t.llm == nil {
		return "", fmt.Errorf("no LLM configured: cannot translate question to %s query", sourceType)
	}

	systemPrompt := prompts.TranslatorSystem(sourceType)

	resp, err := t.llm.Chat(ctx, llm.ChatRequest{
		System: llm.StaticSystem(systemPrompt),
		Messages: []llm.Message{
			{Role: "user", Content: question},
		},
		MaxTokens: 256,
	})
	if err != nil {
		return "", fmt.Errorf("LLM translation failed: %w", err)
	}

	query := strings.TrimSpace(resp.Content)
	if query == "" {
		return "", fmt.Errorf("LLM returned empty translation for question %q", question)
	}
	return query, nil
}
