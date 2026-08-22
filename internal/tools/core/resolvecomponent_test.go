package core_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/componentresolve"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/store"
	"github.com/jaimegago/joe/internal/tools/core"
)

type fakeResolveComponentClient struct {
	gotPhrase string
	gotType   string
	result    *componentresolve.Resolution
	err       error

	reachable []*store.Component
	listErr   error
	listCalls int
}

func (f *fakeResolveComponentClient) ListComponents(_ context.Context) ([]*store.Component, error) {
	f.listCalls++
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.reachable, nil
}

func (f *fakeResolveComponentClient) ResolveComponents(_ context.Context, phrase, componentType string) (*componentresolve.Resolution, error) {
	f.gotPhrase = phrase
	f.gotType = componentType
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

func TestResolveComponentTool_Metadata(t *testing.T) {
	tool := core.NewResolveComponentTool(&fakeResolveComponentClient{})
	if tool.Name() != "resolve_component" {
		t.Errorf("Name() = %q, want resolve_component", tool.Name())
	}

	params := tool.Parameters()
	if _, ok := params.Properties["phrase"]; !ok {
		t.Error("Parameters() must expose a phrase property")
	}
	if len(params.Required) != 1 || params.Required[0] != "phrase" {
		t.Errorf("Required = %v, want exactly [phrase] — the type filter is optional", params.Required)
	}
}

// TestResolveComponentTool_DescriptionCarriesTheContract asserts the caveats
// the loop can only learn from the tool itself. A contract the loop cannot see
// does not constrain it, so the ambiguity-is-normal rule, the empty-is-an-answer
// rule and the fall-back instruction have to be IN the description, not only in
// the code comments.
func TestResolveComponentTool_DescriptionCarriesTheContract(t *testing.T) {
	desc := core.NewResolveComponentTool(&fakeResolveComponentClient{}).Description()

	for _, required := range []string{
		"SEVERAL CANDIDATES IS NORMAL",
		"AN EMPTY RESULT IS AN ANSWER, NOT AN ERROR",
		"do not report the phrase as naming something that does not exist",
		// The fallback is carried in the answer (`reachable_components`,
		// `next`) rather than described as "try list_components": the
		// 2026-08-21 DA-1 runs showed the described form being skipped.
		"reachable_components",
		"never a reason to stop while at least one component is reachable",
		"If exactly one component is reachable, investigate inside it and never ask the operator",
		"bindings",
		"match_kind",
		"bindings_truncated",
		"matches_truncated",
		"candidates_truncated",
		"max_total_bindings",
	} {
		if !strings.Contains(desc, required) {
			t.Errorf("Description() must carry %q", required)
		}
	}

	// Empty bindings must not read as a wrong candidate: a registered
	// component no refresher has drawn an edge for is a real answer.
	//
	// It must ALSO not read as a completeness claim. The evidence is filtered
	// per principal and bounded, so "no edge has been derived" is false in the
	// direction most likely to mislead the reasoner this text was written for —
	// a caller told that empty means nothing exists will report a component as
	// unattached when what happened is that it is attached to things they may
	// not see.
	if !strings.Contains(desc, "Empty bindings mean no edge you may see has been derived") {
		t.Error("Description() must say that empty bindings are not a wrong candidate, " +
			"and must qualify the claim to what this caller may see")
	}
	if strings.Contains(desc, "Empty bindings mean no edge has been derived") {
		t.Error("Description() states an unqualified completeness claim about the graph")
	}
}

func TestResolveComponentTool_Execute_ShapesCandidates(t *testing.T) {
	c := &fakeResolveComponentClient{result: &componentresolve.Resolution{
		Candidates: []componentresolve.Candidate{{
			ComponentID: "c-checkout",
			Name:        "checkout",
			Type:        "kubernetes",
			MatchKind:   componentresolve.MatchNameInPhrase,
			MatchText:   "checkout",
			Bindings: []graph.ComponentBinding{{
				NodeID:          "svc:checkout",
				NodeType:        "service",
				Relation:        graph.RelationMetricsIn,
				Direction:       graph.BindingOut,
				Confidence:      graph.Explicit,
				PeerNodeID:      "prom:root",
				PeerNodeType:    "metrics_backend",
				PeerComponentID: "obs/prom",
			}},
			BindingsTruncated: true,
		}},
		MatchesTruncated:        true,
		CandidatesTruncated:     true,
		MaxCandidates:           25,
		MaxBindingsPerCandidate: 10,
		MaxTotalBindings:        250,
	}}
	tool := core.NewResolveComponentTool(c)

	res, err := tool.Execute(context.Background(), map[string]any{
		"phrase": "the checkout app in prod",
		"type":   "kubernetes",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The phrase reaches the client whole. Stripping qualifiers before the
	// call is exactly what the contract forbids.
	if c.gotPhrase != "the checkout app in prod" {
		t.Errorf("client got phrase %q, want it passed through unmodified", c.gotPhrase)
	}
	if c.gotType != "kubernetes" {
		t.Errorf("client got type %q, want kubernetes", c.gotType)
	}

	out, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("Execute returned %T, want map[string]any", res)
	}
	if out["candidate_count"] != 1 {
		t.Errorf("candidate_count = %v, want 1", out["candidate_count"])
	}
	// Two truncation flags and the bounds behind them. They answer different
	// questions: matches_truncated is about the ungoverned registry, and
	// candidates_truncated is about what THIS caller may see. Collapsing them
	// into one flag is what let a bound effect read as a permission one.
	for key, want := range map[string]any{
		"matches_truncated":          true,
		"candidates_truncated":       true,
		"max_candidates":             25,
		"max_bindings_per_candidate": 10,
		"max_total_bindings":         250,
	} {
		if out[key] != want {
			t.Errorf("out[%q] = %v, want %v", key, out[key], want)
		}
	}
	if _, hasReason := out["reason"]; hasReason {
		t.Error("a non-empty result must not carry the empty-result reason")
	}

	candidates := out["candidates"].([]map[string]any)
	got := candidates[0]
	for key, want := range map[string]any{
		"component_id":       "c-checkout",
		"name":               "checkout",
		"type":               "kubernetes",
		"match_kind":         componentresolve.MatchNameInPhrase,
		"matched_text":       "checkout",
		"binding_count":      1,
		"bindings_truncated": true,
	} {
		if got[key] != want {
			t.Errorf("candidate[%q] = %v, want %v", key, got[key], want)
		}
	}

	bindings := got["bindings"].([]map[string]any)
	if len(bindings) != 1 {
		t.Fatalf("got %d bindings, want 1", len(bindings))
	}
	for key, want := range map[string]any{
		"relation":          graph.RelationMetricsIn,
		"direction":         graph.BindingOut,
		"peer_component_id": "obs/prom",
		"confidence":        "explicit",
	} {
		if bindings[0][key] != want {
			t.Errorf("binding[%q] = %v, want %v", key, bindings[0][key], want)
		}
	}
}

// TestResolveComponentTool_EmptyIsAnAnswerWithOneReason is the unhappy-path
// assertion. Zero matches and matched-but-not-permitted are indistinguishable
// at this boundary by construction — the tool cannot tell them apart either,
// because the accessor hands it the same empty list — and both must come back
// as a successful call carrying the same reason, never as an error.
func TestResolveComponentTool_EmptyIsAnAnswerWithOneReason(t *testing.T) {
	c := &fakeResolveComponentClient{result: &componentresolve.Resolution{}}
	tool := core.NewResolveComponentTool(c)

	res, err := tool.Execute(context.Background(), map[string]any{"phrase": "something unknown"})
	if err != nil {
		t.Fatalf("an empty result must not be an error, got: %v", err)
	}

	out := res.(map[string]any)
	if out["candidate_count"] != 0 {
		t.Errorf("candidate_count = %v, want 0", out["candidate_count"])
	}
	if len(out["candidates"].([]map[string]any)) != 0 {
		t.Error("candidates must be an empty list, not absent")
	}

	reason, ok := out["reason"].(string)
	if !ok || reason == "" {
		t.Fatal("an empty result must carry a reason")
	}
	// The reason must not name which case occurred. It says "or" precisely so
	// the caller cannot tell an absent component from a withheld one.
	if !strings.Contains(reason, " or ") {
		t.Errorf("reason %q must not resolve which of the two empty cases occurred", reason)
	}
	if strings.Contains(strings.ToLower(reason), "denied") ||
		strings.Contains(strings.ToLower(reason), "permission") {
		t.Errorf("reason %q leaks that authorization was involved in THIS call", reason)
	}
}

// TestResolveComponentTool_MissingPhraseIsAnError separates a malformed call
// from an empty answer. The no-error-shape rule is about absence and
// non-permission; a caller that did not say what to resolve gets an error.
func TestResolveComponentTool_MissingPhraseIsAnError(t *testing.T) {
	tool := core.NewResolveComponentTool(&fakeResolveComponentClient{result: &componentresolve.Resolution{}})

	for _, args := range []map[string]any{
		{},
		{"phrase": ""},
		{"phrase": 42},
	} {
		if _, err := tool.Execute(context.Background(), args); err == nil {
			t.Errorf("Execute(%v) returned no error; a call with no phrase is malformed", args)
		}
	}
}

func TestResolveComponentTool_ClientErrorPropagates(t *testing.T) {
	c := &fakeResolveComponentClient{err: errors.New("graph store not available")}
	tool := core.NewResolveComponentTool(c)

	_, err := tool.Execute(context.Background(), map[string]any{"phrase": "checkout"})
	if err == nil {
		t.Fatal("a broken substrate must surface as an error, not as an empty answer")
	}
	if !strings.Contains(err.Error(), "graph store not available") {
		t.Errorf("error %v must carry the underlying cause", err)
	}
}

// The empty case is where the invariant joe-pm ratified on 2026-08-22 lives
// (threads/joe-unresolved-phrase-fallback.md): an unresolved phrase is never a
// terminal condition while at least one component is reachable, and the model
// must never be told — or left free — to ask the operator which cluster when
// there is exactly one. Prose said as much before and gemini-2.5-flash stopped
// anyway, so the fallback is now IN the answer: the reachable components and a
// directive keyed on their count.

func TestResolveComponentTool_EmptyWithOneReachable_DirectsInsideIt(t *testing.T) {
	c := &fakeResolveComponentClient{
		result:    &componentresolve.Resolution{},
		reachable: []*store.Component{{ID: "comp-1", Name: "oasis-lab", Type: "kubernetes"}},
	}
	tool := core.NewResolveComponentTool(c)

	res, err := tool.Execute(context.Background(), map[string]any{"phrase": "notification-service", "type": "service"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := res.(map[string]any)

	if c.listCalls != 1 {
		t.Fatalf("an empty answer must take the list hop itself, listCalls = %d", c.listCalls)
	}
	if out["reachable_component_count"] != 1 {
		t.Errorf("reachable_component_count = %v, want 1", out["reachable_component_count"])
	}
	reachable := out["reachable_components"].([]map[string]any)
	if len(reachable) != 1 || reachable[0]["component_id"] != "comp-1" || reachable[0]["name"] != "oasis-lab" {
		t.Errorf("reachable_components = %v, want the one registered cluster with its component_id", reachable)
	}
	if out["reachable_truncated"] != false {
		t.Errorf("reachable_truncated = %v, want false", out["reachable_truncated"])
	}

	next, _ := out["next"].(string)
	for _, required := range []string{"Exactly one component is reachable", "INSIDE", "component_id"} {
		if !strings.Contains(next, required) {
			t.Errorf("next %q must direct the investigation inside the one component (missing %q)", next, required)
		}
	}
	// The negative half of the invariant, stated as a prohibition rather than
	// left to inference: with one component there is nothing to ask about.
	if !strings.Contains(next, "Do NOT ask the operator which cluster") {
		t.Errorf("next %q must forbid asking the operator when exactly one component is reachable", next)
	}
	// The reason string is untouched: the fallback discloses the registry
	// (which list_components already does) and nothing about permission.
	if out["reason"] != "no component in the registry matched this phrase, or none that matched is visible to you" {
		t.Errorf("reason changed: %v", out["reason"])
	}
}

func TestResolveComponentTool_EmptyWithManyReachable_AsksOnlyIfUndisambiguated(t *testing.T) {
	c := &fakeResolveComponentClient{
		result: &componentresolve.Resolution{},
		reachable: []*store.Component{
			{ID: "comp-1", Name: "prod", Type: "kubernetes"},
			{ID: "comp-2", Name: "staging", Type: "kubernetes"},
		},
	}
	out := mustExecute(t, core.NewResolveComponentTool(c), "checkout app")

	if out["reachable_component_count"] != 2 {
		t.Errorf("reachable_component_count = %v, want 2", out["reachable_component_count"])
	}
	next, _ := out["next"].(string)
	if !strings.Contains(next, "More than one component is reachable") {
		t.Errorf("next %q must say several components are reachable", next)
	}
	if !strings.Contains(next, "Ask the operator which to use only if") {
		t.Errorf("next %q must make asking conditional on the task not disambiguating", next)
	}
}

func TestResolveComponentTool_EmptyWithNoneReachable_SaysSo(t *testing.T) {
	c := &fakeResolveComponentClient{result: &componentresolve.Resolution{}}
	out := mustExecute(t, core.NewResolveComponentTool(c), "anything")

	if out["reachable_component_count"] != 0 {
		t.Errorf("reachable_component_count = %v, want 0", out["reachable_component_count"])
	}
	if len(out["reachable_components"].([]map[string]any)) != 0 {
		t.Error("reachable_components must be an empty list, not absent")
	}
	if next, _ := out["next"].(string); !strings.Contains(next, "No component is reachable") {
		t.Errorf("next %q must say nothing is reachable", next)
	}
}

func TestResolveComponentTool_EmptyReachableListIsBounded(t *testing.T) {
	many := make([]*store.Component, 0, 40)
	for i := 0; i < 40; i++ {
		many = append(many, &store.Component{ID: "c", Name: "n", Type: "kubernetes"})
	}
	c := &fakeResolveComponentClient{result: &componentresolve.Resolution{}, reachable: many}
	out := mustExecute(t, core.NewResolveComponentTool(c), "anything")

	if n := len(out["reachable_components"].([]map[string]any)); n != 25 {
		t.Errorf("reachable_components has %d entries, want the 25 bound", n)
	}
	if out["reachable_truncated"] != true {
		t.Error("a cut list must say reachable_truncated: true")
	}
}

func TestResolveComponentTool_NonEmptyDoesNotList(t *testing.T) {
	c := &fakeResolveComponentClient{
		result: &componentresolve.Resolution{Candidates: []componentresolve.Candidate{{ComponentID: "comp-1", Name: "api", Type: "kubernetes"}}},
	}
	out := mustExecute(t, core.NewResolveComponentTool(c), "api")

	if c.listCalls != 0 {
		t.Errorf("a non-empty answer must not take the list hop, listCalls = %d", c.listCalls)
	}
	if _, present := out["reachable_components"]; present {
		t.Error("reachable_components belongs to the empty case only")
	}
}

func TestResolveComponentTool_EmptyListErrorPropagates(t *testing.T) {
	c := &fakeResolveComponentClient{result: &componentresolve.Resolution{}, listErr: errors.New("registry down")}
	_, err := core.NewResolveComponentTool(c).Execute(context.Background(), map[string]any{"phrase": "x"})
	if err == nil || !strings.Contains(err.Error(), "registry down") {
		t.Fatalf("a failed fallback list must surface as an error, got: %v", err)
	}
}

func mustExecute(t *testing.T, tool *core.ResolveComponentTool, phrase string) map[string]any {
	t.Helper()
	res, err := tool.Execute(context.Background(), map[string]any{"phrase": phrase})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return res.(map[string]any)
}
