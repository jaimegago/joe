package skills

import (
	"sort"
	"strings"
	"unicode"
)

// MaxActiveSkills bounds how many skills the router activates for any single
// query. See docs/reference/joe-skills-design.md "Skill router context budget".
const MaxActiveSkills = 3

// minTokenLen drops 1- and 2-character tokens before scoring — they generate
// far more noise than signal (e.g. "is", "to", "a") and rarely carry the
// domain meaning a skill description is keyed on.
const minTokenLen = 3

// Router selects which skills to load into context for a given user query.
// Phase 1 uses simple token-overlap scoring against each skill's name and
// description. Body text is intentionally excluded — the description is the
// contract the skill author signs for routing.
type Router struct {
	registry *Registry
	tokens   map[string]map[string]struct{} // skill name → token set
}

// NewRouter prepares a router over the given registry. Tokenization happens
// once at construction; matching is a cheap set intersection per query.
func NewRouter(registry *Registry) *Router {
	r := &Router{
		registry: registry,
		tokens:   map[string]map[string]struct{}{},
	}
	if registry == nil {
		return r
	}
	for _, s := range registry.All() {
		r.tokens[s.Name] = tokenize(s.Name + " " + s.Description)
	}
	return r
}

// Registry returns the underlying registry the router was built over. May be
// nil. Useful for diagnostics and status endpoints in later phases.
func (r *Router) Registry() *Registry {
	if r == nil {
		return nil
	}
	return r.registry
}

// Match returns up to MaxActiveSkills skills whose name or description shares
// tokens with the query. Skills are ranked by overlap count; ties are broken
// by skill name for determinism. An empty result means no skill matched and
// the caller should proceed with no skill content appended.
func (r *Router) Match(query string) []*Skill {
	if r == nil || r.registry == nil || r.registry.Len() == 0 {
		return nil
	}
	queryTokens := tokenize(query)
	if len(queryTokens) == 0 {
		return nil
	}

	type scored struct {
		skill *Skill
		score int
	}
	var hits []scored
	for _, s := range r.registry.All() {
		skillTokens := r.tokens[s.Name]
		score := 0
		for t := range queryTokens {
			if _, ok := skillTokens[t]; ok {
				score++
			}
		}
		if score > 0 {
			hits = append(hits, scored{skill: s, score: score})
		}
	}
	if len(hits) == 0 {
		return nil
	}

	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].skill.Name < hits[j].skill.Name
	})

	if len(hits) > MaxActiveSkills {
		hits = hits[:MaxActiveSkills]
	}
	out := make([]*Skill, len(hits))
	for i, h := range hits {
		out[i] = h.skill
	}
	return out
}

// RenderPromptSection formats the matched skills as an addition to a system
// prompt. Returns "" when no skills matched, so callers can concatenate
// unconditionally.
func RenderPromptSection(matched []*Skill) string {
	if len(matched) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("SENIOR-SRE JUDGMENT FRAMES — apply the relevant ones when reasoning about this request:\n\n")
	for i, s := range matched {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("## Skill: ")
		b.WriteString(s.Name)
		b.WriteString("\n")
		b.WriteString(s.Description)
		b.WriteString("\n\n")
		b.WriteString(s.Body)
	}
	return b.String()
}

// tokenize lowercases the input and splits it on non-letter/digit runes,
// dropping tokens shorter than minTokenLen and a small stop-word list.
func tokenize(s string) map[string]struct{} {
	out := map[string]struct{}{}
	if s == "" {
		return out
	}
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	for _, f := range fields {
		if len(f) < minTokenLen {
			continue
		}
		if _, stop := stopWords[f]; stop {
			continue
		}
		out[f] = struct{}{}
	}
	return out
}

// stopWords is a tiny English stop list. Skill descriptions and user queries
// are short enough that aggressive filtering hurts more than it helps; this
// only knocks out the most common high-frequency function words.
var stopWords = map[string]struct{}{
	"the": {}, "and": {}, "for": {}, "are": {}, "but": {},
	"not": {}, "you": {}, "all": {}, "can": {}, "has": {},
	"with": {}, "this": {}, "that": {}, "from": {}, "what": {},
	"how": {}, "why": {}, "when": {}, "where": {}, "into": {},
	"out": {}, "any": {}, "use": {}, "via": {}, "per": {},
	"its": {}, "have": {}, "had": {}, "was": {}, "were": {},
	"been": {}, "your": {}, "our": {}, "their": {},
}
