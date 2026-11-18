package audit_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jaimegago/joe/internal/audit"
)

var sessionTrigger = audit.Trigger{Type: audit.TriggerOperatorRequest, Ref: "task/t-1"}

func scoped(ctx context.Context) context.Context {
	return audit.WithScope(ctx, audit.Scope{SessionID: "s-1", TaskID: "t-1", Trigger: sessionTrigger})
}

var podGet = audit.Operation{Verb: "get", Resource: "pods", Name: "api-0", Namespace: "default"}

// TestActFor_TriggerRule pins the one rule ActFor applies: a trigger the model
// declared is recorded as declared when its type is in the vocabulary and it
// carries a reference; anything else records the session's opening trigger,
// and Source says which it was.
func TestActFor_TriggerRule(t *testing.T) {
	cases := []struct {
		name string
		call audit.Call
		want audit.Trigger
	}{
		{
			name: "declared alert with reference is recorded as declared",
			call: audit.Call{TriggerType: "alert", TriggerRef: "alert/high-memory"},
			want: audit.Trigger{Type: "alert", Ref: "alert/high-memory", Source: audit.TriggerSourceModel},
		},
		{
			name: "declared prior finding keeps the model's own reference form",
			call: audit.Call{TriggerType: "prior_finding", TriggerRef: "api-0 is in CrashLoopBackOff"},
			want: audit.Trigger{Type: "prior_finding", Ref: "api-0 is in CrashLoopBackOff", Source: audit.TriggerSourceModel},
		},
		{
			name: "nothing declared records the session's trigger",
			call: audit.Call{},
			want: audit.Trigger{Type: audit.TriggerOperatorRequest, Ref: "task/t-1", Source: audit.TriggerSourceSession},
		},
		{
			name: "a type outside the vocabulary records the session's trigger",
			call: audit.Call{TriggerType: "hunch", TriggerRef: "something felt off"},
			want: audit.Trigger{Type: audit.TriggerOperatorRequest, Ref: "task/t-1", Source: audit.TriggerSourceSession},
		},
		{
			name: "a type with no reference records the session's trigger",
			call: audit.Call{TriggerType: "alert", TriggerRef: "   "},
			want: audit.Trigger{Type: audit.TriggerOperatorRequest, Ref: "task/t-1", Source: audit.TriggerSourceSession},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			act, sessionID := audit.ActFor(audit.WithCall(scoped(context.Background()), tc.call), podGet)
			if sessionID != "s-1" {
				t.Errorf("session id = %q, want s-1", sessionID)
			}
			if act.Trigger == nil || *act.Trigger != tc.want {
				t.Errorf("trigger = %+v, want %+v", act.Trigger, tc.want)
			}
		})
	}
}

// TestActFor_ReasonIsTheModelsVerbatimOrEmpty pins D-0170: the reasoning is
// what the model wrote with the call, byte for byte, and nothing stands in
// for it when the model wrote nothing.
func TestActFor_ReasonIsTheModelsVerbatimOrEmpty(t *testing.T) {
	const stated = "  The operator asked why api-0 restarts;\n\tits own events are the first place to look.  "
	act, _ := audit.ActFor(audit.WithCall(scoped(context.Background()),
		audit.Call{Tool: "k8s_get", CallID: "call-1", Reason: stated}), podGet)
	if act.Reasoning != stated {
		t.Errorf("reasoning = %q, want the model's text verbatim %q", act.Reasoning, stated)
	}
	if act.Tool != "k8s_get" || act.ToolCallID != "call-1" {
		t.Errorf("tool = %q, tool_call_id = %q; want the call the reason was stated with", act.Tool, act.ToolCallID)
	}

	for name, ctx := range map[string]context.Context{
		"call with no reason": audit.WithCall(scoped(context.Background()), audit.Call{Tool: "k8s_get"}),
		"no call at all":      scoped(context.Background()),
	} {
		act, _ := audit.ActFor(ctx, podGet)
		if act.Reasoning != "" {
			t.Errorf("%s: reasoning = %q; Joe writes no reason the model did not state", name, act.Reasoning)
		}
		// The empty reason is recorded, not omitted: an entry says the model
		// gave none.
		var decoded map[string]any
		if err := json.Unmarshal([]byte(act.Encode()), &decoded); err != nil {
			t.Fatalf("%s: decode: %v", name, err)
		}
		if got, present := decoded["reasoning"]; !present || got != "" {
			t.Errorf("%s: encoded reasoning = %v (present %v); want an explicit empty string", name, got, present)
		}
	}
}

// TestActFor_NoScope_BelongsToNoSession: an act recorded outside an agent
// loop carries its operation and nothing that would place it in a trail. A
// tool call on the context does not change that — the session is the scope's
// to name, and only the code that starts a loop stamps one.
func TestActFor_NoScope_BelongsToNoSession(t *testing.T) {
	ctx := audit.WithCall(context.Background(), audit.Call{
		Tool: "k8s_get", Reason: "stated with no session", TriggerType: "alert", TriggerRef: "alert/x",
	})
	act, sessionID := audit.ActFor(ctx, podGet)
	if sessionID != "" || act.SessionID != "" {
		t.Errorf("session id = %q / %q; an act outside a loop belongs to no session", sessionID, act.SessionID)
	}
	if act.Trigger != nil {
		t.Errorf("trigger = %+v; want none outside a session", act.Trigger)
	}
	if act.Reasoning != "" {
		t.Errorf("reasoning = %q; want none outside a session", act.Reasoning)
	}
	if act.Operation != podGet {
		t.Errorf("operation = %+v, want %+v", act.Operation, podGet)
	}
}

// TestWithScope_EmptySessionIsNotAScope: a scope that names no session stamps
// nothing, so an act can never be filed under the empty session id.
func TestWithScope_EmptySessionIsNotAScope(t *testing.T) {
	ctx := audit.WithScope(context.Background(), audit.Scope{TaskID: "t-1", Trigger: sessionTrigger})
	if _, ok := audit.ScopeFrom(ctx); ok {
		t.Error("a scope with no session id was stamped")
	}
}
