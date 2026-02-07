package config

import (
	"fmt"
	"os"
)

// ValidateAPIKeys validates that required API keys are set for the given model configuration.
// Returns an error with helpful messaging if validation fails.
func ValidateAPIKeys(mc ModelConfig) error {
	switch mc.Provider {
	case "claude":
		if os.Getenv("ANTHROPIC_API_KEY") == "" {
			return fmt.Errorf("ANTHROPIC_API_KEY environment variable is required for Claude provider")
		}
	case "gemini":
		geminiKey := os.Getenv("GEMINI_API_KEY")
		googleKey := os.Getenv("GOOGLE_API_KEY")
		if geminiKey == "" && googleKey == "" {
			return fmt.Errorf("GEMINI_API_KEY or GOOGLE_API_KEY environment variable is required for Gemini provider")
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
	supportedProviders := []string{"claude", "gemini"}
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
	case "claude":
		apiKey := os.Getenv("ANTHROPIC_API_KEY")
		if apiKey == "" {
			return fmt.Errorf("You need to connect Joe to an LLM.\n\nClaude is configured but ANTHROPIC_API_KEY is not set or is empty.\n\nCurrently supported LLMs:\n  - Claude (Anthropic) - requires ANTHROPIC_API_KEY\n  - Gemini (Google) - requires GEMINI_API_KEY or GOOGLE_API_KEY\n\nTo use Claude:\n  export ANTHROPIC_API_KEY=your-api-key-here\n\nTo use Gemini, update your config to use a Gemini model")
		}
	case "gemini":
		geminiKey := os.Getenv("GEMINI_API_KEY")
		googleKey := os.Getenv("GOOGLE_API_KEY")
		if geminiKey == "" && googleKey == "" {
			return fmt.Errorf("You need to connect Joe to an LLM.\n\nGemini is configured but neither GEMINI_API_KEY nor GOOGLE_API_KEY is set or both are empty.\n\nCurrently supported LLMs:\n  - Claude (Anthropic) - requires ANTHROPIC_API_KEY\n  - Gemini (Google) - requires GEMINI_API_KEY or GOOGLE_API_KEY\n\nTo use Gemini:\n  export GEMINI_API_KEY=your-api-key-here\n\nTo use Claude, update your config to use a Claude model")
		}
	}

	return nil
}
