// Package componentresolve is the NAMING hop: it turns a task phrase like
// "app XYZ in prod" into the registered components that phrase might name.
//
// It is deliberately not the binding hop. The observe API's
// resolveComponentForService walks metrics_in/logs_in/traces_in/alerts_in edges
// to answer "which backend serves this subject" — a question about components
// that are already known to each other. This package answers the question one
// step earlier: which components could this English phrase be talking about at
// all. Graph edges bind known components to each other; they cannot
// disambiguate a phrase against a registry, so the two hops do not compose into
// one and this package does not wrap that resolver.
//
// Matching is NARROW and deterministic: component name and component type, by
// token, with no model and no fuzzy distance. Qualifiers in the phrase — "in
// prod", "the slow one", "since yesterday" — are never parsed here. The
// disambiguating work happens in the caller, which already has the whole phrase
// and, from this package, the graph evidence for each candidate. Collapsing an
// ambiguous phrase to one authoritative-looking component_id inside the
// resolver would discard the ambiguity irrecoverably; a caller handed several
// ranked candidates with their evidence can still get it right.
package componentresolve

import (
	"sort"
	"strings"

	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/store"
)

// Match kinds, strongest first. The kind travels all the way out to the caller
// because "why did this match" is half of the evidence a candidate carries: a
// component whose full name appeared verbatim in the phrase is a different sort
// of answer from one that shares a single token with it, and only the caller
// can weigh that against the rest of the sentence.
const (
	// MatchExactName — the whole phrase, normalized, IS the component's name.
	MatchExactName = "exact_name"

	// MatchNameInPhrase — the component's name appears in the phrase as a
	// contiguous run of tokens.
	MatchNameInPhrase = "name_in_phrase"

	// MatchNameToken — one token of the phrase equals one token of the
	// component's name.
	MatchNameToken = "name_token"

	// MatchType — one token of the phrase equals one token of the component's
	// TYPE (kubernetes, prometheus, oci_registry …). Weakest, and the reason
	// "check prometheus" legitimately returns every Prometheus component.
	MatchType = "type"
)

// rankOf orders the kinds. Lower is stronger.
func rankOf(kind string) int {
	switch kind {
	case MatchExactName:
		return 0
	case MatchNameInPhrase:
		return 1
	case MatchNameToken:
		return 2
	case MatchType:
		return 3
	default:
		return 4
	}
}

// MaxMatches bounds how many ranked matches leave this package. It exists
// because a type-token match is legitimately broad — one phrase containing the
// word "kubernetes" matches every Kubernetes component — and a caller reading
// candidates to tell them apart is not helped by an inventory. Overflow is
// reported, never silently dropped: Match returns a truncated flag, and the
// tool surfaces it.
const MaxMatches = 25

// Matched is one component the matcher selected, with the deterministic reason
// it selected it. It is pre-governance: nothing here has been checked against a
// principal yet.
type Matched struct {
	Component *store.Component

	// MatchKind is one of the Match* constants.
	MatchKind string

	// MatchText is the literal text that matched — the component name for a
	// name match, the single token for a token match. It is what a reader
	// checks the phrase against.
	MatchText string
}

// Candidate is one component the phrase might name, as it leaves the tool: the
// component's identity, why it matched, and the graph relations binding it to
// other components the caller is allowed to see.
type Candidate struct {
	ComponentID string
	Name        string
	Type        string

	MatchKind string
	MatchText string

	// Bindings is the graph evidence: what this component is bound to and via
	// which relations. Empty is a real answer — a registered component that no
	// refresher has yet drawn an edge for has nothing to show, and saying so is
	// more useful than omitting the candidate.
	Bindings []graph.ComponentBinding

	// BindingsTruncated reports that the component has more bindings than were
	// returned. It means the evidence is a prefix, not that it is wrong.
	BindingsTruncated bool
}

// Resolution is the whole answer to one resolve call.
type Resolution struct {
	// Candidates is ranked strongest match first. It may be empty, and an
	// empty Candidates list is an answer rather than a failure.
	Candidates []Candidate

	// MatchesTruncated reports that more components matched the phrase than
	// MaxMatches, so the ranked list is a prefix. It is set from the MATCH
	// step, before governance filtering, so it never leaks whether the
	// components dropped past the bound would have been visible.
	MatchesTruncated bool
}

// Match returns the components whose name or type the phrase could be naming,
// ranked strongest first, plus a flag reporting that the ranked list was capped
// at MaxMatches.
//
// typeFilter, when non-empty, restricts the candidate set to components of that
// exact type before any matching happens. It is a filter the caller asked for,
// not something parsed out of the phrase.
//
// The ordering is total and deterministic: match rank, then longer matched text
// first (a longer literal match is more specific), then component name, then
// component ID. Two runs over the same registry and the same phrase return the
// same list in the same order.
func Match(components []*store.Component, phrase, typeFilter string) ([]Matched, bool) {
	phraseTokens := tokenize(phrase)
	if len(phraseTokens) == 0 {
		return nil, false
	}
	wantType := strings.ToLower(strings.TrimSpace(typeFilter))

	var matched []Matched
	for _, c := range components {
		if c == nil {
			continue
		}
		if wantType != "" && strings.ToLower(c.Type) != wantType {
			continue
		}
		if m, ok := matchOne(c, phraseTokens); ok {
			matched = append(matched, m)
		}
	}

	sort.SliceStable(matched, func(i, j int) bool {
		a, b := matched[i], matched[j]
		if ra, rb := rankOf(a.MatchKind), rankOf(b.MatchKind); ra != rb {
			return ra < rb
		}
		if la, lb := len(a.MatchText), len(b.MatchText); la != lb {
			return la > lb
		}
		if a.Component.Name != b.Component.Name {
			return a.Component.Name < b.Component.Name
		}
		return a.Component.ID < b.Component.ID
	})

	if len(matched) > MaxMatches {
		return matched[:MaxMatches], true
	}
	return matched, false
}

// matchOne applies the four rules to one component and returns the STRONGEST
// that fires. A component matches at most once: the kinds are alternatives, not
// accumulating signals, and reporting the strongest is what lets the caller
// read the rank as a claim about specificity.
func matchOne(c *store.Component, phraseTokens []string) (Matched, bool) {
	nameTokens := tokenize(c.Name)

	if len(nameTokens) > 0 {
		if equalTokens(phraseTokens, nameTokens) {
			return Matched{Component: c, MatchKind: MatchExactName, MatchText: c.Name}, true
		}
		if containsRun(phraseTokens, nameTokens) {
			return Matched{Component: c, MatchKind: MatchNameInPhrase, MatchText: c.Name}, true
		}
		if tok, ok := firstSharedToken(phraseTokens, nameTokens); ok {
			return Matched{Component: c, MatchKind: MatchNameToken, MatchText: tok}, true
		}
	}

	typeTokens := tokenize(c.Type)
	if len(typeTokens) > 0 {
		if containsRun(phraseTokens, typeTokens) {
			return Matched{Component: c, MatchKind: MatchType, MatchText: strings.Join(typeTokens, " ")}, true
		}
		if tok, ok := firstSharedToken(phraseTokens, typeTokens); ok {
			return Matched{Component: c, MatchKind: MatchType, MatchText: tok}, true
		}
	}

	return Matched{}, false
}

// tokenize lowercases and splits on every run of non-alphanumeric characters,
// so "app-xyz", "app_xyz", "App XYZ" and "app.xyz" all tokenize alike. There is
// no stop-word list: a stop-word list is a policy about English that would have
// to be justified, maintained, and translated, and its absence costs only that
// a component literally named "in" matches the word "in" — degenerate, honest,
// and ranked below every stronger match anyway.
func tokenize(s string) []string {
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		switch {
		case r >= 'a' && r <= 'z':
			return false
		case r >= '0' && r <= '9':
			return false
		default:
			return true
		}
	})
	if len(fields) == 0 {
		return nil
	}
	return fields
}

// equalTokens reports whether two token slices are identical.
func equalTokens(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	return allEqual(a, b)
}

// allEqual compares two equal-length token slices element-wise. Callers check
// the lengths themselves, because containsRun compares a window rather than a
// whole slice.
func allEqual(a, b []string) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// containsRun reports whether needle appears in haystack as a contiguous run of
// tokens. Contiguity is what keeps "name in phrase" a claim about the phrase
// naming the component, rather than about the phrase happening to contain the
// component's tokens scattered across a sentence.
func containsRun(haystack, needle []string) bool {
	if len(needle) == 0 || len(needle) > len(haystack) {
		return false
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if allEqual(haystack[i:i+len(needle)], needle) {
			return true
		}
	}
	return false
}

// firstSharedToken returns the first token of the PHRASE that also appears in
// other. Scanning phrase order rather than component order makes the reported
// MatchText stable against a component rename that reorders its own tokens.
func firstSharedToken(phraseTokens, other []string) (string, bool) {
	set := make(map[string]struct{}, len(other))
	for _, t := range other {
		set[t] = struct{}{}
	}
	for _, t := range phraseTokens {
		if _, ok := set[t]; ok {
			return t, true
		}
	}
	return "", false
}
