package claude

// Model name constants for Claude provider.
const (
	// DefaultModel is the default Claude model if none is specified.
	DefaultModel = "claude-sonnet-4-20250514"

	// Default token limit for Claude requests.
	defaultMaxTokens = 4096
)

// SuggestedModels returns a list of common Claude model names.
// Used in error messages to help users choose valid models.
func SuggestedModels() []string {
	return []string{
		"claude-sonnet-4-20250514",
		"claude-opus-4-20241229",
		"claude-3-5-sonnet-20241022",
		"claude-3-5-haiku-20241022",
	}
}
