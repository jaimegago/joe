package agentloop

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/jaimegago/joe/internal/llm"
)

// Token estimation heuristic (centralised so tests target it directly).
//
// Joe does NOT call a provider tokenizer for pruning decisions — that would
// add a network round-trip and a provider dependency to every prune. Instead
// it estimates a message's token cost as the total character length of its
// content, its tool-call names + JSON-encoded arguments, divided by 4 and
// rounded up. Four characters per token is the conventional rough English
// ratio; pruning only needs a stable over/under estimate that orders
// messages consistently, not exact provider accounting (the runaway ceiling
// and cost gate use real provider Usage numbers — this estimate governs
// history pruning only).
//
// A tool-result message carries its result payload in Content (see
// tools.ResultToMessage, which marshals the result to JSON and stores it
// there), so counting Content alone already covers tool results.

// EstimateMessageTokens returns the estimated token cost of a single
// message: its content plus, for an assistant tool-call message, each tool
// call's name and JSON-encoded arguments.
func EstimateMessageTokens(msg llm.Message) int {
	chars := len(msg.Content)
	for _, tc := range msg.ToolCalls {
		chars += len(tc.Name)
		if b, err := json.Marshal(tc.Args); err == nil {
			chars += len(b)
		}
	}
	return ceilDiv(chars, 4)
}

// EstimateMessagesTokens sums EstimateMessageTokens over a message slice.
func EstimateMessagesTokens(msgs []llm.Message) int {
	total := 0
	for i := range msgs {
		total += EstimateMessageTokens(msgs[i])
	}
	return total
}

// EstimateOverheadTokens estimates the fixed per-request overhead that is
// NOT part of the prunable message history: the system prompt string and
// the tool definitions sent on every request. Uses the same char/4 heuristic
// so the overhead reservation is denominated consistently with the message
// estimates it is subtracted alongside.
func EstimateOverheadTokens(systemPrompt string, tools []llm.ToolDefinition) int {
	chars := len(systemPrompt)
	for _, t := range tools {
		chars += len(t.Name) + len(t.Description)
		if b, err := json.Marshal(t.Parameters); err == nil {
			chars += len(b)
		}
	}
	return ceilDiv(chars, 4)
}

// ComputeInputTokenBudget derives the per-turn input token budget the
// session prunes history to:
//
//	floor(windowTokens * fraction) - maxOutputTokens - overheadTokens
//
// i.e. a fraction of the model's context window, minus the output the loop
// reserves for the response (the table's max-output, the same value the loop
// caps requests at), minus the estimated fixed system-prompt + tool overhead.
// The result may be non-positive on a pathologically small window; callers
// clamp to a positive floor before setting it on the session so token
// pruning still engages.
func ComputeInputTokenBudget(windowTokens, maxOutputTokens, overheadTokens int, fraction float64) int {
	usable := int(math.Floor(float64(windowTokens) * fraction))
	return usable - maxOutputTokens - overheadTokens
}

// truncationMarkerFmt is the explicit marker inserted in place of the elided
// middle of a truncated message. It names the exact number of characters
// dropped and tells the model how to recover the full output, so the
// truncation is unmistakable rather than a silent cut. The %d is the elided
// character count.
const truncationMarkerFmt = "\n\n[... %d characters omitted to fit the context budget — re-invoke the tool with a narrower query for the full output ...]\n\n"

// truncationHeadNumerator / truncationSplitDenominator express the head share
// of the kept span: head ≈ 60%, tail ≈ 40%. Infra tool output carries its
// identity up front (resource names, kinds) and its status/errors at the end,
// so a head-heavy split preserves the most diagnostic regions.
const (
	truncationHeadNumerator    = 6
	truncationSplitDenominator = 10
)

// TruncateContent bounds a single message's content to tokenCap estimated
// tokens, using the same chars/4 heuristic as EstimateMessageTokens so the
// cap is denominated consistently with the budget it derives from. Content
// already within the cap is returned unchanged with didTruncate=false.
//
// Otherwise the head (~60%) and tail (~40%) of the budgeted character span are
// kept and the elided middle is replaced with truncationMarkerFmt naming how
// many characters were dropped. The marker length is reserved using an
// upper-bound digit count (the full content length), so the actual marker —
// which names the real, smaller elided count — is never longer than reserved
// and the result stays at or under the cap.
//
// This is the single centralised ingestion-truncation primitive; callers
// apply it before a message enters session history (see Session.truncate*),
// never to messages already in history.
func TruncateContent(content string, tokenCap int) (truncated string, didTruncate bool) {
	if tokenCap < 1 {
		tokenCap = 1
	}
	if ceilDiv(len(content), 4) <= tokenCap {
		return content, false
	}
	charCap := tokenCap * 4
	reserve := len(fmt.Sprintf(truncationMarkerFmt, len(content)))
	keep := charCap - reserve
	if keep < 0 {
		keep = 0
	}
	// keep < len(content) always holds here: keep <= charCap < len(content)
	// (content is over the cap), so the elided count below is positive.
	head := keep * truncationHeadNumerator / truncationSplitDenominator
	tail := keep - head
	elided := len(content) - head - tail
	var b strings.Builder
	b.Grow(head + reserve + tail)
	b.WriteString(content[:head])
	b.WriteString(fmt.Sprintf(truncationMarkerFmt, elided))
	b.WriteString(content[len(content)-tail:])
	return b.String(), true
}

func ceilDiv(n, d int) int {
	if n <= 0 || d <= 0 {
		return 0
	}
	return (n + d - 1) / d
}
