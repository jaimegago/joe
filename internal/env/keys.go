package env

// Environment variable names used across multiple packages.
// Defined here to ensure consistency and avoid typos.
const (
	// LLM API Keys
	AnthropicAPIKey = "ANTHROPIC_API_KEY"
	GeminiAPIKey    = "GEMINI_API_KEY"
	GoogleAPIKey    = "GOOGLE_API_KEY"

	// System
	Home = "HOME"

	// Mode selects Joe's boot posture for the write floor (D-0018). When set to
	// ModeObservation, Joe boots with the write floor up (reason observation): a
	// hard read-only floor that is the intended resting posture, distinct from
	// panic/safe mode. Unset or any other value leaves the floor down (RBAC then
	// governs writes), unless a sticky panic state forces safe mode regardless.
	// Read once at boot in cmd/joe/server.go, consistent with the other JOE_*
	// boot env vars.
	Mode = "JOE_MODE"
)

// Values for the Mode env var.
const (
	// ModeObservation raises the write floor with reason observation at boot.
	ModeObservation = "observation"
)
