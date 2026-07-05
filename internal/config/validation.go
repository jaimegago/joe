package config

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/jaimegago/joe/internal/env"
)

// noProviderKeyMessage is the actionable error shown when auto-selection runs
// but no supported provider key is present. Format args (in order):
// AnthropicAPIKey, GeminiAPIKey, GoogleAPIKey.
const noProviderKeyMessage = `you need to connect Joe to an LLM.

No supported provider API key was found in joe's environment.
Set exactly one of:
  - Claude (Anthropic): export %s=...
  - Gemini (Google):    export %s=...   (or %s)

Then restart joe. To force a specific provider/model regardless of which
keys are present, set:
  export JOE_LLM_PROVIDER=claude|gemini
  export JOE_LLM_MODEL=<model-name>`

// AutoSelectProvider fills the provider gap for a user who expressed no explicit
// LLM provider preference (no JOE_LLM_PROVIDER and no llm.current in the config
// file), choosing a provider from whichever supported API key is present in the
// environment. joe calls this once at startup so a stranger with exactly
// one provider key can run with zero configuration.
//
// Rules:
//   - explicit preference set      -> no-op (the explicit choice always wins)
//   - selected provider's key set  -> keep it (preserves any JOE_LLM_MODEL)
//   - only the other key set        -> switch to that provider with its default model
//   - both keys set                 -> keep Claude (deterministic tie-break, logged)
//   - neither key set               -> actionable error naming both providers
func (c *Config) AutoSelectProvider() error {
	if c.explicitProvider {
		return nil
	}

	hasAnthropic := os.Getenv(env.AnthropicAPIKey) != ""
	hasGemini := os.Getenv(env.GeminiAPIKey) != "" || os.Getenv(env.GoogleAPIKey) != ""

	if !hasAnthropic && !hasGemini {
		return fmt.Errorf(noProviderKeyMessage, env.AnthropicAPIKey, env.GeminiAPIKey, env.GoogleAPIKey)
	}

	current, _ := c.LLM.CurrentModel()

	// If the currently-selected provider already has its key, keep it. This
	// preserves any model the user set via JOE_LLM_MODEL or the config file.
	if (current.Provider == providerClaude && hasAnthropic) || (current.Provider == providerGemini && hasGemini) {
		if current.Provider == providerClaude && hasAnthropic && hasGemini {
			slog.Info("both ANTHROPIC_API_KEY and a Gemini key are set; defaulting to Claude. Set JOE_LLM_PROVIDER=gemini (optionally JOE_LLM_MODEL) or llm.current in config to use Gemini")
		}
		return nil
	}

	// Otherwise switch to whichever key is present, preferring Claude on a tie.
	if hasAnthropic {
		c.selectClaude()
		slog.Info("no Gemini key found; auto-selected Claude from ANTHROPIC_API_KEY", "model", defaultLLMModel)
	} else {
		c.selectGemini()
		slog.Info("no ANTHROPIC_API_KEY found; auto-selected Gemini from the available key", "model", defaultGeminiModel)
	}
	return nil
}

// selectClaude points the active model at the Claude default.
func (c *Config) selectClaude() {
	c.setCurrentModel(defaultLLMCurrent, ModelConfig{Provider: providerClaude, Model: defaultLLMModel})
}

// selectGemini points the active model at the Gemini default.
func (c *Config) selectGemini() {
	c.setCurrentModel(defaultGeminiCurrent, ModelConfig{Provider: providerGemini, Model: defaultGeminiModel})
}

// setCurrentModel sets the active model key and its config, creating the
// Available map if needed.
func (c *Config) setCurrentModel(key string, mc ModelConfig) {
	if c.LLM.Available == nil {
		c.LLM.Available = make(map[string]ModelConfig)
	}
	c.LLM.Current = key
	c.LLM.Available[key] = mc
}

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
	case providerOpenAICompat:
		// The generic OpenAI-compatible provider gates on BaseURL presence,
		// NOT on an API key: OPENAI_API_KEY is optional (empty is valid for
		// keyless local endpoints), but the endpoint base URL is required.
		if mc.BaseURL == "" {
			return fmt.Errorf("base_url is required for the %s provider (set base_url on the model config)", providerOpenAICompat)
		}
	default:
		return fmt.Errorf("unsupported LLM provider: %s", mc.Provider)
	}
	return nil
}

// ValidateCostCurrency validates the LLM cost-recording currency
// configuration (Stream G phase G1). Two rules:
//
//  1. LLMConfig.Currency must be one of the allowed values (USD, EUR).
//     An empty string is treated as the default (USD) to keep configs
//     that omit the field working — defaulting is handled by Load via
//     defaultConfig, but the validator stays permissive of an empty
//     value so config produced by older Save() calls is still accepted.
//  2. When Currency is not USD, USDToConfiguredRate must be present and
//     positive (> 0). The rate is the (later) recorder's USD→currency
//     conversion factor; a zero or negative rate would either store all
//     costs as 0 or invert the sign, both clearly wrong. When Currency
//     IS USD the rate is ignored (implicitly 1.0) and not validated.
//
// G1 wires this validator's definition only. Later phases (the recorder,
// the cost gate, the settings service) call it; no in-tree caller invokes
// it this phase.
func ValidateCostCurrency(lc LLMConfig) error {
	cur := lc.Currency
	if cur == "" {
		cur = defaultCurrency
	}
	allowed := false
	for _, c := range AllowedCurrencies {
		if cur == c {
			allowed = true
			break
		}
	}
	if !allowed {
		return fmt.Errorf("llm.currency = %q is not in the allowed set %v", lc.Currency, AllowedCurrencies)
	}
	if cur != CurrencyUSD && lc.USDToConfiguredRate <= 0 {
		return fmt.Errorf("llm.usd_to_configured_rate must be positive when llm.currency is not %s (got %g for currency %q)", CurrencyUSD, lc.USDToConfiguredRate, cur)
	}
	return nil
}

// ValidateAPIKeysWithUserMessage validates API keys and returns a user-friendly error message.
// This is suitable for CLI output where we want to show detailed setup instructions.
func ValidateAPIKeysWithUserMessage(mc ModelConfig) error {
	// Check if provider is supported
	supportedProviders := []string{providerClaude, providerGemini, providerOpenAICompat}
	providerSupported := false
	for _, p := range supportedProviders {
		if mc.Provider == p {
			providerSupported = true
			break
		}
	}

	if !providerSupported {
		return fmt.Errorf("you need to connect Joe to an LLM.\n\nCurrently supported LLMs:\n  - Claude (Anthropic)\n  - Gemini (Google)\n  - Any OpenAI-compatible endpoint (provider 'openai-compat' + base_url)\n\nConfigured provider '%s' is not supported", mc.Provider)
	}

	// Check for API keys
	switch mc.Provider {
	case providerClaude:
		apiKey := os.Getenv(env.AnthropicAPIKey)
		if apiKey == "" {
			return fmt.Errorf("you need to connect Joe to an LLM.\n\nClaude is configured but %s is not set or is empty.\n\nCurrently supported LLMs:\n  - Claude (Anthropic) - requires %s\n  - Gemini (Google) - requires %s or %s\n\nTo use Claude:\n  export %s=your-api-key-here\n\nTo use Gemini, update your config to use a Gemini model",
				env.AnthropicAPIKey, env.AnthropicAPIKey, env.GeminiAPIKey, env.GoogleAPIKey, env.AnthropicAPIKey)
		}
	case providerGemini:
		geminiKey := os.Getenv(env.GeminiAPIKey)
		googleKey := os.Getenv(env.GoogleAPIKey)
		if geminiKey == "" && googleKey == "" {
			return fmt.Errorf("you need to connect Joe to an LLM.\n\nGemini is configured but neither %s nor %s is set or both are empty.\n\nCurrently supported LLMs:\n  - Claude (Anthropic) - requires %s\n  - Gemini (Google) - requires %s or %s\n\nTo use Gemini:\n  export %s=your-api-key-here\n\nTo use Claude, update your config to use a Claude model",
				env.GeminiAPIKey, env.GoogleAPIKey, env.AnthropicAPIKey, env.GeminiAPIKey, env.GoogleAPIKey, env.GeminiAPIKey)
		}
	case providerOpenAICompat:
		// The key is optional; only the base URL is required for this provider.
		if mc.BaseURL == "" {
			return fmt.Errorf("you need to connect Joe to an LLM.\n\nThe 'openai-compat' provider is configured but no base_url is set.\n\nSet base_url to your OpenAI-compatible endpoint (e.g. http://localhost:11434/v1).\n%s is optional and only sent when set (keyless local endpoints are supported)", env.OpenAIAPIKey)
		}
	}

	return nil
}
