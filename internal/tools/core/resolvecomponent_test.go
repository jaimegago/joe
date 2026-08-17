package core_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/componentresolve"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/tools/core"
)

type fakeResolveComponentClient struct {
	gotPhrase string
	gotType   string
	result    *componentresolve.Resolution
	err       error
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
		"fall back",
		"bindings",
		"match_kind",
		"bindings_truncated",
		"matches_truncated",
	} {
		if !strings.Contains(desc, required) {
			t.Errorf("Description() must carry %q", required)
		}
	}

	// Empty bindings must not read as a wrong candidate: a registered
	// component no refresher has drawn an edge for is a real answer.
	if !strings.Contains(desc, "Empty bindings mean no edge has been derived") {
		t.Error("Description() must say that empty bindings are not a wrong candidate")
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
		MatchesTruncated: true,
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
	if out["matches_truncated"] != true {
		t.Error("matches_truncated must be reported to the caller")
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
