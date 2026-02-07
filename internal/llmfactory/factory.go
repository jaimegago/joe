package llmfactory

import (
	"context"
	"fmt"

	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/llm/claude"
	"github.com/jaimegago/joe/internal/llm/gemini"
)

// NewAdapter creates an LLMAdapter from a ModelConfig.
// It validates that the required API key environment variable is set
// before creating the provider client.
//
// Note: For Gemini clients, callers should check if the returned adapter
// implements io.Closer and call Close() when done to prevent resource leaks.
func NewAdapter(ctx context.Context, mc config.ModelConfig) (llm.LLMAdapter, error) {
	// Validate API keys using centralized validation
	if err := config.ValidateAPIKeys(mc); err != nil {
		return nil, err
	}

	switch mc.Provider {
	case "claude":
		return claude.NewClient(mc.Model)
	case "gemini":
		return gemini.NewClient(ctx, mc.Model)
	default:
		return nil, fmt.Errorf("unsupported LLM provider: %q (supported: claude, gemini)", mc.Provider)
	}
}
