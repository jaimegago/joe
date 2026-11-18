package tools

import (
	"context"

	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/prompts"
)

// The act declaration: the three arguments a model states with a tool call
// that acts against a managed system.
//
//   - reason      — why it is making the call, in its own words;
//   - trigger     — what caused the call, from the audit trigger vocabulary;
//   - trigger_ref — what that trigger refers to.
//
// They belong to the call, not to the tool: the wrapped tool never sees them.
// WithActDeclaration adds them to the tool's schema, lifts them off the
// arguments when the call arrives, and stamps them on the context for the
// duration of the call, where the guarded accessor reads them when it writes
// the act's audit row (audit.ActFor).
//
// All three are optional, and nothing is enforced on them. A call that states
// no reason is made, and its audit entry records an empty reason: Joe writes
// none on the model's behalf and reconstructs none afterwards (D-0170).
const (
	ActReasonArg     = "reason"
	ActTriggerArg    = "trigger"
	ActTriggerRefArg = "trigger_ref"
)

// actArgs are the act declaration's argument names, in schema order.
var actArgs = []struct {
	name        string
	description string
}{
	{ActReasonArg, prompts.ActReasonParam},
	{ActTriggerArg, prompts.ActTriggerParam},
	{ActTriggerRefArg, prompts.ActTriggerRefParam},
}

// actTool decorates a Tool with the act declaration.
type actTool struct {
	Tool
}

// WithActDeclaration wraps a tool whose calls reach a managed system so that
// each call can carry the model's reason and trigger. The wrapped tool's
// name, description and behaviour are unchanged; its schema gains the three
// optional act arguments, and its Execute receives the arguments without
// them.
func WithActDeclaration(t Tool) Tool {
	if _, already := t.(actTool); already {
		return t
	}
	return actTool{Tool: t}
}

// Unwrap returns the tool the act declaration decorates, or t itself when it
// carries none. It exists for callers that need the concrete tool.
func Unwrap(t Tool) Tool {
	if a, ok := t.(actTool); ok {
		return a.Tool
	}
	return t
}

// Parameters returns the wrapped tool's schema with the act arguments added.
// The wrapped schema is copied, never mutated: tools return their schema by
// value but its Properties map is shared.
func (a actTool) Parameters() llm.ParameterSchema {
	inner := a.Tool.Parameters()
	props := make(map[string]llm.Property, len(inner.Properties)+len(actArgs))
	for k, v := range inner.Properties {
		props[k] = v
	}
	for _, arg := range actArgs {
		if _, taken := props[arg.name]; taken {
			// The tool already has a parameter of this name and it means what
			// the tool says it means. Its value is still read as the act
			// declaration below; no shipped tool is in this position, and the
			// registration test refuses one that is.
			continue
		}
		props[arg.name] = llm.Property{Type: "string", Description: arg.description}
	}
	return llm.ParameterSchema{
		Type:       inner.Type,
		Properties: props,
		Required:   inner.Required,
	}
}

// Execute lifts the act declaration off the arguments, stamps it on the
// context, and runs the wrapped tool on the remaining arguments.
//
// The values are taken exactly as the model wrote them — no trimming, no
// normalising, no default. An argument that is absent or is not a string is
// the model stating nothing, and is recorded as nothing.
func (a actTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	call := audit.Call{
		Tool:   a.Tool.Name(),
		CallID: ToolCallID(ctx),
	}
	call.Reason, _ = args[ActReasonArg].(string)
	call.TriggerType, _ = args[ActTriggerArg].(string)
	call.TriggerRef, _ = args[ActTriggerRefArg].(string)

	inner := make(map[string]any, len(args))
	for k, v := range args {
		switch k {
		case ActReasonArg, ActTriggerArg, ActTriggerRefArg:
			continue
		}
		inner[k] = v
	}
	return a.Tool.Execute(audit.WithCall(ctx, call), inner)
}

type toolCallIDKey struct{}

// WithToolCallID returns a context carrying the id the model gave the tool
// call now executing. Both batch executors stamp it per call, so an act's
// audit row can name the call it belongs to.
func WithToolCallID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, toolCallIDKey{}, id)
}

// ToolCallID returns the id of the tool call executing on ctx, or "".
func ToolCallID(ctx context.Context) string {
	id, _ := ctx.Value(toolCallIDKey{}).(string)
	return id
}
