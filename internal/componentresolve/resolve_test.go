package componentresolve_test

import (
	"fmt"
	"testing"

	"github.com/jaimegago/joe/internal/access"
	"github.com/jaimegago/joe/internal/componentresolve"
	"github.com/jaimegago/joe/internal/store"
)

func comp(id, name, typ string) *store.Component {
	return &store.Component{ID: id, Name: name, Type: typ}
}

// matchIDs reduces a match list to component IDs in order, which is what the
// ordering assertions care about.
func matchIDs(matched []componentresolve.Matched) []string {
	out := make([]string, 0, len(matched))
	for _, m := range matched {
		out = append(out, m.Component.ID)
	}
	return out
}

// TestMatch_Kinds walks the four match kinds against one small registry. Each
// case asserts the KIND as well as the hit, because the kind is carried out to
// the caller as the "why" half of a candidate's evidence — a wrong kind is a
// wrong answer even when the component is right.
func TestMatch_Kinds(t *testing.T) {
	components := []*store.Component{
		comp("c-checkout", "checkout", "kubernetes"),
		comp("c-prom", "prom-prod", "prometheus"),
		comp("c-oci", "images", "oci_registry"),
	}

	cases := []struct {
		name     string
		phrase   string
		wantID   string
		wantKind string
		wantText string
	}{
		{
			name:     "whole phrase is the name",
			phrase:   "checkout",
			wantID:   "c-checkout",
			wantKind: componentresolve.MatchExactName,
			wantText: "checkout",
		},
		{
			name:     "name appears inside the phrase",
			phrase:   "the checkout app in prod",
			wantID:   "c-checkout",
			wantKind: componentresolve.MatchNameInPhrase,
			wantText: "checkout",
		},
		{
			name:     "multi-token name appears as a contiguous run",
			phrase:   "is prom prod scraping anything",
			wantID:   "c-prom",
			wantKind: componentresolve.MatchNameInPhrase,
			wantText: "prom-prod",
		},
		{
			name:     "one token of the name matches",
			phrase:   "what is wrong with prom",
			wantID:   "c-prom",
			wantKind: componentresolve.MatchNameToken,
			wantText: "prom",
		},
		{
			name:     "type token matches when the name does not",
			phrase:   "check prometheus",
			wantID:   "c-prom",
			wantKind: componentresolve.MatchType,
			wantText: "prometheus",
		},
		{
			name:     "underscored type matches on one of its tokens",
			phrase:   "which registry holds the image",
			wantID:   "c-oci",
			wantKind: componentresolve.MatchType,
			wantText: "registry",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			matched, truncated := componentresolve.Match(components, tc.phrase, "")
			if truncated {
				t.Fatalf("Match(%q) reported truncation over a 3-component registry", tc.phrase)
			}
			if len(matched) == 0 {
				t.Fatalf("Match(%q) returned no candidates, want %s", tc.phrase, tc.wantID)
			}
			got := matched[0]
			if got.Component.ID != tc.wantID {
				t.Errorf("Match(%q) top candidate = %s, want %s (full order %v)",
					tc.phrase, got.Component.ID, tc.wantID, matchIDs(matched))
			}
			if got.MatchKind != tc.wantKind {
				t.Errorf("Match(%q) kind = %q, want %q", tc.phrase, got.MatchKind, tc.wantKind)
			}
			if got.MatchText != tc.wantText {
				t.Errorf("Match(%q) matched text = %q, want %q", tc.phrase, got.MatchText, tc.wantText)
			}
		})
	}
}

// TestMatch_QualifiersAreNotParsed is the MATCH NARROW half of the contract.
// "in prod" must not steer the answer: a component whose name contains "prod"
// is reached because the token "prod" is in its NAME, and one that has nothing
// to do with the word is not reached at all. Nothing here knows that "prod" is
// an environment.
func TestMatch_QualifiersAreNotParsed(t *testing.T) {
	components := []*store.Component{
		comp("c-a", "checkout", "kubernetes"),
		comp("c-b", "checkout-staging", "kubernetes"),
	}

	matched, _ := componentresolve.Match(components, "the checkout app in prod", "")

	// Both are legitimate candidates — "checkout" names one exactly and is a
	// token of the other. Collapsing to one on the strength of "in prod" is
	// exactly what this tool must not do.
	if len(matched) != 2 {
		t.Fatalf("Match returned %d candidates (%v), want both: ambiguity must survive matching",
			len(matched), matchIDs(matched))
	}
	if matched[0].Component.ID != "c-a" {
		t.Errorf("ranked order = %v, want the name-in-phrase match first", matchIDs(matched))
	}
	if matched[0].MatchKind != componentresolve.MatchNameInPhrase {
		t.Errorf("top match kind = %q, want %q", matched[0].MatchKind, componentresolve.MatchNameInPhrase)
	}
	if matched[1].MatchKind != componentresolve.MatchNameToken {
		t.Errorf("second match kind = %q, want %q", matched[1].MatchKind, componentresolve.MatchNameToken)
	}
}

// TestMatch_RankingIsTotalAndDeterministic pins that the ordering never depends
// on registry order. The same components shuffled must produce the same list —
// a ranking that moved with the store's row order would make the tool's answer
// unreproducible and its top candidate arbitrary.
func TestMatch_RankingIsTotalAndDeterministic(t *testing.T) {
	forward := []*store.Component{
		comp("c-1", "api", "kubernetes"),
		comp("c-2", "api-gateway", "kubernetes"),
		comp("c-3", "gateway", "kubernetes"),
	}
	reversed := []*store.Component{forward[2], forward[1], forward[0]}

	a, _ := componentresolve.Match(forward, "the api gateway", "")
	b, _ := componentresolve.Match(reversed, "the api gateway", "")

	if fmt.Sprint(matchIDs(a)) != fmt.Sprint(matchIDs(b)) {
		t.Fatalf("ranking depends on registry order: %v vs %v", matchIDs(a), matchIDs(b))
	}
	// "api-gateway" is the whole phrase; it must outrank both single-token names.
	if a[0].Component.ID != "c-2" {
		t.Errorf("ranked order = %v, want the exact-name match first", matchIDs(a))
	}
}

// TestMatch_TypeFilter asserts the filter is applied by the CALLER's argument
// and not read out of the phrase.
func TestMatch_TypeFilter(t *testing.T) {
	components := []*store.Component{
		comp("c-k8s", "checkout", "kubernetes"),
		comp("c-git", "checkout", "git"),
	}

	all, _ := componentresolve.Match(components, "checkout", "")
	if len(all) != 2 {
		t.Fatalf("unfiltered Match returned %v, want both components", matchIDs(all))
	}

	filtered, _ := componentresolve.Match(components, "checkout", "git")
	if len(filtered) != 1 || filtered[0].Component.ID != "c-git" {
		t.Fatalf("type-filtered Match returned %v, want [c-git]", matchIDs(filtered))
	}

	// The filter is case-insensitive on the type but never inferred: a phrase
	// naming a type does not filter, it only ever matches.
	byPhrase, _ := componentresolve.Match(components, "the git checkout", "")
	if len(byPhrase) != 2 {
		t.Errorf("a type word in the phrase must not act as a filter, got %v", matchIDs(byPhrase))
	}
}

// TestMatch_TruncationIsReported asserts the bound is reported rather than
// silently applied. A caller told "here are the matches" reads that differently
// from "here are the first N of many".
func TestMatch_TruncationIsReported(t *testing.T) {
	var components []*store.Component
	for i := 0; i < componentresolve.MaxMatchScan+5; i++ {
		components = append(components, comp(fmt.Sprintf("c-%03d", i), fmt.Sprintf("api-%03d", i), "kubernetes"))
	}

	matched, bounded := componentresolve.Match(components, "api", "")
	if len(matched) != componentresolve.MaxMatchScan {
		t.Errorf("Match returned %d candidates, want the MaxMatchScan bound of %d",
			len(matched), componentresolve.MaxMatchScan)
	}
	if !bounded {
		t.Error("Match must report the bound when more components matched than it examines")
	}

	few, boundedFew := componentresolve.Match(components[:3], "api", "")
	if boundedFew {
		t.Errorf("Match reported the bound for %d matches, well inside it", len(few))
	}
}

// TestMatch_ScanBoundIsWiderThanTheAnswerBound pins the relationship the fix
// turns on rather than leaving it to two constants in different packages
// drifting together.
//
// This bound is spent on a PERMISSION-BLIND ordering — nothing here has seen a
// principal — so everything it cuts is cut before anyone asks who is calling. It
// is therefore a work bound, and it is safe only while it is wide enough that a
// sparsely-granted principal's own components survive it to be evaluated. The
// answer bound is the narrow one, and it lives past the permit.
func TestMatch_ScanBoundIsWiderThanTheAnswerBound(t *testing.T) {
	if componentresolve.MaxMatchScan <= access.MaxResolveCandidates {
		t.Fatalf("MaxMatchScan (%d) must exceed the post-permit candidate bound (%d): a work bound "+
			"no wider than the answer bound cuts the principal's own matches before permission is evaluated",
			componentresolve.MaxMatchScan, access.MaxResolveCandidates)
	}
}

// TestMatch_NoMatchAndEmptyPhrase covers the two ways nothing comes back. Both
// are answers rather than errors, and the matcher signals them identically —
// an empty list, no truncation.
func TestMatch_NoMatchAndEmptyPhrase(t *testing.T) {
	components := []*store.Component{comp("c-1", "checkout", "kubernetes")}

	if matched, truncated := componentresolve.Match(components, "unrelated words entirely", ""); len(matched) != 0 || truncated {
		t.Errorf("a non-matching phrase returned %v (truncated=%v), want an empty answer", matchIDs(matched), truncated)
	}
	if matched, truncated := componentresolve.Match(components, "   ", ""); len(matched) != 0 || truncated {
		t.Errorf("a whitespace phrase returned %v (truncated=%v), want an empty answer", matchIDs(matched), truncated)
	}
	if matched, _ := componentresolve.Match(nil, "checkout", ""); len(matched) != 0 {
		t.Errorf("an empty registry returned %v, want an empty answer", matchIDs(matched))
	}
}

// TestMatch_NilComponentIsSkipped guards the loop against a nil row rather than
// panicking inside a tool call the loop is waiting on.
func TestMatch_NilComponentIsSkipped(t *testing.T) {
	components := []*store.Component{nil, comp("c-1", "checkout", "kubernetes")}
	matched, _ := componentresolve.Match(components, "checkout", "")
	if len(matched) != 1 || matched[0].Component.ID != "c-1" {
		t.Fatalf("Match returned %v, want just [c-1]", matchIDs(matched))
	}
}
