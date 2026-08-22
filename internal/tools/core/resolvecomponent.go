package core

import (
	"context"
	"fmt"

	"github.com/jaimegago/joe/internal/componentresolve"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/store"
)

// ResolveComponentClient defines the subset of the in-process core client
// needed for ResolveComponentTool. ListComponents is here for the empty case:
// when nothing matched, the tool answers with what IS reachable instead of
// leaving the model to decide whether to go and look.
type ResolveComponentClient interface {
	ResolveComponents(ctx context.Context, phrase, componentType string) (*componentresolve.Resolution, error)
	ListComponents(ctx context.Context) ([]*store.Component, error)
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

// The empty case carries the fallback WITH it rather than describing it.
//
// Before this, an empty result told the model to "try list_components" and
// the task prompt said an empty result "is not a wall". Both were prose, and
// on 2026-08-21 (joe-pm threads/da1-real-app.md, runs 20260821-210907-b6e2a2
// and 20260821-211242-2641c1) gemini-2.5-flash read candidate_count: 0 as
// "notification-service does not exist", stopped, and asked the operator which
// cluster to look in — with exactly one cluster registered. The invariant
// joe-pm ratified on 2026-08-22 (threads/joe-unresolved-phrase-fallback.md):
//
//	An unresolved phrase is never a terminal condition while at least one
//	component is reachable. Asking the operator for a cluster or namespace is
//	permitted only when more than one component is reachable and the phrase
//	does not disambiguate — never when there is exactly one.
//
// So the empty answer now includes the reachable components themselves and a
// directive keyed on how many there are. The model no longer has to choose to
// take a second hop to learn that the only place to look is oasis-lab; the
// hop is taken for it. This discloses nothing new: the list is the same
// ungoverned registry read list_components already returns whole (see
// inProcessCoreClient.ResolveComponents), and the reason string above is
// unchanged, so not-found and not-permitted remain indistinguishable.
//
// maxReachableComponents bounds the list so the empty answer cannot grow
// with the registry; reachable_truncated says when it was cut.
const maxReachableComponents = 25

const (
	fallbackNoneReachable = "No component is reachable at all, so there is nowhere to look for what the phrase names. Say so plainly."
	fallbackOneReachable  = "Exactly one component is reachable. Whatever the phrase names is not a registered component, so it lives INSIDE that one component — a workload, namespace, repository path or similar. Continue the investigation there with the tools you have (list resources, read logs and events) using its component_id. Do NOT ask the operator which cluster, namespace or component to look in: there is only one, and asking is not an answer."
	fallbackManyReachable = "More than one component is reachable. Whatever the phrase names is not a registered component, so it lives inside one of them. Read the task for an environment, region, type or other qualifier and pick the component it points at; if several still fit, investigate them in turn. Ask the operator which to use only if the task genuinely does not disambiguate and investigating each is not practical."
)

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
		"Empty bindings mean no edge you may see has been derived for that component, NOT that the candidate is wrong — and the bindings shown are never a complete account of what the component is attached to. " +
		"`bindings_truncated` means you may see more relations for that candidate than were returned; `candidates_truncated` means more components you may see matched than were returned; `matches_truncated` means more components matched the phrase than were examined. " +
		"`max_candidates`, `max_bindings_per_candidate` and `max_total_bindings` are the bounds actually in force, so narrow the phrase rather than re-asking when a truncation flag is set. " +
		"AN EMPTY RESULT IS AN ANSWER, NOT AN ERROR, and it does not tell you whether nothing matched or nothing matched that you may see — so do not report the phrase as naming something that does not exist. " +
		"An empty result carries `reachable_components`: the components you can see, and a `next` directive keyed on how many there are. " +
		"An unresolved phrase is never a reason to stop while at least one component is reachable — the phrase names something INSIDE a component (a workload, a namespace, a path), so continue the investigation inside the reachable components using their component_id. " +
		"If exactly one component is reachable, investigate inside it and never ask the operator which cluster or namespace to use. " +
		"Ask the operator only when more than one component is reachable and the task does not say which."
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

	// Two truncation flags, because they answer different questions and a
	// caller that conflates them draws the wrong conclusion from an empty
	// answer. matches_truncated is a fact about the REGISTRY — more components
	// matched than were examined — and it is disclosable because the registry is
	// ungoverned and list_components already returns it whole. candidates_truncated
	// is a fact about THIS CALLER — more components they may see matched than
	// were returned. The bounds behind each are reported beside them.
	out := map[string]any{
		"phrase":                     phrase,
		"candidates":                 candidates,
		"candidate_count":            len(candidates),
		"matches_truncated":          resolution.MatchesTruncated,
		"candidates_truncated":       resolution.CandidatesTruncated,
		"max_candidates":             resolution.MaxCandidates,
		"max_bindings_per_candidate": resolution.MaxBindingsPerCandidate,
		"max_total_bindings":         resolution.MaxTotalBindings,
	}
	if componentType != "" {
		out["type_filter"] = componentType
	}
	if len(candidates) == 0 {
		out["reason"] = noCandidatesReason
		if err := t.attachReachable(ctx, out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// attachReachable adds the reachable-component fallback to an empty answer.
// A failure to list is an error in the same sense a failure to resolve is: the
// caller asked a well-formed question and the registry could not be read.
func (t *ResolveComponentTool) attachReachable(ctx context.Context, out map[string]any) error {
	components, err := t.client.ListComponents(ctx)
	if err != nil {
		return fmt.Errorf("resolve component fallback: list components failed: %w", err)
	}

	truncated := false
	if len(components) > maxReachableComponents {
		components = components[:maxReachableComponents]
		truncated = true
	}
	reachable := make([]map[string]any, 0, len(components))
	for _, c := range components {
		reachable = append(reachable, map[string]any{
			"component_id": c.ID,
			"name":         c.Name,
			"type":         c.Type,
		})
	}

	out["reachable_components"] = reachable
	out["reachable_component_count"] = len(reachable)
	out["reachable_truncated"] = truncated
	switch {
	case len(reachable) == 0:
		out["next"] = fallbackNoneReachable
	case len(reachable) == 1 && !truncated:
		out["next"] = fallbackOneReachable
	default:
		out["next"] = fallbackManyReachable
	}
	return nil
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
