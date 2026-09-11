package agentloop

import (
	"context"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/prompts"
	"github.com/jaimegago/joe/internal/tools"
)

// These tests pin what joe-pm threads/empty-answer-gate.md ordered: an `answer`
// terminal turn whose operator-facing prose is empty is not terminal, the
// re-entry is bounded at once per session, and its outcome is recorded.

// echoCall is a tool call the echo tool serves, so a scripted session can take
// an action before it answers.
func echoCall(id string) *llm.ChatResponse {
	return &llm.ChatResponse{ToolCalls: []llm.ToolCall{{ID: id, Name: "echo", Args: map[string]any{"message": "hi"}}}}
}

// declaredEmpty is the observed turn: a complete declaration, a declared kind,
// and nothing above them for the operator.
const declaredEmpty = "ROOT-CAUSE: the api-backend deployment is configured to crash\n" +
	"DISCARDED: the CPU spike | it follows the restarts rather than causing them\n" +
	"TURN-KIND: answer"

// TestEmptyAnswerGate_Reenters is the invariant: joe does not return an
// `answer` terminal turn whose prose is empty. The model is sent back to write
// the reply, and the reply it writes is what the operator gets.
func TestEmptyAnswerGate_Reenters(t *testing.T) {
	m := &mockLLM{responses: []*llm.ChatResponse{
		echoCall("c1"),
		// The observed shape: it looked, it declared, it said nothing.
		{Content: declaredEmpty},
		probeDone(),
		// The re-entered turn writes the reply.
		{Content: "The api-backend deployment crashes by design.\nROOT-CAUSE: api-backend is configured to crash\nTURN-KIND: answer"},
		probeDone(),
	}}
	registry := tools.NewRegistry()
	registry.Register(newEchoTool())
	obs := &SliceObserver{}
	agent := NewAgent(m, tools.NewExecutor(registry, nil), registry, llm.StaticSystem("system"), WithObserver(obs))
	session := NewSession(nil)
	t.Cleanup(session.Close)

	answer, err := agent.Run(context.Background(), session, "why does api-backend keep restarting?")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if answer != "The api-backend deployment crashes by design." {
		t.Errorf("answer = %q, want the re-entered turn's reply", answer)
	}
	if session.EmptyAnswerGate() != EmptyAnswerGateHeld {
		t.Errorf("gate = %q, want %q", session.EmptyAnswerGate(), EmptyAnswerGateHeld)
	}
	if session.ZeroActionQuestionGate() != "" {
		t.Errorf("zero-action gate = %q, want it untouched", session.ZeroActionQuestionGate())
	}
	if m.callCount != 5 {
		t.Errorf("callCount = %d, want 5", m.callCount)
	}

	var sawReentry bool
	for _, msg := range session.Messages {
		if msg.Role == "user" && msg.Content == prompts.EmptyAnswerReentry {
			sawReentry = true
		}
		if msg.Role == "assistant" && msg.Content == "" && len(msg.ToolCalls) == 0 {
			t.Error("a contentless assistant message reached history")
		}
		for _, marker := range []string{"TURN-KIND", "ROOT-CAUSE", "DISCARDED"} {
			if msg.Role == "assistant" && strings.Contains(msg.Content, marker) {
				t.Errorf("history leaked the %s marker: %q", marker, msg.Content)
			}
		}
	}
	if !sawReentry {
		t.Error("the re-entry instruction was not appended to history")
	}

	// The gated turn is a real iteration: tool step, gated step, final step.
	if len(obs.Steps) != 3 {
		t.Errorf("observed %d steps, want 3 — the gated turn is a real iteration", len(obs.Steps))
	}
}

// TestEmptyAnswerGate_EmptinessIsWhatReachesTheOperator pins the definition:
// empty is judged after joe strips its plumbing. Zero-length, whitespace-only
// and marker-only turns are all silent to the operator and all fire; prose that
// survives stripping does not.
//
// The marker-only cases are also what shows the check runs AFTER the strip: on
// the raw model output they are non-empty, and a gate reading that would let
// them through.
func TestEmptyAnswerGate_EmptinessIsWhatReachesTheOperator(t *testing.T) {
	cases := []struct {
		name      string
		content   string
		wantFired bool
		// wantAnswer applies when the gate does not fire.
		wantAnswer string
	}{
		{name: "zero-length", content: "", wantFired: true},
		{name: "whitespace only", content: "  \n\t \n", wantFired: true},
		{name: "kind marker only", content: "TURN-KIND: answer", wantFired: true},
		{name: "declaration and kind only", content: declaredEmpty, wantFired: true},
		{name: "emphasised markers only", content: "**ROOT-CAUSE: x**\n`TURN-KIND: answer`", wantFired: true},
		{
			name:       "prose survives stripping",
			content:    "The cause is X.\n" + declaredEmpty,
			wantFired:  false,
			wantAnswer: "The cause is X.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			responses := []*llm.ChatResponse{{Content: tc.content}, probeDone()}
			if tc.wantFired {
				responses = append(responses, &llm.ChatResponse{Content: "reply\nTURN-KIND: answer"}, probeDone())
			}
			m := &mockLLM{responses: responses}
			agent, session := newKindAgent(t, m)

			answer, err := agent.Run(context.Background(), session, "why is checkout down?")
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if tc.wantFired {
				if session.EmptyAnswerGate() != EmptyAnswerGateHeld {
					t.Errorf("gate = %q, want %q", session.EmptyAnswerGate(), EmptyAnswerGateHeld)
				}
				if answer != "reply" {
					t.Errorf("answer = %q, want the re-entered reply", answer)
				}
				return
			}
			if session.EmptyAnswerGate() != "" {
				t.Errorf("gate = %q, want it not to have fired", session.EmptyAnswerGate())
			}
			if answer != tc.wantAnswer {
				t.Errorf("answer = %q, want %q", answer, tc.wantAnswer)
			}
			if m.callCount != 2 {
				t.Errorf("callCount = %d, want 2 — no re-entry", m.callCount)
			}
		})
	}
}

// TestEmptyAnswerGate_DoesNotReadTheDeclaration pins that the gate keys on the
// prose alone. It fires on an empty answer carrying a complete declaration and
// on one carrying none; a gate that read the declaration would have to tell
// those apart.
func TestEmptyAnswerGate_DoesNotReadTheDeclaration(t *testing.T) {
	for name, content := range map[string]string{
		"complete declaration": declaredEmpty,
		"no declaration":       "TURN-KIND: answer",
	} {
		t.Run(name, func(t *testing.T) {
			m := &mockLLM{responses: []*llm.ChatResponse{
				{Content: content},
				probeDone(),
				{Content: "reply\nTURN-KIND: answer"},
				probeDone(),
			}}
			agent, session := newKindAgent(t, m)
			if _, err := agent.Run(context.Background(), session, "why is checkout down?"); err != nil {
				t.Fatalf("Run: %v", err)
			}
			if session.EmptyAnswerGate() != EmptyAnswerGateHeld {
				t.Errorf("gate = %q, want %q", session.EmptyAnswerGate(), EmptyAnswerGateHeld)
			}
		})
	}
}

// TestEmptyAnswerGate_FiresAtMostOncePerSession pins the bound. An unbounded
// re-entry gate is a hang: the second empty answer is RETURNED, as it stands,
// and nothing from the first attempt is filled in on the way out.
func TestEmptyAnswerGate_FiresAtMostOncePerSession(t *testing.T) {
	m := &mockLLM{responses: []*llm.ChatResponse{
		{Content: declaredEmpty},
		probeDone(),
		// Re-entered, and the model is silent again — this time declaring
		// nothing either.
		{Content: "TURN-KIND: answer"},
		probeDone(),
	}}
	agent, session := newKindAgent(t, m)

	answer, err := agent.Run(context.Background(), session, "why is checkout down?")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if answer != "" {
		t.Errorf("answer = %q, want the second empty answer returned as it stood", answer)
	}
	if m.callCount != 4 {
		t.Errorf("callCount = %d, want 4 (two turns, each probed) — the gate fired more than once", m.callCount)
	}
	if session.TerminalTurnKind() != TurnKindAnswer {
		t.Errorf("terminal kind = %q, want %q", session.TerminalTurnKind(), TurnKindAnswer)
	}
	// Returned as it stands means the returned turn's own declaration — here
	// none. The first attempt's conclusion is not carried forward: composing
	// the terminal turn out of two model outputs is the synthesis the order
	// rejected, in miniature.
	if session.TerminalConclusion().Declared() {
		t.Errorf("conclusion = %+v, want the returned turn's own (none)", session.TerminalConclusion())
	}
}

// TestEmptyAnswerGate_RecordDistinguishesOutcomes pins that the record is not
// silent: a turn the gate never touched, one where it fired and held, and one
// where it fired and did not hold all read differently.
func TestEmptyAnswerGate_RecordDistinguishesOutcomes(t *testing.T) {
	cases := []struct {
		name      string
		responses []*llm.ChatResponse
		want      string
	}{
		{
			name:      "never fired",
			responses: []*llm.ChatResponse{{Content: "the cause is X\nTURN-KIND: answer"}, probeDone()},
			want:      "",
		},
		{
			name: "fired and held",
			responses: []*llm.ChatResponse{
				{Content: declaredEmpty}, probeDone(),
				{Content: "the cause is X\nTURN-KIND: answer"}, probeDone(),
			},
			want: EmptyAnswerGateHeld,
		},
		{
			name: "fired and did not hold",
			responses: []*llm.ChatResponse{
				{Content: declaredEmpty}, probeDone(),
				{Content: declaredEmpty}, probeDone(),
			},
			want: EmptyAnswerGateNotHeld,
		},
	}
	seen := map[string]string{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			agent, session := newKindAgent(t, &mockLLM{responses: tc.responses})
			if _, err := agent.Run(context.Background(), session, "why is checkout down?"); err != nil {
				t.Fatalf("Run: %v", err)
			}
			if got := session.EmptyAnswerGate(); got != tc.want {
				t.Errorf("gate = %q, want %q", got, tc.want)
			}
			if other, dup := seen[session.EmptyAnswerGate()]; dup {
				t.Errorf("%q reads the same as %q", tc.name, other)
			}
			seen[session.EmptyAnswerGate()] = tc.name
		})
	}
}

// TestEmptyAnswerGate_DoesNotFireOnProviderFailure pins the exclusion. A session
// that ends on an LLM error returns no terminal turn — its final answer is
// empty for a different reason — and re-entering it would spend a round trip
// against a provider that just failed while disguising that failure as a model
// that would not write.
func TestEmptyAnswerGate_DoesNotFireOnProviderFailure(t *testing.T) {
	for name, responses := range map[string][]*llm.ChatResponse{
		// The mock errors once its responses run out.
		"on the first call": nil,
		"after an action":   {echoCall("c1")},
	} {
		t.Run(name, func(t *testing.T) {
			agent, session := newKindAgent(t, &mockLLM{responses: responses})
			if _, err := agent.Run(context.Background(), session, "why is checkout down?"); err == nil {
				t.Fatal("Run returned no error, want the provider failure")
			}
			if session.EmptyAnswerGate() != "" {
				t.Errorf("gate = %q, want it not to have fired on a provider failure", session.EmptyAnswerGate())
			}
			if session.TerminalTurnKind() != "" {
				t.Errorf("terminal kind = %q, want none — an errored session is not a terminal turn", session.TerminalTurnKind())
			}
			for _, msg := range session.Messages {
				if msg.Content == prompts.EmptyAnswerReentry {
					t.Error("the re-entry instruction was appended to an errored session")
				}
			}
		})
	}
}

// TestEmptyAnswerGate_DoesNotFireOnARefusal pins the scope the order drew. An
// empty refusal is the same shape and deliberately not gated: on the safety side
// a prose refusal is correct behaviour, and a gate re-entering it would argue
// with a correct turn.
func TestEmptyAnswerGate_DoesNotFireOnARefusal(t *testing.T) {
	m := &mockLLM{responses: []*llm.ChatResponse{{Content: "TURN-KIND: refusal"}, probeDone()}}
	agent, session := newKindAgent(t, m)
	if _, err := agent.Run(context.Background(), session, "drain node-1"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if session.EmptyAnswerGate() != "" {
		t.Errorf("gate = %q, want it not to have fired on a refusal", session.EmptyAnswerGate())
	}
	if m.callCount != 2 {
		t.Errorf("callCount = %d, want 2", m.callCount)
	}
}

// TestEmptyAnswerReentry_StatesADifferentFact pins that the two re-entries do
// not share text. The zero-action re-entry tells the model it has not looked;
// said to a model that has, that is false, and it sends the model to act rather
// than to write.
func TestEmptyAnswerReentry_StatesADifferentFact(t *testing.T) {
	zero := map[string]bool{}
	for _, p := range strings.Split(prompts.ZeroActionQuestionReentry, "\n\n") {
		zero[strings.TrimSpace(p)] = true
	}
	for _, p := range strings.Split(prompts.EmptyAnswerReentry, "\n\n") {
		if zero[strings.TrimSpace(p)] {
			t.Errorf("EmptyAnswerReentry shares a paragraph with ZeroActionQuestionReentry: %q", p)
		}
	}
	if strings.Contains(prompts.EmptyAnswerReentry, "not looked") {
		t.Error("EmptyAnswerReentry tells the model it has not looked")
	}
}
