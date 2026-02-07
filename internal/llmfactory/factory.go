package llmfactory

import (
	"context"
	"fmt"
	"os"

	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/llm/claude"
	"github.com/jaimegago/joe/internal/llm/gemini"
)

// NewAdapter creates an LLMAdapter from a ModelConfig.
// It validates that the required API key environment variable is set
// before creating the provider client.
func NewAdapter(ctx context.Context, mc config.ModelConfig) (llm.LLMAdapter, error) {
	switch mc.Provider {
	case "claude":
		if os.Getenv("ANTHROPIC_API_KEY") == "" {
			return nil, fmt.Errorf("ANTHROPIC_API_KEY is not set (required for provider %q)", mc.Provider)
		}
		return claude.NewClient(mc.Model)
	case "gemini":
		if os.Getenv("GEMINI_API_KEY") == "" && os.Getenv("GOOGLE_API_KEY") == "" {
			return nil, fmt.Errorf("GEMINI_API_KEY or GOOGLE_API_KEY must be set (required for provider %q)", mc.Provider)
		}
		return gemini.NewClient(ctx, mc.Model)
	default:
		return nil, fmt.Errorf("unsupported LLM provider: %q (supported: claude, gemini)", mc.Provider)
	}
}
