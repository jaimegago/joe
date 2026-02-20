// Package embeddings provides an LLM-backed Embedder that implements the
// knowledge.Embedder interface using the llm.LLMAdapter.Embed method.
package embeddings

import (
	"context"
	"fmt"

	"github.com/jaimegago/joe/internal/llm"
)

// LLMEmbedder implements knowledge.Embedder using a Joe LLM adapter.
type LLMEmbedder struct {
	adapter   llm.LLMAdapter
	modelName string
}

// New creates an LLMEmbedder.
// modelName is stored in knowledge_entries.embedding_model to track which model
// was used, enabling cache invalidation when the model changes.
func New(adapter llm.LLMAdapter, modelName string) *LLMEmbedder {
	if modelName == "" {
		modelName = "unknown"
	}
	return &LLMEmbedder{adapter: adapter, modelName: modelName}
}

// Embed generates an embedding vector for text.
func (e *LLMEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	if text == "" {
		return nil, fmt.Errorf("cannot embed empty text")
	}
	vec, err := e.adapter.Embed(ctx, text)
	if err != nil {
		return nil, fmt.Errorf("llm embed: %w", err)
	}
	return vec, nil
}

// ModelName returns the identifier for the embedding model.
func (e *LLMEmbedder) ModelName() string {
	return e.modelName
}
