package config

const (
	// Default server configuration.
	defaultServerAddress = "localhost:7777"

	// Default LLM configuration.
	defaultLLMCurrent = "claude-sonnet"
	defaultLLMModel   = "claude-sonnet-4-20250514"

	// Default Gemini configuration. Used when auto-selecting Gemini for a user
	// who has a Gemini key but no Anthropic key and expressed no explicit
	// provider preference. Kept consistent with internal/llm/gemini.DefaultModel.
	defaultGeminiCurrent = "gemini-flash"
	defaultGeminiModel   = "gemini-2.5-flash"

	// Default refresh intervals.
	defaultRefreshIntervalMinutes = 5
	defaultMaxCallsPerHour        = 100
	defaultBatchThreshold         = 10
	defaultBatchTimeoutSec        = 30

	// Default notification thresholds.
	defaultDesktopThreshold = "medium"
	defaultSlackThreshold   = "high"

	// Default quiet hours.
	defaultQuietStart    = "22:00"
	defaultQuietEnd      = "08:00"
	defaultQuietTimezone = "Local"

	// Provider names.
	providerClaude = "claude"
	providerGemini = "gemini"

	// Default knowledge configuration.
	defaultKnowledgeSemanticTopK  = 5
	defaultKnowledgeMinConfidence = float32(0.0)

	// LLM cost-recording currency. Stream G phase G1: the configured
	// currency that LLM cost rows (llm_usage.currency) are stamped with
	// at record time, and that cost caps (llm_cost_limits) are
	// denominated in. The initial allowed set is USD + EUR; default is
	// USD. The recorder, the cost gate, and the settings service all
	// land in later phases — only the field, the default, and the
	// validation rule live here in G1.
	CurrencyUSD     = "USD"
	CurrencyEUR     = "EUR"
	defaultCurrency = CurrencyUSD
)

// AllowedCurrencies enumerates the initial allowed-currency set for the
// LLM cost-recording configuration (LLMConfig.Currency). Exposed so a UI or
// admin tool can enumerate the same set the validator enforces.
var AllowedCurrencies = []string{CurrencyUSD, CurrencyEUR}
