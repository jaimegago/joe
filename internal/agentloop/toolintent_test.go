package agentloop

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/prompts"
	"github.com/jaimegago/joe/internal/tools"
)

// The loop's completion signal is "the model returned no tool calls". That
// signal is ambiguous: a model can narrate the tool call it is about to make and
// then omit the call itself (reproduced on gemini-2.5-flash at roughly one turn
// in six). Read literally, the narration ends the run as a SUCCESS carrying the
// narration as the answer — the user sees Joe announce a step and then stop, with
// no answer and nothing in the logs. probeUnfulfilledToolIntent disambiguates the
// turn before the loop accepts it. These tests pin both directions of that
// disambiguation, plus the guarantee that a genuine answer is never touched.

func newIntentAgent(t *testing.T, m *mockLLM) (*Agent, *Session) {
	t.Helper()
	registry := tools.NewRegistry()
	registry.Register(newEchoTool())
	agent := NewAgent(m, tools.NewExecutor(registry, nil), registry, "system")
	session := NewSession(nil)
	t.Cleanup(session.Close)
	return agent, session
}

// TestProbe_RecoversNarratedToolCall is the reported bug: the model announces a
// tool call in prose and emits no call. Pre-fix the loop returned that narration
// as the final answer and reported the run completed. The probe must recover the
// call, run it, and let the investigation continue to a real answer.
func TestProbe_RecoversNarratedToolCall(t *testing.T) {
	narration := "I will start by investigating the shop-cluster. I'll use the echo tool."
	m := &mockLLM{responses: []*llm.ChatResponse{
		// Iteration 1: narration, no tool call — the failure.
		{Content: narration},
		// The probe: offered tools, the model makes the call it promised.
		{ToolCalls: []llm.ToolCall{
			{ID: "c1", Name: "echo", Args: map[string]any{"message": "hi"}},
		}},
		// Iteration 2: the real answer, now grounded in the tool result.
		{Content: "the cause is X"},
		// The probe on THAT answer: genuinely finished.
		{Content: "DONE"},
	}}
	agent, session := newIntentAgent(t, m)

	answer, err := agent.Run(context.Background(), session, "why is checkout timing out?")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if answer != "the cause is X" {
		t.Errorf("answer = %q, want the investigation to continue to %q", answer, "the cause is X")
	}

	// The recovered turn must be written back as ONE coherent assistant message
	// carrying both the narration the model wrote and the call it meant to emit —
	// that is the turn the model intended to produce.
	var found bool
	for _, msg := range session.Messages {
		if msg.Role == "assistant" && msg.Content == narration {
			found = true
			if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].Name != "echo" {
				t.Errorf("narration message carries tool calls %+v, want the recovered echo call", msg.ToolCalls)
			}
		}
	}
	if !found {
		t.Error("narration was not preserved as the assistant message for the recovered turn")
	}

	// The tool actually ran.
	var ran bool
	for _, msg := range session.Messages {
		if msg.ToolResultID == "c1" {
			ran = true
		}
	}
	if !ran {
		t.Error("recovered tool call was never executed")
	}
}

// TestProbe_PreservesGenuineFinalAnswer is the safety property that makes the
// probe affordable: when the model was genuinely finished, its answer must reach
// the user EXACTLY as first written. The probe's own text (DONE, or whatever the
// model says) is a control signal, never content — it must never be returned or
// persisted in place of the answer.
func TestProbe_PreservesGenuineFinalAnswer(t *testing.T) {
	m := &mockLLM{responses: []*llm.ChatResponse{
		{Content: "readiness probes are failing on checkout"},
		{Content: "DONE"},
	}}
	agent, session := newIntentAgent(t, m)

	answer, err := agent.Run(context.Background(), session, "why is checkout timing out?")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if answer != "readiness probes are failing on checkout" {
		t.Errorf("answer = %q, want the model's original answer, not the probe's reply", answer)
	}

	last := session.Messages[len(session.Messages)-1]
	if last.Role != "assistant" || last.Content != "readiness probes are failing on checkout" {
		t.Errorf("last history message = %+v, want the original answer as the assistant turn", last)
	}
	for _, msg := range session.Messages {
		if msg.Content == "DONE" {
			t.Error("the probe's reply leaked into session history")
		}
	}
}

// TestProbe_InstructionNeverEntersHistory: the probe is a private control
// exchange. Its user-role instruction must never be persisted, or the
// conversation would carry a synthetic user turn the user never sent — visible
// on reload and fed back to the model on every later iteration.
func TestProbe_InstructionNeverEntersHistory(t *testing.T) {
	m := &mockLLM{responses: []*llm.ChatResponse{
		{Content: "an answer"},
		{Content: "DONE"},
	}}
	agent, session := newIntentAgent(t, m)

	if _, err := agent.Run(context.Background(), session, "hello"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, msg := range session.Messages {
		if strings.Contains(msg.Content, prompts.UnfulfilledToolIntentProbe) {
			t.Fatal("the probe instruction was persisted into session history")
		}
	}
}

// TestProbe_ErrorIsNonFatal: the probe is a best-effort disambiguation, not a
// gate. If it fails, the loop must fall back to the pre-probe behaviour —
// accept the response as final — rather than failing a turn that has a
// perfectly good answer in hand.
func TestProbe_ErrorIsNonFatal(t *testing.T) {
	// One scripted response, then the mock errors — which is exactly what the
	// probe call receives.
	m := &mockLLM{responses: []*llm.ChatResponse{{Content: "an answer"}}}
	agent, session := newIntentAgent(t, m)

	answer, err := agent.Run(context.Background(), session, "hello")
	if err != nil {
		t.Fatalf("Run returned %v; a failed probe must not fail the turn", err)
	}
	if answer != "an answer" {
		t.Errorf("answer = %q, want %q", answer, "an answer")
	}
}

// TestProbe_OffersToolsAndReplaysTheAnswer pins the shape of the probe request:
// it must offer tools (a model that cannot call one cannot fulfil its intent)
// and must replay the model's own text, since that text is the thing being
// disambiguated.
func TestProbe_OffersToolsAndReplaysTheAnswer(t *testing.T) {
	m := &mockLLM{responses: []*llm.ChatResponse{
		{Content: "I'll check the pods."},
		{Content: "DONE"},
	}}
	agent, session := newIntentAgent(t, m)

	if _, err := agent.Run(context.Background(), session, "hello"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	req := m.lastReq // the probe is the last call made
	if len(req.Tools) == 0 {
		t.Error("probe offered no tools; the model cannot make the call it promised")
	}
	last := req.Messages[len(req.Messages)-1]
	if last.Role != "user" || last.Content != prompts.UnfulfilledToolIntentProbe {
		t.Errorf("probe's final message = %+v, want the probe instruction as a user turn", last)
	}
	prior := req.Messages[len(req.Messages)-2]
	if prior.Role != "assistant" || prior.Content != "I'll check the pods." {
		t.Errorf("probe replayed %+v, want the model's own text as the assistant turn", prior)
	}
}

// TestProbe_EmptyContentStillProbed: a response with neither text nor tool calls
// is the most degenerate silent stop of all — the user gets a blank turn. It has
// no assistant text to replay (providers reject a contentless assistant
// message), but it is still worth probing.
func TestProbe_EmptyContentStillProbed(t *testing.T) {
	m := &mockLLM{responses: []*llm.ChatResponse{
		{Content: ""},
		{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "echo", Args: map[string]any{"message": "hi"}}}},
		{Content: "recovered answer"},
		{Content: "DONE"},
	}}
	agent, session := newIntentAgent(t, m)

	answer, err := agent.Run(context.Background(), session, "hello")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if answer != "recovered answer" {
		t.Errorf("answer = %q, want an empty response to be probed and recovered", answer)
	}
	for _, msg := range session.Messages {
		if msg.Role == "assistant" && msg.Content == "" && len(msg.ToolCalls) == 0 {
			t.Error("empty assistant message was written to history")
		}
	}
}

// TestProbe_TokenUsageIsBilled: the probe is a real LLM call. Its tokens are real
// spend and must land in the turn's totals, or cost accounting silently
// understates every turn.
func TestProbe_TokenUsageIsBilled(t *testing.T) {
	m := &mockLLM{responses: []*llm.ChatResponse{
		{Content: "an answer", Usage: llm.TokenUsage{TotalTokens: 100}},
		{Content: "DONE", Usage: llm.TokenUsage{TotalTokens: 7}},
	}}
	agent, session := newIntentAgent(t, m)

	if _, err := agent.Run(context.Background(), session, "hello"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if session.TotalTokens != 107 {
		t.Errorf("TotalTokens = %d, want 107 (the answer's 100 plus the probe's 7)", session.TotalTokens)
	}
}

// TestProbe_SkippedOnCancelledContext: a caller who has gone away gets no probe.
func TestProbe_SkippedOnCancelledContext(t *testing.T) {
	m := &mockLLM{responses: []*llm.ChatResponse{{Content: "an answer"}}}
	agent, session := newIntentAgent(t, m)

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel as soon as the first Chat returns, so the probe sees a done context.
	m.onChat = cancel

	_, err := agent.Run(ctx, session, "hello")
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run: %v", err)
	}
	if m.callCount != 1 {
		t.Errorf("made %d Chat calls, want 1 — the probe must not run on a cancelled context", m.callCount)
	}
}
