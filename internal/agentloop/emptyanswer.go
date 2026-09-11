package agentloop

import "strings"

// The empty-answer gate's outcome, recorded on the session so a turn the gate
// touched is distinguishable from one it never needed to.
//
// The gate (see Agent.Run) keys on an `answer` terminal turn whose
// operator-facing prose is empty — the turn joe-pm
// queue/empty-answer-turn-accepted.md observed: six iterations, five actions, a
// complete declared conclusion, and the empty string for the operator. It is the
// counterpart of the zero-action question gate and states the opposite fact:
// not "you have not looked", but "you have not told the operator".
//
// It fires at most once per session. "Fired and did not hold" is a real outcome
// and must be legible: the second empty answer is RETURNED as it stands rather
// than looped or filled in, and the fact that it was is recorded here.
const (
	// EmptyAnswerGateHeld marks a session where the gate fired and the model
	// did not go on to return another empty answer.
	EmptyAnswerGateHeld = "held"

	// EmptyAnswerGateNotHeld marks a session where the gate fired and the
	// session again ended on an `answer` with empty prose. That turn was
	// returned to the operator as it stood.
	EmptyAnswerGateNotHeld = "not_held"
)

// operatorProseEmpty reports whether a terminal turn's content leaves the
// operator nothing to read. It is called on content joe's plumbing has ALREADY
// been stripped from — the turn-kind marker and the declaration block — so
// emptiness is a property of what reaches the operator rather than of the raw
// model output: a turn made only of markers is empty, and so is one carrying
// only whitespace.
func operatorProseEmpty(content string) bool {
	return strings.TrimSpace(content) == ""
}
