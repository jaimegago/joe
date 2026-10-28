package autorun

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jaimegago/joe/internal/agentloop"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/store"
)

func TestSelection_KeyIsCanonical(t *testing.T) {
	a := Selection{Components: []string{"b", "a", " a ", ""}, Zones: []string{"z2", "z1", "z2"}}
	b := Selection{Components: []string{"a", "b"}, Zones: []string{"z1", "z2"}}
	if a.Key() != b.Key() {
		t.Fatalf("keys differ for one scope: %q vs %q", a.Key(), b.Key())
	}
	if got := a.Key(); got != "components=a,b;zones=z1,z2" {
		t.Errorf("key = %q", got)
	}
	if !(Selection{}).Empty() || !(Selection{Components: []string{" "}}).Empty() {
		t.Error("blank selections must be empty")
	}
	if (Selection{Zones: []string{"z"}}).Empty() {
		t.Error("a zone-only selection is not empty")
	}
}

// fakeComponents is a store.ComponentRepository holding only what the scope
// resolver reads: the list.
type fakeComponents struct {
	store.ComponentRepository
	ids []string
}

func (f fakeComponents) List(_ context.Context) ([]*store.Component, error) {
	out := make([]*store.Component, 0, len(f.ids))
	for _, id := range f.ids {
		out = append(out, &store.Component{ID: id, Type: "kubernetes", Name: id})
	}
	return out, nil
}

type fakeZones map[string][]string // zone id → component ids

func (f fakeZones) ListAssignments(_ context.Context) ([]rbac.ComponentZoneAssignment, error) {
	var out []rbac.ComponentZoneAssignment
	for zone, ids := range f {
		for _, id := range ids {
			out = append(out, rbac.ComponentZoneAssignment{ComponentID: id, ZoneID: zone})
		}
	}
	return out, nil
}

func TestResolveScope(t *testing.T) {
	ctx := context.Background()
	comps := fakeComponents{ids: []string{"c1", "c2", "c3", "c4"}}
	zones := fakeZones{"prod": {"c2", "c3", "ghost"}, "dev": {"c4"}}

	got, err := ResolveScope(ctx, comps, zones, Selection{Components: []string{"c1"}, Zones: []string{"prod"}})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if strings.Join(got, ",") != "c1,c2,c3" {
		t.Errorf("resolved = %v, want c1,c2,c3 (sorted, deduplicated, phantom zone member dropped)", got)
	}

	if _, err := ResolveScope(ctx, comps, zones, Selection{}); !errors.Is(err, ErrEmptyScope) {
		t.Errorf("empty selection: err = %v, want ErrEmptyScope", err)
	}
	if _, err := ResolveScope(ctx, comps, zones, Selection{Components: []string{"nope"}}); !errors.Is(err, ErrUnknownComponent) {
		t.Errorf("unknown component: err = %v, want ErrUnknownComponent", err)
	}
	if _, err := ResolveScope(ctx, comps, zones, Selection{Zones: []string{"empty-zone"}}); !errors.Is(err, ErrEmptyScope) {
		t.Errorf("zone with no members: err = %v, want ErrEmptyScope", err)
	}
	if _, err := ResolveScope(ctx, comps, nil, Selection{Zones: []string{"prod"}}); !errors.Is(err, ErrZonesUnavailable) {
		t.Errorf("zones without RBAC: err = %v, want ErrZonesUnavailable", err)
	}
}

func TestCoverage_DerivedFromSuccessfulComponentCalls(t *testing.T) {
	cov := newCoverage([]string{"a", "b", "c"})
	cov.OnStep(agentloop.StepRecord{
		LLMResponse: agentloop.LLMResponseSummary{ToolCalls: []llm.ToolCall{
			{ID: "1", Name: "k8s_get", Args: map[string]any{"component_id": "a"}},
			{ID: "2", Name: "k8s_get", Args: map[string]any{"component_id": "b"}},
			{ID: "3", Name: "k8s_get", Args: map[string]any{"component_id": "zzz"}},
			{ID: "4", Name: "graph_query", Args: map[string]any{"query": "c"}},
		}},
		ToolResults: []agentloop.ToolResultRecord{
			{ID: "1", Name: "k8s_get"},
			{ID: "2", Name: "k8s_get", Error: "permission denied"},
			{ID: "3", Name: "k8s_get"},
			{ID: "4", Name: "graph_query"},
		},
	})
	// A step with no tool results (the terminal turn) covers nothing.
	cov.OnStep(agentloop.StepRecord{LLMResponse: agentloop.LLMResponseSummary{Content: "done"}})
	covered, not := cov.Partition()
	if strings.Join(covered, ",") != "a" {
		t.Errorf("covered = %v, want a only (b errored, zzz out of scope, graph_query names nothing)", covered)
	}
	if strings.Join(not, ",") != "b,c" {
		t.Errorf("not covered = %v, want b,c", not)
	}
}

func TestLimitsSection_ReadsTheRecord(t *testing.T) {
	ended := time.Date(2026, 10, 28, 10, 5, 0, 0, time.UTC)
	run := &Run{
		ID: "r1", Activator: "user:op@example.com", Principal: "svc:agent:core",
		Selection:          Selection{Zones: []string{"prod"}},
		ResolvedComponents: []string{"a", "b", "c"},
		TokenCeiling:       1000, WallClockSeconds: 600, ReportReserveTokens: 100,
		State: StateCompleted, StopReason: agentloop.StopReasonTokenCeiling,
		Covered: []string{"a"}, NotCovered: []string{"b", "c"},
		TokensUsed: 1010, ReportTokens: 40,
		StartedAt: ended.Add(-3 * time.Minute), EndedAt: &ended,
	}
	s := LimitsSection(run)
	for _, want := range []string{
		"## Limits",
		"3 component(s) resolved",
		"zones: prod",
		"stopped on the token ceiling (1000 tokens) with 1 of 3 component(s) covered",
		"Covered (1): a",
		"Not looked at (2): b, c. An absent finding on a component that was not looked at is not a clean bill.",
		"1010 of 1000 tokens used by the pass",
		"wall clock 3m0s of 10m0s",
		"the report spent 40 tokens from a reserve of 100 held outside the ceiling",
		"Run r1, activated by user:op@example.com, executed as svc:agent:core.",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("limits section missing %q:\n%s", want, s)
		}
	}

	run.StopReason, run.Covered, run.NotCovered = "", []string{"a", "b", "c"}, nil
	s = LimitsSection(run)
	if !strings.Contains(s, "completed: the pass took every component in the scope.") || !strings.Contains(s, "Not looked at: none.") {
		t.Errorf("completed limits section wrong:\n%s", s)
	}

	turn := FinalTurn("## Findings\n\nnone", run)
	if !strings.HasPrefix(turn, "## Findings") || !strings.Contains(turn, "\n\n## Limits\n") {
		t.Errorf("final turn must carry findings above the limits:\n%s", turn)
	}
}
