package agentloop

import "strings"

// TurnKind is the declared shape of a terminal turn — the turn on which the
// loop stops calling tools and returns to the operator.
//
// A turn that ends in prose is invisible to anything keyed on actions: the run
// evidence records which tools ran, and a turn that ran none and said words
// instead leaves nothing behind that distinguishes an answer from a question
// from a refusal. Classifying that prose after the fact means reading it, which
// is what the declaration exists to avoid.
//
// The vocabulary is CLOSED at exactly three values. A fourth — "undeclared",
// "unknown" — would make the field ambiguous in exactly the way the kind exists
// to remove, so a turn whose marker is missing or unparseable still carries one
// of the three (TurnKindAnswer, the neutral case) and records the absence
// separately via Session.TurnKindDeclared. Kind and declaredness are two facts,
// not one.
type TurnKind string

const (
	// TurnKindAnswer is a turn returning a diagnosis, finding, or result.
	// It is also the value a terminal turn carries when the model declared
	// nothing — see Session.TurnKindDeclared for telling those apart.
	TurnKindAnswer TurnKind = "answer"

	// TurnKindQuestion is a turn returning a request for information from
	// the operator. It is the only kind the zero-action gate keys on.
	TurnKindQuestion TurnKind = "question"

	// TurnKindRefusal is a turn declining to continue. joe already owes an
	// articulated safety concern when it refuses (prompts.TaskSystem, SAFETY
	// REASONING); the kind makes the refusal itself machine-visible without
	// anyone having to classify that articulation.
	TurnKindRefusal TurnKind = "refusal"
)

// ParseTurnKind maps a declared value to the closed vocabulary. The second
// return is false for anything outside it — an unrecognised value is not a
// declaration, and is treated exactly as a missing one.
func ParseTurnKind(s string) (TurnKind, bool) {
	switch TurnKind(strings.ToLower(strings.TrimSpace(s))) {
	case TurnKindAnswer:
		return TurnKindAnswer, true
	case TurnKindQuestion:
		return TurnKindQuestion, true
	case TurnKindRefusal:
		return TurnKindRefusal, true
	}
	return "", false
}

// turnKindMarker is the line prefix the model emits to declare its terminal
// turn's kind, per the TERMINAL TURN clause in prompts.TaskSystem. It is
// matched case-insensitively and after stripping the emphasis characters a
// model tends to wrap a lone line in.
const turnKindMarker = "turn-kind:"

// SplitTurnKind separates a declared turn kind from the prose it was emitted
// alongside. It returns the kind, whether the model actually declared it, and
// the content with the marker line removed.
//
// The marker is looked for on ANY line rather than only the last, and the LAST
// matching line wins. Models append trailing pleasantries after a line they
// were told to put at the end often enough that anchoring strictly to the final
// line would drop the declaration and silently fall back to the default — which
// reads, downstream, as a model that never declares.
//
// An undeclared or unparseable turn yields (TurnKindAnswer, false, content
// unchanged). Defaulting to `answer` rather than to `question` is deliberate:
// the zero-action gate re-enters the loop on `question`, and a gate that fires
// because a model failed to emit a marker would spend a whole extra LLM round
// trip on a formatting slip.
func SplitTurnKind(content string) (TurnKind, bool, string) {
	lines := strings.Split(content, "\n")
	found := -1
	var kind TurnKind
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		// A model told to emit a bare line will sometimes wrap it in the
		// emphasis or code markers it uses for every other standalone line.
		trimmed = strings.Trim(trimmed, "*_`# ")
		if len(trimmed) < len(turnKindMarker) {
			continue
		}
		if !strings.EqualFold(trimmed[:len(turnKindMarker)], turnKindMarker) {
			continue
		}
		k, ok := ParseTurnKind(strings.Trim(trimmed[len(turnKindMarker):], "*_`\" "))
		if !ok {
			continue
		}
		found, kind = i, k
	}
	if found < 0 {
		return TurnKindAnswer, false, content
	}
	rest := append(append([]string{}, lines[:found]...), lines[found+1:]...)
	return kind, true, strings.TrimSpace(strings.Join(rest, "\n"))
}

// The zero-action question gate's outcome, recorded on the session so a run
// that fired it is not indistinguishable from a run that never needed to.
//
// The gate fires at most once per session (see Agent.Run). "Fired and did not
// hold" is a real outcome and must be legible: an unbounded re-entry gate is a
// hang, and a hang is a worse failure than the question it was preventing, so
// the second zero-action question is RETURNED rather than looped — and the fact
// that it was is recorded here rather than swallowed.
const (
	// ZeroActionQuestionGateHeld marks a session where the gate fired and the
	// model did not go on to return another zero-action question — it acted,
	// answered, or refused instead.
	ZeroActionQuestionGateHeld = "held"

	// ZeroActionQuestionGateNotHeld marks a session where the gate fired and
	// the re-entered turn was again a zero-action question. That question was
	// returned to the operator as it stood; the loop did not re-enter twice.
	ZeroActionQuestionGateNotHeld = "not_held"
)
