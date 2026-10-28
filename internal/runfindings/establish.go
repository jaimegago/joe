package runfindings

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/rbac"
)

// WeightProvider supplies the operator's weight for a component. No such label
// exists today, so UniformWeight is the only implementation; the seam is here
// from the start so the label is a field addition, not a redesign.
type WeightProvider interface {
	Weight(ctx context.Context, componentID string) (float64, error)
}

// UniformWeight weighs every component DefaultWeight.
type UniformWeight struct{}

// Weight implements WeightProvider.
func (UniformWeight) Weight(context.Context, string) (float64, error) { return DefaultWeight, nil }

// EstablishInput is what a run hands the finding pipeline. Everything the run
// record owns arrives here as an input; nothing is derived from the run.
type EstablishInput struct {
	// Reader is the guarded graph read (the accessor). Every read is made as
	// Principal.
	Reader GraphReader
	// Principal is the identity the run's reads resolve to. Empty means
	// agent:core — the run's principal by design (docs/backlog/autojoe-ro.md
	// § Principal); a caller passes one only to make the default explicit.
	Principal rbac.Principal
	// Weight is nil for UniformWeight.
	Weight WeightProvider
	// Scope is the run's resolved component set. A candidate whose subject
	// lies outside it is not emitted, and neither is a proposal whose target
	// does.
	Scope Set

	RunID     string
	ScopeKey  string
	SessionID string
	// Now is the establishment time written on every finding. The caller
	// supplies it so a batch shares one timestamp.
	Now time.Time
}

// DroppedKind says what a Dropped entry is about.
type DroppedKind string

const (
	DroppedFinding  DroppedKind = "finding"
	DroppedProposal DroppedKind = "proposal"
)

// Dropped records a candidate, or a candidate's proposal, that was not emitted
// and why. It is an observation for the run's limits, never a finding.
type Dropped struct {
	Kind DroppedKind `json:"kind"`
	// Index is the candidate's position in the block.
	Index int `json:"index"`
	// Key is set when the candidate was well-formed enough to have one.
	Key    string `json:"key,omitempty"`
	Reason string `json:"reason"`
}

// Establish turns the model's candidates into finding records: it validates
// each against the finding model, resolves the subject node and its component
// from the graph, checks scope, derives reach from the graph, reads weight,
// computes the band through the fixed table, validates the proposal, and
// derives the key. Candidates that fail are returned as Dropped with the
// reason. The findings come back unlabelled (Label empty); LabelAgainstPrevious assigns one.
//
// A graph read error — including a permit denial at the accessor — is
// returned as an error: it is the pipeline failing, not a candidate, and a run
// must not quietly deliver a shorter list because the store was unreachable or
// the principal was refused.
func Establish(ctx context.Context, cands []Candidate, in EstablishInput) ([]Finding, []Dropped, error) {
	if in.Reader == nil {
		return nil, nil, errors.New("establish: graph reader is nil")
	}
	if in.Scope == nil {
		return nil, nil, errors.New("establish: scope is nil")
	}
	if in.Principal == "" {
		p, err := rbac.AgentCorePrincipal()
		if err != nil {
			return nil, nil, fmt.Errorf("establish: %w", err)
		}
		in.Principal = p
	}
	weight := in.Weight
	if weight == nil {
		weight = UniformWeight{}
	}
	now := in.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}

	var (
		out     []Finding
		dropped []Dropped
		seen    = make(map[string]struct{})
	)
	drop := func(kind DroppedKind, i int, key, reason string) {
		dropped = append(dropped, Dropped{Kind: kind, Index: i, Key: key, Reason: reason})
	}

	for i, c := range cands {
		t, ok := ParseType(c.Type)
		if !ok {
			drop(DroppedFinding, i, "", fmt.Sprintf("type %q is not one of issue, risk, graph_update", c.Type))
			continue
		}
		subject := strings.TrimSpace(c.SubjectNodeID)
		claim := strings.TrimSpace(c.Claim)
		if subject == "" || claim == "" {
			drop(DroppedFinding, i, "", "subject_node_id and claim are required")
			continue
		}
		key := ComputeKey(t, subject, claim)
		if _, dup := seen[key]; dup {
			drop(DroppedFinding, i, key, "duplicate of an earlier candidate (same type, subject and claim)")
			continue
		}
		cons, ok := ParseConsequence(c.Consequence)
		if !ok || !cons.AdmissibleFor(t) {
			drop(DroppedFinding, i, key, fmt.Sprintf("consequence %q is not admissible for type %s", c.Consequence, t))
			continue
		}
		condition := strings.TrimSpace(c.Condition)
		if t == TypeRisk && condition == "" {
			drop(DroppedFinding, i, key, "a risk that cannot name its condition is not emitted")
			continue
		}
		evidence := strings.TrimSpace(c.EvidenceLine)
		if evidence == "" {
			drop(DroppedFinding, i, key, "a finding carries its derivation or is not emitted: evidence_line is empty")
			continue
		}

		node, err := in.Reader.GraphGetNode(ctx, in.Principal, subject)
		if err != nil && !errors.Is(err, graph.ErrNodeNotFound) {
			return nil, nil, fmt.Errorf("establish: read subject node %s: %w", subject, err)
		}
		if node == nil {
			drop(DroppedFinding, i, key, fmt.Sprintf("subject node %q is not in the graph", subject))
			continue
		}
		if !in.Scope(node.ComponentID) {
			drop(DroppedFinding, i, key, fmt.Sprintf("subject node %q belongs to component %q, outside the run's resolved component set", subject, node.ComponentID))
			continue
		}

		reach, err := ComputeReach(ctx, in.Reader, in.Principal, subject)
		if err != nil {
			return nil, nil, fmt.Errorf("establish: %w", err)
		}
		w, err := weight.Weight(ctx, node.ComponentID)
		if err != nil {
			return nil, nil, fmt.Errorf("establish: weight for %s: %w", node.ComponentID, err)
		}
		band, base, movement, err := ComputeBand(t, cons, reach.Count, w)
		if err != nil {
			drop(DroppedFinding, i, key, err.Error())
			continue
		}

		f := Finding{
			ID:                 uuid.NewString(),
			Key:                key,
			RunID:              in.RunID,
			ScopeKey:           in.ScopeKey,
			SessionID:          in.SessionID,
			Type:               t,
			Reference:          ReferenceFor(t),
			SubjectNodeID:      subject,
			SubjectComponentID: node.ComponentID,
			Claim:              claim,
			Condition:          condition,
			Rationale:          strings.TrimSpace(c.Rationale),
			Consequence:        cons,
			Band:               band,
			ReachCount:         reach.Count,
			Weight:             w,
			Derivation: Derivation{
				Consequence:  cons,
				EvidenceLine: evidence,
				Reach:        reach,
				Weight:       w,
				BaseBand:     base,
				Band:         band,
				Movement:     movement,
			},
			EvidenceSessionID: in.SessionID,
			EstablishedAt:     now,
			CreatedAt:         now,
		}

		if c.Proposal != nil {
			p, err := establishProposal(ctx, in, t, c.Proposal)
			if err != nil {
				drop(DroppedProposal, i, key, err.Error())
			} else {
				f.Proposal = p
			}
		}

		seen[key] = struct{}{}
		out = append(out, f)
	}
	return out, dropped, nil
}

// establishProposal resolves the proposal's target component from the graph
// and validates the proposal against the finding's type and the run's scope.
func establishProposal(ctx context.Context, in EstablishInput, t Type, cp *CandidateProposal) (*Proposal, error) {
	shape, ok := ParseProposalShape(cp.Shape)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrProposalShape, cp.Shape)
	}
	if t == TypeGraphUpdate && shape != ShapeOperatorAction {
		return nil, fmt.Errorf("proposal: a graph update's proposal is an operator action, got %s", shape)
	}
	p := &Proposal{
		Shape: shape,
		Target: ProposalTarget{
			NodeID:   strings.TrimSpace(cp.TargetNodeID),
			SystemID: strings.TrimSpace(cp.TargetSystemID),
		},
		Base:           strings.TrimSpace(cp.Base),
		Change:         cp.Change,
		ExpectedEffect: strings.TrimSpace(cp.ExpectedEffect),
		Limits:         strings.TrimSpace(cp.Limits),
		Paths:          cp.Paths,
		LiveObject:     cp.LiveObject,
	}
	if p.Target.NodeID != "" {
		node, err := in.Reader.GraphGetNode(ctx, in.Principal, p.Target.NodeID)
		if err != nil && !errors.Is(err, graph.ErrNodeNotFound) {
			return nil, fmt.Errorf("read target node %s: %w", p.Target.NodeID, err)
		}
		if node != nil {
			p.Target.ComponentID = node.ComponentID
		}
	}
	p.Normalise()
	if err := p.Validate(in.Scope); err != nil {
		return nil, err
	}
	return p, nil
}
