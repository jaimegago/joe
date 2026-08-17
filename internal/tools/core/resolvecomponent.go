package core

import (
	"context"
	"fmt"

	"github.com/jaimegago/joe/internal/componentresolve"
	"github.com/jaimegago/joe/internal/llm"
)

// ResolveComponentClient defines the subset of the in-process core client
// needed for ResolveComponentTool.
type ResolveComponentClient interface {
	ResolveComponents(ctx context.Context, phrase, componentType string) (*componentresolve.Resolution, error)
}

// noCandidatesReason is the single reason string an empty result carries. It is
// the SAME string whether nothing matched or everything that matched was
// withheld from this caller, and that identity is the point: a successful
// resolve discloses not just that a component exists but what it is bound to,
// and this tool's consumer paraphrases what it learns into transcripts,
// summaries and incident notes. If not-found and not-permitted differed here,
// the difference would reach someone who should not have it.
//
// The distinction is not lost, it is relocated: the accessor writes one audit
// row per call recording which case occurred, plus a deny row naming each
// withheld component. The operator debugging a missing grant reads it there.
// The cost is accepted knowingly — a missing grant presents to the caller as a
// component that does not exist — which is why the audit half is not optional.
const noCandidatesReason = "no component in the registry matched this phrase, or none that matched is visible to you"

// ResolveComponentTool resolves a task phrase to the components it might name.
//
// It is the NAMING hop: prose in, ranked component candidates out. It is not
// the binding hop — no tool exposes backend resolution to the loop, and
// prometheus_query and its peers keep resolving their own backends server-side.
//
// The contract has two halves that pull in opposite directions on purpose.
// MATCH NARROW: matching is deterministic on component name and type, and
// qualifiers in the phrase ("in prod") are never resolved here. RETURN RICH:
// every candidate carries the graph relations binding it to other components,
// so the caller can disambiguate by reading evidence. Matching stays
// deterministic and the fuzzy step happens in the fuzzy reasoner, which already
// has the whole phrase. The alternative — resolving qualifiers inside the
// resolver — collapses to one authoritative-looking component_id with the
// ambiguity already discarded, which the caller cannot recover from in the way
// it can recover from a bad ranking.
//
// SEVERAL CANDIDATES IS THE NORMAL CASE. Nothing here treats an ambiguous
// result as a failure, and there is no error shape for an empty one either:
// empty is an answer.
type ResolveComponentTool struct {
	client ResolveComponentClient
}

// NewResolveComponentTool creates a new resolve_component tool.
func NewResolveComponentTool(c ResolveComponentClient) *ResolveComponentTool {
	return &ResolveComponentTool{client: c}
}

func (t *ResolveComponentTool) Name() string { return "resolve_component" }

// Description carries everything about USING the result — how to read a
// candidate's evidence, what to do with several candidates, when to fall back —
// because a tool description is read only once the model is already considering
// the tool. What it cannot carry is ORDERING: "resolve before acting" has to
// reach the model before it picks a tool, so that one rule lives in the task
// system prompt and nothing else does.
func (t *ResolveComponentTool) Description() string {
	return "Resolve a task phrase to the registered infrastructure components it might name. " +
		"Give it the phrase as the task worded it (\"the checkout app in prod\") — do NOT strip it down to a bare name first. " +
		"Matching is deterministic on component name and type; qualifiers like \"in prod\" are NOT interpreted here. " +
		"SEVERAL CANDIDATES IS NORMAL AND NOT A FAILURE. Each candidate carries match_kind (exact_name, name_in_phrase, name_token or type — strongest first) " +
		"and bindings: the graph relations tying it to other components, each naming the relation, the direction, and the peer component. " +
		"Disambiguate by reading the bindings against the rest of the task — a candidate whose bindings reach the environment, backend or repository the task talks about is the one the task means. " +
		"Ask for the component_id of the candidate you pick when calling other tools. " +
		"Empty bindings mean no edge has been derived for that component yet, NOT that the candidate is wrong. " +
		"`bindings_truncated` means you were shown a prefix of that candidate's relations; `matches_truncated` means more components matched than were returned. " +
		"AN EMPTY RESULT IS AN ANSWER, NOT AN ERROR, and it does not tell you whether nothing matched or nothing matched that you may see — so do not report the phrase as naming something that does not exist. " +
		"On an empty result, fall back: try list_components, try a different phrase from the task, or carry on with the tools you can reach and say what you could not resolve."
}

func (t *ResolveComponentTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"phrase": {
				Type:        "string",
				Description: "The phrase from the task that names a component, as the task worded it.",
			},
			"type": {
				Type:        "string",
				Description: "Optional: restrict candidates to this component type (kubernetes, prometheus, git, …). This is a filter you apply, never something read out of the phrase.",
			},
		},
		Required: []string{"phrase"},
	}
}

func (t *ResolveComponentTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	phrase, ok := args["phrase"].(string)
	if !ok || phrase == "" {
		return nil, fmt.Errorf("missing required parameter: phrase")
	}
	componentType, _ := args["type"].(string)

	// A malformed call IS an error — the caller failed to say what to resolve.
	// An empty answer to a well-formed call is not, and the two must not be
	// conflated: the no-error-shape rule is about absence and non-permission,
	// not about arguments.
	resolution, err := t.client.ResolveComponents(ctx, phrase, componentType)
	if err != nil {
		return nil, fmt.Errorf("resolve component failed: %w", err)
	}

	candidates := make([]map[string]any, 0, len(resolution.Candidates))
	for _, c := range resolution.Candidates {
		candidates = append(candidates, map[string]any{
			"component_id":       c.ComponentID,
			"name":               c.Name,
			"type":               c.Type,
			"match_kind":         c.MatchKind,
			"matched_text":       c.MatchText,
			"bindings":           bindingViews(c),
			"binding_count":      len(c.Bindings),
			"bindings_truncated": c.BindingsTruncated,
		})
	}

	out := map[string]any{
		"phrase":            phrase,
		"candidates":        candidates,
		"candidate_count":   len(candidates),
		"matches_truncated": resolution.MatchesTruncated,
	}
	if componentType != "" {
		out["type_filter"] = componentType
	}
	if len(candidates) == 0 {
		out["reason"] = noCandidatesReason
	}
	return out, nil
}

// bindingViews flattens one candidate's graph evidence into the tool's wire
// shape. Direction is carried because these relations are directional and mean
// different things from each end: metrics_in OUT of a service names the backend
// scraping it, metrics_in IN to a Prometheus component names a service it
// scrapes.
func bindingViews(c componentresolve.Candidate) []map[string]any {
	views := make([]map[string]any, 0, len(c.Bindings))
	for _, b := range c.Bindings {
		views = append(views, map[string]any{
			"relation":          b.Relation,
			"direction":         b.Direction,
			"node_id":           b.NodeID,
			"node_type":         b.NodeType,
			"peer_component_id": b.PeerComponentID,
			"peer_node_id":      b.PeerNodeID,
			"peer_node_type":    b.PeerNodeType,
			"confidence":        b.Confidence.String(),
		})
	}
	return views
}
