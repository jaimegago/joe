package agentloop

import "github.com/jaimegago/joe/internal/llm"

// RunObserver receives step-by-step notifications during Agent.Run().
// Implementations must be safe for use from a single goroutine (the agent loop).
type RunObserver interface {
	OnStep(step StepRecord)
}

// StepRecord captures one iteration of the agentic loop.
type StepRecord struct {
	StepNumber  int
	LLMRequest  LLMRequestSummary
	LLMResponse LLMResponseSummary
	ToolResults []ToolResultRecord // nil when the LLM returned no tool calls
}

// LLMRequestSummary is a lightweight summary of what was sent to the LLM.
// Full message contents are omitted to avoid excessive memory use.
type LLMRequestSummary struct {
	MessageCount   int
	ToolsAvailable []string
}

// LLMResponseSummary captures the LLM's response for one iteration.
type LLMResponseSummary struct {
	Content   string
	ToolCalls []llm.ToolCall
	Usage     llm.TokenUsage
}

// ToolResultRecord captures the result of executing a single tool call.
type ToolResultRecord struct {
	ID         string
	Name       string
	Result     any
	Error      string // empty on success
	ErrorCode  string // stable tool-failure code from the injected classifier (api.classifyWriteFailure), empty otherwise
	DurationMs int
}

// WithObserver sets an optional observer that receives step notifications
// during Agent.Run(). If nil, no notifications are emitted.
func WithObserver(o RunObserver) AgentOption {
	return func(a *Agent) { a.observer = o }
}

// SliceObserver collects all StepRecords into a slice. Safe for single-goroutine use.
type SliceObserver struct {
	Steps []StepRecord
}

// OnStep appends the step to the collected slice.
func (o *SliceObserver) OnStep(step StepRecord) {
	o.Steps = append(o.Steps, step)
}
