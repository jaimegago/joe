package llmfactory

import (
	"context"
	"os"

	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/env"
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
	default: // "gemini" — ValidateAPIKeys already rejects unknown providers above
		return gemini.NewClient(ctx, mc.Model)
	}
}

// HasProviderAPIKey reports whether the API-key environment variable
// for the given provider is set to a non-empty value WITHOUT reading
// or returning the key material in any form. It is the single source
// of truth for "does the deployment have credentials for this
// provider?" — handlers that surface provider configuration (the Stream
// G phase G5 providers endpoint) call this rather than hardcoding the
// environment-variable names themselves. Keeping the var-name list in
// the factory means a future provider addition updates one place,
// not two.
//
// The mapping mirrors config.ValidateAPIKeys: Claude requires
// ANTHROPIC_API_KEY; Gemini accepts either GEMINI_API_KEY or
// GOOGLE_API_KEY. Any other provider name returns false — an unknown
// provider has, by definition, no key.
func HasProviderAPIKey(provider string) bool {
	switch provider {
	case "claude":
		return os.Getenv(env.AnthropicAPIKey) != ""
	case "gemini":
		return os.Getenv(env.GeminiAPIKey) != "" || os.Getenv(env.GoogleAPIKey) != ""
	default:
		return false
	}
}
