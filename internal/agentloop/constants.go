package agentloop

const (
	// DefaultMaxIterations prevents infinite agentic loops.
	DefaultMaxIterations = 10

	// DefaultMaxMessages limits session history to prevent unbounded growth.
	DefaultMaxMessages = 100
)
