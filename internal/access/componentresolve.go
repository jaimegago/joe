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

	// ReasonComponentResolveNoPermittedMatch — components matched, and the
	// principal may read none of them. This is the row an operator debugging
	// a missing grant is looking for; the per-component deny rows beside it
	// name which components.
	ReasonComponentResolveNoPermittedMatch = "component_resolve_no_permitted_match"

	// ReasonComponentResolveMatch — at least one permitted candidate.
	ReasonComponentResolveMatch = "component_resolve_match"
)

// componentResolveSubkind tags the outcome row's context blob, mirroring how
// migration 015 describes the context column: a JSON blob carrying kind-specific
// specifics, with a sub-discriminator where one kind covers more than one event.
const componentResolveSubkind = "component_resolve"

// ComponentBindingSet is the governed graph evidence for one component: what it
// is bound to, and via which relations.
type ComponentBindingSet struct {
	ComponentID string
	Bindings    []graph.ComponentBinding

	// Truncated reports that the component has more bindings than the limit
	// returned. The evidence is a prefix, not a wrong answer.
	Truncated bool
}

// ComponentBindings is the guarded read behind the resolve-component tool. For
// each component in componentIDs, in order, it:
//
//  1. evaluates rbac.ActionRead on THAT COMPONENT and drops it silently on a
//     denial — a component the principal may not read is not a candidate, and
//     the caller is never told the difference between "denied" and "absent";
//  2. reads that component's bindings from the graph store; and
//  3. drops every binding whose PEER component the principal may not read, so
//     a permitted candidate cannot become a side channel disclosing the
//     existence of a component the grant does not cover.
//
// Every distinct component considered — candidate or peer — passes through the
// permit chokepoint exactly once per call, so the audit trail names each one
// once rather than once per edge.
//
// limit bounds the bindings returned PER COMPONENT; a non-positive limit means
// graph.DefaultComponentBindingLimit. The limit is applied by the store before
// the peer filter, so a set whose peers were all filtered out can be both empty
// and truncated — that is honest: the principal was shown everything they may
// see out of a prefix that was itself capped.
//
// An empty result is a normal answer and never an error. The error return is
// reserved for a graph store that is absent or failing.
func (a *Accessor) ComponentBindings(ctx context.Context, principal rbac.Principal, componentIDs []string, limit int) ([]ComponentBindingSet, error) {
	if limit <= 0 {
		limit = graph.DefaultComponentBindingLimit
	}

	// permitted memoises the decision per component id for this call. The
	// candidate list and the peer sets overlap heavily in practice — two
	// candidates commonly share a Prometheus — and a second permit for the
	// same component would add an audit row that says nothing new.
	permitted := make(map[string]bool, len(componentIDs))
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

	var sets []ComponentBindingSet
	for _, id := range componentIDs {
		if id == "" {
			continue
		}
		ok, err := allowed(id)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		if a.graph == nil {
			return nil, ErrGraphUnavailable
		}

		// limit+1 rows: the extra row is how truncation is detected without
		// the store having to report it, keeping ListComponentBindings a
		// plain ordered read.
		raw, err := a.graph.ListComponentBindings(ctx, id, limit+1)
		if err != nil {
			return nil, fmt.Errorf("list bindings for component %s: %w", id, err)
		}
		truncated := len(raw) > limit
		if truncated {
			raw = raw[:limit]
		}

		visible := make([]graph.ComponentBinding, 0, len(raw))
		for _, b := range raw {
			peerOK, err := allowed(b.PeerComponentID)
			if err != nil {
				return nil, err
			}
			if peerOK {
				visible = append(visible, b)
			}
		}

		sets = append(sets, ComponentBindingSet{
			ComponentID: id,
			Bindings:    visible,
			Truncated:   truncated,
		})
	}

	// Written only on the answering path. An error return means the call did
	// not fall into any of the three cases, and a row claiming one would be a
	// worse trail than none — the error is what happened.
	a.auditComponentResolveOutcome(ctx, principal, len(componentIDs), len(sets))
	return sets, nil
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
func (a *Accessor) auditComponentResolveOutcome(ctx context.Context, principal rbac.Principal, matched, permitted int) {
	if a.auditRepo == nil {
		return
	}

	reason := ReasonComponentResolveMatch
	switch {
	case matched == 0:
		reason = ReasonComponentResolveNoMatch
	case permitted == 0:
		reason = ReasonComponentResolveNoPermittedMatch
	}

	// Counts only. The task phrase itself is deliberately NOT recorded: it is
	// arbitrary operator prose arriving from the loop, the audit log is the
	// governance trail rather than a query log, and the component identities an
	// operator needs in order to act are already on the per-component rows this
	// row sits beside.
	blob, err := json.Marshal(map[string]any{
		"subkind":   componentResolveSubkind,
		"matched":   matched,
		"permitted": permitted,
	})
	if err != nil {
		// Marshalling a map of ints cannot fail; fall back to a valid empty
		// blob rather than writing a row the reader cannot parse.
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
