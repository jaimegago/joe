package llm

import (
	"context"
	"strings"
)

// LLMAdapter is the interface for AI providers (Claude, OpenAI, Ollama, etc.)
// Joe is AI-agnostic - different providers implement this interface
type LLMAdapter interface {
	// Chat sends a chat request and returns a response
	Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
}

// ChatRequest represents a request to the LLM
type ChatRequest struct {
	System    SystemPrompt
	Messages  []Message
	Tools     []ToolDefinition
	MaxTokens int
}

// SystemSegment is one ordered region of the system prompt, carrying whether
// its bytes are stable.
//
// The seam carries structure, not mechanism. An adapter handed a single
// opaque system string cannot know which region is stable — only the
// assembler that concatenated it knows that — and the one boundary an adapter
// could find unaided, the end of the system prompt, is the wrong one: joe
// appends query-derived skills last, so a breakpoint there would write a fresh
// cache entry every turn and never read one, paying the write premium for zero
// reads. Each adapter decides what to do with the marking; adapters with no
// caching contract ignore it.
type SystemSegment struct {
	// Text is the segment's content, carrying no separator of its own.
	Text string
	// Stable is true when the segment's bytes are a deterministic function of
	// the tool set and the binary — identical across independent
	// constructions within one process AND across process restarts. Anything
	// varying per request, per caller, per query, or with live system state is
	// volatile, and so is anything that can differ across a restart.
	Stable bool
}

// SystemPrompt is the ordered segment list an assembler hands to an adapter.
type SystemPrompt []SystemSegment

// systemSegmentSeparator joins adjacent segments. It reproduces exactly the
// "\n\n" the task handler used when it concatenated these sections into a
// single string, so segmenting the prompt changes no bytes reaching a provider.
const systemSegmentSeparator = "\n\n"

// StaticSystem returns a one-segment stable system prompt. It is the
// constructor for the ordinary case — a system prompt that is a single
// package-level constant, and therefore stable by construction. Empty text
// yields a nil SystemPrompt.
func StaticSystem(text string) SystemPrompt {
	if text == "" {
		return nil
	}
	return SystemPrompt{{Text: text, Stable: true}}
}

// Append adds a segment, dropping empty ones so the rendered string never
// carries a separator around absent content.
func (p SystemPrompt) Append(text string, stable bool) SystemPrompt {
	if text == "" {
		return p
	}
	return append(p, SystemSegment{Text: text, Stable: stable})
}

// String renders the whole system prompt as a provider sees it.
func (p SystemPrompt) String() string {
	return joinSegments(p)
}

// StablePrefix returns the rendered leading run of stable segments — the
// region a prefix cache can key on.
//
// It is the LEADING run rather than every stable segment, because provider
// caching is a byte-exact PREFIX match: a stable segment sitting behind a
// volatile one is part of no stable prefix, however stable its own bytes are.
// Returns "" when the first segment is volatile.
func (p SystemPrompt) StablePrefix() string {
	n := 0
	for _, seg := range p {
		if !seg.Stable {
			break
		}
		n++
	}
	return joinSegments(p[:n])
}

func joinSegments(segs []SystemSegment) string {
	var b strings.Builder
	for i, seg := range segs {
		if i > 0 {
			b.WriteString(systemSegmentSeparator)
		}
		b.WriteString(seg.Text)
	}
	return b.String()
}

// ChatResponse represents a response from the LLM
type ChatResponse struct {
	Content   string
	ToolCalls []ToolCall
	Usage     TokenUsage
}

// Message represents a message in the conversation
type Message struct {
	Role         string     // "user", "assistant"
	Content      string     // Text content
	ToolCalls    []ToolCall // For assistant messages: the tool calls made
	ToolResultID string     // For tool result messages: references the tool call ID
	ToolName     string     // For tool result messages: the tool name (needed by Gemini)
	IsError      bool       // For tool result messages: whether the result is an error
}

// ToolDefinition describes a tool available to the LLM
type ToolDefinition struct {
	Name        string
	Description string
	Parameters  ParameterSchema
}

// ParameterSchema defines the structure of tool parameters
type ParameterSchema struct {
	Type       string
	Properties map[string]Property
	Required   []string
}

// Property defines a single parameter property
type Property struct {
	Type        string
	Description string
	Items       *Property // For array types: describes array items
}

// ToolCall represents a tool call from the LLM
type ToolCall struct {
	ID   string
	Name string
	Args map[string]any
	// ProviderSignature is an opaque provider-issued token bound to THIS tool
	// call, which the provider requires replayed verbatim when the call is
	// echoed back in conversation history. The seam carries it because no
	// other layer can: it is minted by the provider, it is meaningless to
	// joe, and dropping it silently breaks the next turn rather than the
	// current one.
	//
	// The live instance is Gemini 3's thought signature. A thinking model
	// returns one alongside each function call and rejects the follow-up
	// request without it — "Function call is missing a thought_signature in
	// functionCall parts. This is required for tools to work correctly" —
	// which is why joe could not complete a multi-turn tool exchange on any
	// pro-tier Gemini model before this field existed.
	//
	// Adapters with no such contract leave it nil, which is an honest report
	// of "this provider issues nothing to replay", not a missing value. It is
	// deliberately []byte and deliberately undocumented in shape: joe never
	// interprets it, and a typed field would invite a reader to.
	//
	// It is NOT persisted. The agent loop copies ToolCalls into history
	// in-memory within one turn, which is the whole lifetime a signature has
	// to survive; a rehydrated conversation starts a fresh exchange.
	ProviderSignature []byte
}

// TokenUsage tracks token consumption
type TokenUsage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
	// CacheReadTokens is the number of input tokens served from a provider
	// prompt cache, and CacheWriteTokens the number written into one. They are
	// the ONLY observable of cache efficacy — a prefix below the model's
	// minimum cacheable length caches nothing, silently and without an error,
	// so byte-stability on its own does not show the mechanism working.
	//
	// An adapter with no caching contract leaves both zero, which is an honest
	// report of "this provider cached nothing", not a missing measurement.
	//
	// Neither is added into TotalTokens. Anthropic reports cached input
	// separately from InputTokens and prices it differently (a write premium,
	// a read discount), so folding them in would silently change what every
	// existing cost and budget consumer means by a total.
	CacheReadTokens  int
	CacheWriteTokens int
}

// CostNanoUnitsPerUnit is the integer scale used to store LLM call cost in
// the llm_usage.estimated_cost_nano column: one stored unit equals 1e-9 of
// the row's currency (one nano-unit). Multiply a per-token price expressed
// in currency units (e.g. dollars per token) by this constant to convert
// it into the integer storage unit; the inverse (1e-9 of the currency
// unit) is the storage granularity. Storing cost as integers makes
// cost-window SUM aggregation exact on both SQLite and Postgres and
// eliminates floating-point drift. Stream G phase G1: definition only —
// no caller multiplies by this yet.
const CostNanoUnitsPerUnit = 1_000_000_000
