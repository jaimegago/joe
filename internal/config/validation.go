package config

import (
	"fmt"
	"os"

	"github.com/jaimegago/joe/internal/env"
)

// ValidateAPIKeys validates that required API keys are set for the given model configuration.
// Returns an error with helpful messaging if validation fails.
func ValidateAPIKeys(mc ModelConfig) error {
	switch mc.Provider {
	case providerClaude:
		if os.Getenv(env.AnthropicAPIKey) == "" {
			return fmt.Errorf("%s environment variable is required for Claude provider", env.AnthropicAPIKey)
		}
	case providerGemini:
		geminiKey := os.Getenv(env.GeminiAPIKey)
		googleKey := os.Getenv(env.GoogleAPIKey)
		if geminiKey == "" && googleKey == "" {
			return fmt.Errorf("%s or %s environment variable is required for Gemini provider", env.GeminiAPIKey, env.GoogleAPIKey)
		}
	default:
		return fmt.Errorf("unsupported LLM provider: %s", mc.Provider)
	}
	return nil
}

// ValidateAPIKeysWithUserMessage validates API keys and returns a user-friendly error message.
// This is suitable for CLI output where we want to show detailed setup instructions.
func ValidateAPIKeysWithUserMessage(mc ModelConfig) error {
	// Check if provider is supported
	supportedProviders := []string{providerClaude, providerGemini}
	providerSupported := false
	for _, p := range supportedProviders {
		if mc.Provider == p {
			providerSupported = true
			break
		}
	}

	if !providerSupported {
		return fmt.Errorf("You need to connect Joe to an LLM.\n\nCurrently supported LLMs:\n  - Claude (Anthropic)\n  - Gemini (Google)\n\nConfigured provider '%s' is not supported.", mc.Provider)
	}

	// Check for API keys
	switch mc.Provider {
	case providerClaude:
		apiKey := os.Getenv(env.AnthropicAPIKey)
		if apiKey == "" {
			return fmt.Errorf("You need to connect Joe to an LLM.\n\nClaude is configured but %s is not set or is empty.\n\nCurrently supported LLMs:\n  - Claude (Anthropic) - requires %s\n  - Gemini (Google) - requires %s or %s\n\nTo use Claude:\n  export %s=your-api-key-here\n\nTo use Gemini, update your config to use a Gemini model",
				env.AnthropicAPIKey, env.AnthropicAPIKey, env.GeminiAPIKey, env.GoogleAPIKey, env.AnthropicAPIKey)
		}
	case providerGemini:
		geminiKey := os.Getenv(env.GeminiAPIKey)
		googleKey := os.Getenv(env.GoogleAPIKey)
		if geminiKey == "" && googleKey == "" {
			return fmt.Errorf("You need to connect Joe to an LLM.\n\nGemini is configured but neither %s nor %s is set or both are empty.\n\nCurrently supported LLMs:\n  - Claude (Anthropic) - requires %s\n  - Gemini (Google) - requires %s or %s\n\nTo use Gemini:\n  export %s=your-api-key-here\n\nTo use Claude, update your config to use a Claude model",
				env.GeminiAPIKey, env.GoogleAPIKey, env.AnthropicAPIKey, env.GeminiAPIKey, env.GoogleAPIKey, env.GeminiAPIKey)
		}
	}

	return nil
}
