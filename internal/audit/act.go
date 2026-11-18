package audit

import (
	"context"
	"encoding/json"
	"strings"
)

// An act is one operation the agent loop takes against a managed system: one
// call that reaches an infrastructure adapter through the guarded accessor.
// Each act leaves one infra_access row, and that row carries the act — the
// session it belongs to, the operation and its target, the reason the model
// gave with the tool call, and what triggered it (D-0170, D-0171).
//
// The three pieces arrive from three places, and none is inferred from
// another:
//
//   - the Operation is declared by the accessor dispatch method, beside the
//     adapter call it gates;
//   - the Scope is stamped on the context by the code that starts an agent
//     loop, and by nothing else;
//   - the Call is stamped by the tool layer for the duration of one tool call,
//     from what the model wrote with that call.
//
// A row written with no Scope on the context — a direct HTTP read, the
// autonomous refresh — still carries its Operation, and belongs to no
// session's trail.

// Operation is what an act did and to what, at the resolution a Kubernetes
// audit event records it: verb, resource, subresource, object name and
// namespace. Other managed systems use the same five fields with the closest
// reading their API allows; a field with no counterpart stays empty.
type Operation struct {
	// Verb is the operation as the managed system's API names it: get, list,
	// query, create.
	Verb string `json:"verb"`
	// APIGroup is the API group that owns Resource: a Kubernetes API group
	// (empty for the core group), or the service of a cloud API (ec2, eks,
	// rds). Empty where the system has no such grouping.
	APIGroup string `json:"api_group,omitempty"`
	// Resource is the resource type, as the API names it (plural for
	// Kubernetes: pods, deployments).
	Resource string `json:"resource"`
	// Subresource narrows Resource: a pod's log, an application's history.
	Subresource string `json:"subresource,omitempty"`
	// Name is the object the operation named; empty for a list or a query.
	Name string `json:"name,omitempty"`
	// Namespace is the namespace the operation was scoped to; empty for a
	// cluster-wide operation and for systems that have none.
	Namespace string `json:"namespace,omitempty"`
}

// IsZero reports whether the operation declares nothing.
func (o Operation) IsZero() bool { return o == Operation{} }

// The trigger vocabulary: what caused an act. The four values are the ones an
// evaluator of the trail recognises; an act's trigger is always one of them.
const (
	TriggerOperatorRequest = "operator_request"
	TriggerAlert           = "alert"
	TriggerPolicy          = "policy"
	TriggerPriorFinding    = "prior_finding"
)

// IsTriggerType reports whether t is a member of the trigger vocabulary.
func IsTriggerType(t string) bool {
	switch t {
	case TriggerOperatorRequest, TriggerAlert, TriggerPolicy, TriggerPriorFinding:
		return true
	}
	return false
}

// Trigger sources: who said what the trigger was.
const (
	// TriggerSourceModel — the model declared the trigger with the tool call.
	TriggerSourceModel = "model"
	// TriggerSourceSession — the model declared none (or none usable), and the
	// row records what opened the session the act belongs to.
	TriggerSourceSession = "session"
)

// Trigger is what caused an act: a type from the vocabulary and a reference to
// the thing the type names. Source says whether the model declared it or the
// session's own opening trigger was recorded.
type Trigger struct {
	Type   string `json:"type"`
	Ref    string `json:"ref"`
	Source string `json:"source,omitempty"`
}

// Act is the typed shape of the audit_log.context column on an infra_access
// row that records an act.
type Act struct {
	SessionID  string    `json:"session_id,omitempty"`
	TaskID     string    `json:"task_id,omitempty"`
	Tool       string    `json:"tool,omitempty"`
	ToolCallID string    `json:"tool_call_id,omitempty"`
	Operation  Operation `json:"operation"`
	// Reasoning is the model's own statement of why it made the call, recorded
	// verbatim. It is "" when the model gave none: Joe writes no reason on the
	// model's behalf and reconstructs none afterwards (D-0170).
	Reasoning string `json:"reasoning"`
	// Trigger is absent on a row written outside an agent-loop session.
	Trigger *Trigger `json:"trigger,omitempty"`
}

// Scope names the agent-loop session an act belongs to. It is stamped by the
// code that starts a loop (the task handlers and the Auto Joe run service) and
// read by the accessor when it writes the act's row.
//
// It is deliberately not agentctx.SessionID: that value is also populated from
// a caller-supplied request header on every route, and a session's trail must
// not be extendable by a request that merely names the session.
type Scope struct {
	// SessionID is the session id Joe returns to its caller.
	SessionID string
	// TaskID is the task or run the loop is executing.
	TaskID string
	// Trigger is what opened the session: the operator's request, or the
	// schedule that fired. It is recorded on every act whose tool call
	// declared no trigger of its own.
	Trigger Trigger
}

// Call is what the model stated with one tool call. It is stamped for the
// duration of that call by the tool layer.
type Call struct {
	Tool   string
	CallID string
	// Reason is the model's reason argument, verbatim.
	Reason string
	// TriggerType and TriggerRef are the model's trigger arguments, verbatim.
	TriggerType string
	TriggerRef  string
}

type scopeKey struct{}
type callKey struct{}

// WithScope returns a context carrying the agent-loop session scope. A scope
// with no session id is not stamped.
func WithScope(ctx context.Context, s Scope) context.Context {
	if s.SessionID == "" {
		return ctx
	}
	return context.WithValue(ctx, scopeKey{}, s)
}

// ScopeFrom returns the agent-loop session scope on ctx, if any.
func ScopeFrom(ctx context.Context) (Scope, bool) {
	s, ok := ctx.Value(scopeKey{}).(Scope)
	return s, ok
}

// WithCall returns a context carrying what the model stated with the tool
// call now executing.
func WithCall(ctx context.Context, c Call) context.Context {
	return context.WithValue(ctx, callKey{}, c)
}

// CallFrom returns the tool call stamped on ctx, if any.
func CallFrom(ctx context.Context) (Call, bool) {
	c, ok := ctx.Value(callKey{}).(Call)
	return c, ok
}

// ActFor assembles the act an accessor dispatch records: the operation it
// declared, joined to the session scope and the tool call on ctx. The returned
// session id is the value for the row's session_id column, "" when the act
// belongs to no agent-loop session.
//
// The trigger rule: a trigger the model declared with the call is recorded as
// declared when its type is in the vocabulary and it carries a reference;
// otherwise the session's opening trigger is recorded, and Source says which
// it was. The reasoning has no such fallback.
func ActFor(ctx context.Context, op Operation) (act Act, sessionID string) {
	act = Act{Operation: op}
	call, hasCall := CallFrom(ctx)
	if hasCall {
		act.Tool = call.Tool
		act.ToolCallID = call.CallID
	}
	scope, ok := ScopeFrom(ctx)
	if !ok {
		return act, ""
	}
	act.SessionID = scope.SessionID
	act.TaskID = scope.TaskID
	if hasCall {
		act.Reasoning = call.Reason
	}
	trigger := scope.Trigger
	trigger.Source = TriggerSourceSession
	if hasCall && IsTriggerType(call.TriggerType) && strings.TrimSpace(call.TriggerRef) != "" {
		trigger = Trigger{Type: call.TriggerType, Ref: call.TriggerRef, Source: TriggerSourceModel}
	}
	act.Trigger = &trigger
	return act, scope.SessionID
}

// Encode renders the act as the JSON stored in audit_log.context.
func (a Act) Encode() string {
	blob, err := json.Marshal(a)
	if err != nil {
		// Unreachable: Act holds only strings. An empty context is still a
		// valid row, and the insert that follows decides what happens next.
		return "{}"
	}
	return string(blob)
}
