package llm

import "errors"

// ErrContextOverflow is the shared sentinel an adapter wraps when the provider
// rejects a request because the prompt/input exceeds the model's maximum
// context length (a request-rejection error, not a transient failure). Each
// adapter classifies its provider's overflow-shaped rejection into this
// sentinel conservatively — only clearly overflow-shaped messages map to it;
// every other rejection stays a generic provider error.
//
// The task-status classifier maps it (via errors.Is, through the adapter's
// error wrapping and the agentic loop's "llm chat failed: %w" wrap) to the
// terminal context_overflow status, distinct from the generic error bucket.
// Detection-and-reporting only: no retry, no automatic budget adjustment.
var ErrContextOverflow = errors.New("llm: context window exceeded")
