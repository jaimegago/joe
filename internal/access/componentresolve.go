package access

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/rbac"
)

// Component resolution reads the graph, but it is gated on the COMPONENT, not
// on the reserved GraphComponentID the other graph methods use.
//
// The reserved key exists because a graph query is not keyed by any real
// infrastructure source (see the comment at the top of graph.go): there is
// nothing else to gate on, so a stable reserved key standing in for "the graph"
// is the honest choice. Component resolution is the case that key was never
// needed for — every read here is "the graph context OF component X", so it is
// gated on X itself. That is strictly narrower than the reserved key, and it is
// what makes the per-principal filter below meaningful: a principal who may not
// read a component learns nothing about it, not even that it exists.

// Audit reasons for the one per-call outcome row ComponentBindings writes. They
// are what makes the two empty answers distinguishable to an OPERATOR while
// they stay indistinguishable to the caller — nothing matched, versus something
// matched that the principal may not see. The caller gets one empty result with
// one reason either way; the trail keeps the difference.
//
// The row is KindInfraAccess with an empty component and decision "allow": it
// records that the resolve call was answered, and the case it fell into, not an
// authorization outcome — the authorization outcomes are the per-component rows
// the permit chokepoint writes alongside it, which carry decision "deny" and
// the policy's own reason for exactly the components that were withheld.
const (
	// ReasonComponentResolveNoMatch — nothing in the registry matched.
	ReasonComponentResolveNoMatch = "component_resolve_no_match"

	// ReasonComponentResolveNoPermittedMatch — components matched, EVERY ONE OF
	// THEM WAS EVALUATED, and the principal may read none. This is the row an
	// operator debugging a missing grant is looking for; the per-component deny
	// rows beside it name which components.
	ReasonComponentResolveNoPermittedMatch = "component_resolve_no_permitted_match"

	// ReasonComponentResolveBoundedNoPermittedMatch — components matched, the
	// match step stopped at its work bound, and none of the prefix it did
	// evaluate was permitted. The empty answer may be the bound rather than the
	// grant: components this principal can read may have matched and sorted
	// below the cut, in which case permission on them was never evaluated.
	//
	// It exists because the row above is a claim ABOUT PERMISSION, and the audit
	// half is the only place the two empty answers stay distinguishable at all.
	// A permission claim standing in for a bound effect corrupts the one record
	// an operator has, and it corrupts it in the direction that sends them to
	// look at grants that were never the problem.
	ReasonComponentResolveBoundedNoPermittedMatch = "component_resolve_bounded_no_permitted_match"

	// ReasonComponentResolveMatch — at least one permitted candidate.
	ReasonComponentResolveMatch = "component_resolve_match"
)

// componentResolveSubkind tags the outcome row's context blob, mirroring how
// migration 015 describes the context column: a JSON blob carrying kind-specific
// specifics, with a sub-discriminator where one kind covers more than one event.
const componentResolveSubkind = "component_resolve"

// The bounds on one resolution, and the rule they are all built on: A BOUND IS
// SPENT ON AN ORDERING THE PRINCIPAL IS ENTITLED TO, OR ITS EFFECT IS NOT
// DISCLOSED TO THEM.
//
// The two bounds a caller can see — MaxResolveCandidates here, and the
// per-candidate share below — are applied after the permit, so what comes back
// is a prefix of that principal's own ranking rather than a filtered prefix of
// everyone's. The two WORK bounds behind them — componentresolve.MaxMatchScan
// and bindingScanFactor — are applied before it, necessarily, because filtering
// costs a permit per row and unbounded filtering is unbounded work. Neither is
// reported to the caller, because a count derived before the filter and read
// beside one derived after it discloses the difference: how much of this
// component's graph reaches components the grant does not cover. They are
// recorded on the per-call audit row instead, which is where the operator side
// of every other resolve distinction already lives.
const (
	// MaxResolveCandidates is the OUTPUT bound on candidates: how many
	// permitted components one call returns. Applied after the permit, over the
	// ranked ordering the principal is entitled to.
	MaxResolveCandidates = 25

	// MaxResolveBindings is the TOTAL evidence budget for one call, across
	// every candidate. It exists because a per-candidate bound and a candidate
	// bound compose MULTIPLICATIVELY, and a product neither bounded nor
	// reported is not a contract. It is allocated as an equal share per
	// surviving candidate, capped at graph.DefaultComponentBindingLimit — so
	// the common few-candidate call still gets the full per-component evidence
	// and only a wide answer pays.
	MaxResolveBindings = 250

	// bindingScanFactor multiplies the per-candidate share to get the WORK
	// bound on rows read from the graph store for that candidate. It is what
	// makes the mirror case rare rather than routine: evidence the principal IS
	// entitled to is withheld only once a component's edges to components they
	// may NOT see outnumber their own by this factor.
	//
	// It also bounds the whole call: the shares sum to at most
	// MaxResolveBindings, so the rows read sum to at most that times this, and
	// each row costs at most one peer permit.
	bindingScanFactor = 4
)

// ComponentResolveRequest is one governed resolution.
type ComponentResolveRequest struct {
	// ComponentIDs is the ranked match prefix to evaluate, strongest first.
	// Order is the contract: the candidate bound takes a prefix of it after
	// the permit, so a caller that hands over an unranked list gets an
	// arbitrary subset of what it is entitled to.
	ComponentIDs []string

	// MatchesBounded reports that the match step which produced ComponentIDs
	// stopped at its own work bound, so this is a prefix of what matched. It
	// changes no answer. It exists so the audit row cannot claim a permission
	// cause for a bound effect: with it set, "none of these was permitted" is
	// not the same statement as "nothing you may see matched".
	MatchesBounded bool

	// PerComponentLimit and TotalBindingBudget LOWER the evidence bounds. Zero
	// or negative means the package default; a value above the ceiling is
	// clamped to it, so a caller may narrow an answer and never widen one.
	// Production passes zero for both.
	PerComponentLimit  int
	TotalBindingBudget int
}

// ComponentResolveResult is one governed resolution's answer, together with the
// bounds that shaped it. The bounds are returned rather than assumed by the
// caller because the per-candidate share is computed here, from how many
// candidates survived the permit.
type ComponentResolveResult struct {
	// Sets is one entry per permitted candidate, in the request's order.
	Sets []ComponentBindingSet

	// CandidatesTruncated reports that more components the principal may read
	// matched than were returned. It is a claim about THEIR ranking, which is
	// what makes it safe to disclose.
	CandidatesTruncated bool

	// MaxCandidates, PerComponentLimit and TotalBindingBudget are the bounds
	// actually in force for this call.
	MaxCandidates      int
	PerComponentLimit  int
	TotalBindingBudget int
}

// ComponentBindingSet is the governed graph evidence for one component: what it
// is bound to, and via which relations.
type ComponentBindingSet struct {
	ComponentID string
	Bindings    []graph.ComponentBinding

	// Truncated reports that the component has more bindings THE PRINCIPAL MAY
	// SEE than were returned. The evidence is a prefix of their own entitled
	// set, not a wrong answer.
	//
	// It is derived from the VISIBLE bindings, after the peer filter. Derived
	// from the raw row count instead — which is what it used to be — it would
	// pair with the post-filter binding count the tool reports beside it to
	// disclose how many of this component's edges reach components outside the
	// grant. Cardinality only, but on the governed side, and precisely the side
	// channel the peer filter exists to close.
	Truncated bool
}

// ComponentBindings is the guarded read behind the resolve-component tool. It
// runs in two passes, and the split is the governance, not a refactor.
//
// PASS ONE decides the candidate set. For each id in req.ComponentIDs, in rank
// order, it evaluates rbac.ActionRead on THAT COMPONENT and drops it silently on
// a denial — a component the principal may not read is not a candidate, and the
// caller is never told the difference between "denied" and "absent". The
// candidate bound is spent HERE, on the permitted list, which is what makes the
// answer a prefix of the ranking this principal is entitled to. Spent one pass
// earlier it would take a prefix of everyone's ranking and then filter it, so a
// principal whose own components sort below the bound would receive the empty
// answer while never having been evaluated at all.
//
// PASS TWO assembles the evidence. For each surviving candidate it reads that
// component's bindings from the graph store and drops every binding whose PEER
// component the principal may not read, so a permitted candidate cannot become a
// side channel disclosing the existence of a component the grant does not cover.
// The per-candidate bound is spent on what SURVIVES that filter, so Truncated is
// a statement about the principal's own evidence and discloses nothing about the
// edges dropped from it.
//
// Every distinct component considered — candidate or peer — passes through the
// permit chokepoint exactly once per call, so the audit trail names each one
// once rather than once per edge.
//
// An empty result is a normal answer and never an error. The error return is
// reserved for a graph store that is absent or failing; as before, it fires only
// once a permitted candidate exists, so a principal with no grants gets the
// benign empty answer on a graph-less deployment rather than an error.
func (a *Accessor) ComponentBindings(ctx context.Context, principal rbac.Principal, req ComponentResolveRequest) (ComponentResolveResult, error) {
	budget := req.TotalBindingBudget
	if budget <= 0 {
		budget = MaxResolveBindings
	}

	// permitted memoises the decision per component id for this call. The
	// candidate list and the peer sets overlap heavily in practice — two
	// candidates commonly share a Prometheus — and a second permit for the
	// same component would add an audit row that says nothing new.
	permitted := make(map[string]bool, len(req.ComponentIDs))
	allowed := func(componentID string) (bool, error) {
		if v, ok := permitted[componentID]; ok {
			return v, nil
		}
		err := a.permitForPrincipal(ctx, principal, componentID, rbac.ActionRead)
		switch {
		case err == nil:
			permitted[componentID] = true
			return true, nil
		case errors.Is(err, ErrPermissionDenied):
			permitted[componentID] = false
			return false, nil
		default:
			// A non-denial error from permit is an audit-write failure on a
			// fail-closed path. Reads are fail-open, so this branch is not
			// reachable for ActionRead today; propagate rather than silently
			// treating an infrastructure failure as a denial, which would
			// present a broken audit trail as an empty answer.
			return false, err
		}
	}

	// Pass one. One permitted id past the bound is collected and then dropped:
	// that extra decision is how "there are more you may see" is established
	// exactly, without scanning the rest of the match prefix.
	candidates := make([]string, 0, MaxResolveCandidates+1)
	for _, id := range req.ComponentIDs {
		if id == "" {
			continue
		}
		ok, err := allowed(id)
		if err != nil {
			return ComponentResolveResult{}, err
		}
		if !ok {
			continue
		}
		candidates = append(candidates, id)
		if len(candidates) > MaxResolveCandidates {
			break
		}
	}
	candidatesTruncated := len(candidates) > MaxResolveCandidates
	if candidatesTruncated {
		candidates = candidates[:MaxResolveCandidates]
	}

	perCandidate := perCandidateShare(budget, len(candidates), req.PerComponentLimit)
	scan := perCandidate * bindingScanFactor

	// Pass two.
	sets := make([]ComponentBindingSet, 0, len(candidates))
	evidenceBounded := 0
	for _, id := range candidates {
		if a.graph == nil {
			return ComponentResolveResult{}, ErrGraphUnavailable
		}

		// scan+1 rows: the extra row is how the work bound is detected without
		// the store having to report it, keeping ListComponentBindings a
		// plain ordered read.
		raw, err := a.graph.ListComponentBindings(ctx, id, scan+1)
		if err != nil {
			return ComponentResolveResult{}, fmt.Errorf("list bindings for component %s: %w", id, err)
		}
		scanBounded := len(raw) > scan
		if scanBounded {
			raw = raw[:scan]
		}

		visible := make([]graph.ComponentBinding, 0, perCandidate)
		truncated := false
		for _, b := range raw {
			peerOK, err := allowed(b.PeerComponentID)
			if err != nil {
				return ComponentResolveResult{}, err
			}
			if !peerOK {
				continue
			}
			if len(visible) == perCandidate {
				// One visible binding past the bound is enough to know the
				// evidence is a prefix. Stopping here also stops permitting
				// peers this answer will not carry.
				truncated = true
				break
			}
			visible = append(visible, b)
		}
		if scanBounded && !truncated {
			// The output bound never bit, so the work bound is the only thing
			// that could have shortened this candidate's evidence. That is the
			// mirror case, and it is counted for the audit row rather than
			// reported to the caller — see the bounds comment above.
			evidenceBounded++
		}

		sets = append(sets, ComponentBindingSet{
			ComponentID: id,
			Bindings:    visible,
			Truncated:   truncated,
		})
	}

	// Written only on the answering path. An error return means the call did
	// not fall into any of the four cases, and a row claiming one would be a
	// worse trail than none — the error is what happened.
	a.auditComponentResolveOutcome(ctx, principal, resolveOutcome{
		matched:           len(req.ComponentIDs),
		permitted:         len(sets),
		matchesBounded:    req.MatchesBounded,
		candidatesBounded: candidatesTruncated,
		evidenceBounded:   evidenceBounded,
	})

	return ComponentResolveResult{
		Sets:                sets,
		CandidatesTruncated: candidatesTruncated,
		MaxCandidates:       MaxResolveCandidates,
		PerComponentLimit:   perCandidate,
		TotalBindingBudget:  budget,
	}, nil
}

// perCandidateShare allocates the total evidence budget across the candidates
// that survived the permit.
//
// The allocation is an equal share rather than first-come-first-served, so a
// weakly-ranked candidate still carries enough evidence to be told apart from
// the one above it — which is the entire job of the payload. First-come would
// spend the whole budget on the strongest candidates and hand the rest empty
// bindings, indistinguishable at the tool boundary from a component no refresher
// has drawn an edge for.
//
// override lowers the share; it is clamped to the same ceiling as the derived
// value, so a caller may narrow an answer and never widen one.
func perCandidateShare(budget, candidates, override int) int {
	share := budget
	if candidates > 0 {
		share = budget / candidates
	}
	if override > 0 {
		share = override
	}
	if share > graph.DefaultComponentBindingLimit {
		share = graph.DefaultComponentBindingLimit
	}
	if share < 1 {
		share = 1
	}
	return share
}

// resolveOutcome is what one resolve call has to say about itself: how far it
// got, and which of its bounds bit. The three bound flags are here and not in
// the caller's answer because that is the whole asymmetry this type exists for —
// a bound applied before the peer filter is a fact about components outside the
// grant, so the operator gets it and the caller does not.
type resolveOutcome struct {
	// matched is how many ids the call was handed; permitted is how many
	// candidates it returned.
	matched   int
	permitted int

	// matchesBounded, candidatesBounded and evidenceBounded say which bounds
	// bit: the match work bound upstream, the candidate output bound here, and
	// the per-candidate evidence work bound, the last being a COUNT of
	// candidates whose evidence may be short because of it.
	matchesBounded    bool
	candidatesBounded bool
	evidenceBounded   int
}

// auditComponentResolveOutcome writes the one per-call row that records WHICH
// empty answer the caller got. It is written for every call, including the
// all-permitted one, so the trail reads as a per-call record rather than as an
// exception log where absence has to be interpreted.
//
// It follows the accessor's §4 failure split for reads: an audit-write failure
// is logged loudly and the call PROCEEDS. A resolve answer withheld because a
// bookkeeping row could not be written would be a worse outcome than a gap in
// the trail, and the authorization decisions themselves — the rows that matter
// for a grant — are written by permit under the same posture.
func (a *Accessor) auditComponentResolveOutcome(ctx context.Context, principal rbac.Principal, out resolveOutcome) {
	if a.auditRepo == nil {
		return
	}

	// The bounded case is separated from the permission case, and it is checked
	// first: with the match prefix cut, "none of these was permitted" is a claim
	// about the prefix and not about what matched.
	reason := ReasonComponentResolveMatch
	switch {
	case out.matched == 0:
		reason = ReasonComponentResolveNoMatch
	case out.permitted == 0 && out.matchesBounded:
		reason = ReasonComponentResolveBoundedNoPermittedMatch
	case out.permitted == 0:
		reason = ReasonComponentResolveNoPermittedMatch
	}

	// Counts and flags only. The task phrase itself is deliberately NOT
	// recorded: it is arbitrary operator prose arriving from the loop, the audit
	// log is the governance trail rather than a query log, and the component
	// identities an operator needs in order to act are already on the
	// per-component rows this row sits beside.
	blob, err := json.Marshal(map[string]any{
		"subkind":            componentResolveSubkind,
		"matched":            out.matched,
		"permitted":          out.permitted,
		"matches_bounded":    out.matchesBounded,
		"candidates_bounded": out.candidatesBounded,
		"evidence_bounded":   out.evidenceBounded,
	})
	if err != nil {
		// Marshalling a map of ints and bools cannot fail; fall back to a valid
		// empty blob rather than writing a row the reader cannot parse.
		blob = []byte("{}")
	}

	auditErr := a.auditRepo.Insert(ctx, audit.Event{
		Principal: string(principal),
		Action:    string(rbac.ActionRead),
		Decision:  audit.DecisionAllow,
		Reason:    reason,
		Kind:      audit.KindInfraAccess,
		Context:   string(blob),
	})
	if auditErr != nil {
		audit.FailurePosture(ctx, string(rbac.ActionRead), auditErr,
			"accessor:component_resolve", audit.FailOpen)
	}
}
