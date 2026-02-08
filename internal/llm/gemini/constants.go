package gemini

import "time"

// Model name constants for Gemini provider.
const (
	// DefaultModel is the default Gemini model if none is specified.
	DefaultModel = "gemini-2.5-flash"

	// Minimum valid API key length (exclude test/placeholder values).
	minAPIKeyLength = 20

	// Timeout for internal operations.
	contextTimeout = 3 * time.Second
)
