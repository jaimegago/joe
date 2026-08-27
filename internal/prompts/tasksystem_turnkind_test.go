package prompts

import (
	"strings"
	"testing"
)

// TestTaskSystem_DeclaresTerminalTurnKind pins the clause that makes the kind
// exist at all. The kind is MODEL-DECLARED — nothing derives it from the prose
// — so the parser in agentloop has nothing to read unless this text is in the
// system prompt. A clause deleted here would not fail a single agentloop test:
// those feed the marker in directly, and the loop would go on defaulting every
// live turn to `answer` while every test stayed green.
func TestTaskSystem_DeclaresTerminalTurnKind(t *testing.T) {
	for _, required := range []string{
		// The literal the parser matches. agentloop.turnKindMarker is
		// "turn-kind:", matched case-insensitively.
		"TURN-KIND:",
		// The closed vocabulary, each named with what it is for.
		"answer —",
		"question —",
		"refusal —",
		// Every terminal turn, not only the interesting ones — the property
		// that makes an absent kind unambiguous.
		"including an ordinary answer",
	} {
		if !strings.Contains(TaskSystem, required) {
			t.Errorf("TaskSystem must carry the terminal-turn-kind clause (missing %q)", required)
		}
	}
}

// TestTaskSystem_TurnKindVocabularyIsClosed guards the closure at the prompt
// end. agentloop.ParseTurnKind rejects anything outside the three values, so a
// fourth word offered to the model here would produce turns joe silently
// defaults to `answer` — a model doing exactly as instructed, and a kind that
// means nothing.
//
// The assertion is over the clause's VOCABULARY BULLETS ("- <value> — ...")
// rather than over its prose, because the prose legitimately contains words
// like "other" that are not on offer as values.
func TestTaskSystem_TurnKindVocabularyIsClosed(t *testing.T) {
	clause := TaskSystem[strings.Index(TaskSystem, "TERMINAL TURN"):]
	var offered []string
	for _, line := range strings.Split(clause, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		value, _, ok := strings.Cut(strings.TrimPrefix(line, "- "), " — ")
		if !ok {
			t.Errorf("vocabulary bullet %q is not in the form \"- <value> — <gloss>\"", line)
			continue
		}
		offered = append(offered, strings.TrimSpace(value))
	}
	want := []string{"answer", "question", "refusal"}
	if len(offered) != len(want) {
		t.Fatalf("clause offers %v, want exactly %v", offered, want)
	}
	for i, v := range want {
		if offered[i] != v {
			t.Errorf("vocabulary value %d = %q, want %q", i, offered[i], v)
		}
	}
}

// TestZeroActionQuestionReentry_ClaimsOnlyWhatTheGateKnows is the D-0101 guard
// applied to the re-entry instruction.
//
// The design session chose the zero-action gate over a broader one precisely
// because "you have not looked yet" is a fact about the session, while "the
// operator could not have told you anything you do not already have" is a
// judgement about the world. The prompt must not assert the second: a model
// that meets the genuinely unanswerable case — an intent, a decision that is
// the operator's — would be argued out of the one question it should ask.
func TestZeroActionQuestionReentry_ClaimsOnlyWhatTheGateKnows(t *testing.T) {
	for _, required := range []string{
		// The narrow claim, stated as the fact it is.
		"You have not looked yet",
		// The escape hatch that keeps a legitimate question legitimate.
		"no tool of yours can obtain",
	} {
		if !strings.Contains(ZeroActionQuestionReentry, required) {
			t.Errorf("ZeroActionQuestionReentry must make only the narrow claim (missing %q)", required)
		}
	}
	// It re-enters the loop; it must not read as a refusal of the question.
	for _, forbidden := range []string{
		"do not ask",
		"never ask",
	} {
		if strings.Contains(strings.ToLower(ZeroActionQuestionReentry), forbidden) {
			t.Errorf("ZeroActionQuestionReentry forbids the question outright (%q); it may only require that it come after looking", forbidden)
		}
	}
}
