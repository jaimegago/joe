package tools

import (
	"context"
	"reflect"
	"sort"
	"testing"

	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/llm"
)

// probeTool records what a wrapped tool was actually handed.
type probeTool struct {
	schema   llm.ParameterSchema
	gotArgs  map[string]any
	gotCall  audit.Call
	hadCall  bool
	executed int
}

func (p *probeTool) Name() string                    { return "probe_tool" }
func (p *probeTool) Description() string             { return "probe" }
func (p *probeTool) Parameters() llm.ParameterSchema { return p.schema }
func (p *probeTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	p.executed++
	p.gotArgs = args
	p.gotCall, p.hadCall = audit.CallFrom(ctx)
	return "ok", nil
}

func newProbe() *probeTool {
	return &probeTool{schema: llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"component_id": {Type: "string", Description: "component"},
			"namespace":    {Type: "string", Description: "namespace"},
		},
		Required: []string{"component_id"},
	}}
}

// TestWithActDeclaration_SchemaGainsTheThreeOptionalArguments: the wrapped
// tool's schema carries reason, trigger and trigger_ref as optional strings
// beside its own parameters, and the tool's own schema is left as it was.
func TestWithActDeclaration_SchemaGainsTheThreeOptionalArguments(t *testing.T) {
	inner := newProbe()
	wrapped := WithActDeclaration(inner)

	got := wrapped.Parameters()
	for _, name := range []string{ActReasonArg, ActTriggerArg, ActTriggerRefArg} {
		prop, ok := got.Properties[name]
		if !ok {
			t.Errorf("wrapped schema has no %q parameter", name)
			continue
		}
		if prop.Type != "string" || prop.Description == "" {
			t.Errorf("%q = %+v; want a described string", name, prop)
		}
	}
	for _, name := range []string{"component_id", "namespace"} {
		if _, ok := got.Properties[name]; !ok {
			t.Errorf("wrapped schema lost the tool's own %q parameter", name)
		}
	}
	// Optional: nothing the tool did not require is required now.
	if !reflect.DeepEqual(got.Required, []string{"component_id"}) {
		t.Errorf("required = %v; the act arguments must stay optional", got.Required)
	}
	// The tool's own schema map is shared across calls and must not grow.
	if len(inner.schema.Properties) != 2 {
		t.Errorf("the wrapped tool's own schema was mutated: %v", inner.schema.Properties)
	}
	if wrapped.Name() != inner.Name() || wrapped.Description() != inner.Description() {
		t.Error("the declaration changed the tool's name or description")
	}
}

// TestWithActDeclaration_LiftsTheDeclarationOffTheArguments: the tool is run
// on its own arguments only, and what the model stated travels on the context
// exactly as written.
func TestWithActDeclaration_LiftsTheDeclarationOffTheArguments(t *testing.T) {
	inner := newProbe()
	wrapped := WithActDeclaration(inner)

	const reason = "  The operator asked why checkout is slow;\n start with its pods. "
	args := map[string]any{
		"component_id":   "k8s-prod",
		"namespace":      "shop",
		ActReasonArg:     reason,
		ActTriggerArg:    "alert",
		ActTriggerRefArg: "alert/high-latency",
	}
	ctx := WithToolCallID(context.Background(), "call-42")
	if _, err := wrapped.Execute(ctx, args); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if want := map[string]any{"component_id": "k8s-prod", "namespace": "shop"}; !reflect.DeepEqual(inner.gotArgs, want) {
		t.Errorf("tool received %v, want only its own arguments %v", inner.gotArgs, want)
	}
	if len(args) != 5 {
		t.Errorf("the caller's argument map was modified: %v", args)
	}
	want := audit.Call{Tool: "probe_tool", CallID: "call-42", Reason: reason, TriggerType: "alert", TriggerRef: "alert/high-latency"}
	if !inner.hadCall || inner.gotCall != want {
		t.Errorf("call on context = %+v (present %v), want %+v", inner.gotCall, inner.hadCall, want)
	}
}

// TestWithActDeclaration_NothingStatedIsNothingRecorded — D-0170. A call that
// states no reason, or states one that is not text, is still made, and what
// travels is the empty string: no default, no substitute.
func TestWithActDeclaration_NothingStatedIsNothingRecorded(t *testing.T) {
	for name, args := range map[string]map[string]any{
		"absent":       {"component_id": "k8s-prod"},
		"not a string": {"component_id": "k8s-prod", ActReasonArg: 7, ActTriggerArg: true, ActTriggerRefArg: []any{"x"}},
		"empty string": {"component_id": "k8s-prod", ActReasonArg: ""},
	} {
		t.Run(name, func(t *testing.T) {
			inner := newProbe()
			if _, err := WithActDeclaration(inner).Execute(context.Background(), args); err != nil {
				t.Fatalf("a call that stated no reason was not made: %v", err)
			}
			if inner.executed != 1 {
				t.Fatalf("tool executed %d times, want 1", inner.executed)
			}
			if inner.gotCall.Reason != "" || inner.gotCall.TriggerType != "" || inner.gotCall.TriggerRef != "" {
				t.Errorf("call = %+v; want nothing recorded where nothing was stated", inner.gotCall)
			}
			if _, leaked := inner.gotArgs[ActReasonArg]; leaked {
				t.Error("the reason argument reached the tool")
			}
		})
	}
}

// bareCoreTools are the core tools that read only Joe's own stores — the
// component registry and the infrastructure graph. A call to one reaches no
// managed system, leaves no act entry, and carries no act declaration.
var bareCoreTools = map[string]bool{
	"list_components":   true,
	"resolve_component": true,
	"graph_query":       true,
	"graph_related":     true,
}

// TestCoreRegistry_ActDeclarationCoversManagedSystemTools pins which tools
// carry the declaration: every core tool that reaches a managed system, and no
// other. A core tool added without it would act with no way for the model to
// state why; a shared or Joe-internal tool given it would ask the model for a
// reason Joe records nowhere.
//
// The core set is derived, not listed: it is what NewCoreRegistry holds beyond
// what registerSharedTools registers.
func TestCoreRegistry_ActDeclarationCoversManagedSystemTools(t *testing.T) {
	shared := NewRegistry()
	registerSharedTools(shared, nil)
	sharedNames := map[string]bool{}
	for _, tool := range shared.GetAll() {
		sharedNames[tool.Name()] = true
	}

	full := NewCoreRegistry(stubCoreClient{}, nil, nil)
	var managed, bareSeen []string
	for _, tool := range full.GetAll() {
		name := tool.Name()
		_, declared := tool.Parameters().Properties[ActReasonArg]
		switch {
		case sharedNames[name]:
			if declared {
				t.Errorf("shared tool %q carries the act declaration; it reaches no managed system through the accessor", name)
			}
		case bareCoreTools[name]:
			bareSeen = append(bareSeen, name)
			if declared {
				t.Errorf("%q reads only Joe's own stores and must not carry the act declaration", name)
			}
		default:
			managed = append(managed, name)
			if !declared {
				t.Errorf("core tool %q reaches a managed system and does not carry the act declaration; register it through act()", name)
			}
			for _, arg := range []string{ActTriggerArg, ActTriggerRefArg} {
				if _, ok := tool.Parameters().Properties[arg]; !ok {
					t.Errorf("core tool %q is missing the %q act argument", name, arg)
				}
			}
		}
	}

	sort.Strings(bareSeen)
	if len(bareSeen) != len(bareCoreTools) {
		t.Errorf("bare core tools registered = %v, want exactly %d (a renamed tool is no longer covered by this test)", bareSeen, len(bareCoreTools))
	}
	if len(managed) == 0 {
		t.Fatal("found no managed-system tools; the test is checking nothing")
	}
}

// TestWithActDeclaration_NoShippedToolShadowsAnActArgument: the declaration
// reads `reason`, `trigger` and `trigger_ref` off the arguments, so a tool
// that defined a parameter of its own under one of those names would have it
// taken away. None does; this is what keeps it that way.
func TestWithActDeclaration_NoShippedToolShadowsAnActArgument(t *testing.T) {
	for _, tool := range NewCoreRegistry(stubCoreClient{}, nil, nil).GetAll() {
		own := Unwrap(tool).Parameters().Properties
		for _, arg := range []string{ActReasonArg, ActTriggerArg, ActTriggerRefArg} {
			if _, clash := own[arg]; clash {
				t.Errorf("tool %q defines its own %q parameter, which the act declaration would consume", tool.Name(), arg)
			}
		}
	}
}

// TestExecuteBatch_StampsEachCallsID: an act's audit row names the tool call
// it belongs to, so the batch executor carries each call's id to its tool.
func TestExecuteBatch_StampsEachCallsID(t *testing.T) {
	// dns_lookup is a classified Read, so the executor runs it; the recorder
	// stands in under that name and reports the id it was handed.
	var seen []string
	registry := NewRegistry()
	registry.Register(idRecorder{name: "dns_lookup", seen: &seen})
	exec := NewExecutor(registry, nil)
	if _, err := exec.ExecuteBatch(context.Background(), []ToolCallRequest{
		{ID: "call-a", Name: "dns_lookup", Args: map[string]any{}},
		{ID: "call-b", Name: "dns_lookup", Args: map[string]any{}},
	}); err != nil {
		t.Fatalf("ExecuteBatch: %v", err)
	}
	if !reflect.DeepEqual(seen, []string{"call-a", "call-b"}) {
		t.Errorf("tool call ids seen by the tool = %v, want [call-a call-b]", seen)
	}
}

type idRecorder struct {
	name string
	seen *[]string
}

func (r idRecorder) Name() string                    { return r.name }
func (r idRecorder) Description() string             { return "records the tool call id" }
func (r idRecorder) Parameters() llm.ParameterSchema { return llm.ParameterSchema{Type: "object"} }
func (r idRecorder) Execute(ctx context.Context, _ map[string]any) (any, error) {
	*r.seen = append(*r.seen, ToolCallID(ctx))
	return nil, nil
}
