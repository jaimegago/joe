// Package runfindings is the typed finding record a read-only Auto Joe run
// produces (docs/backlog/autojoe-ro.md § Incrementality and the finding
// record, § The finding model, § Ranking, § Delivery, § Proposed solutions).
//
// It is deliberately NOT the findings package: `findings` is a human
// cross-session annotation — a person posts a body into a session's timeline —
// and `joe_warnings` has no production writer. Both match this record in name
// and not in shape, and reusing either would make "who wrote this" ambiguous
// on a record whose author is a principal (svc:agent:core). A run finding is a
// different thing with a different owner, so it has a table and a package of
// its own.
//
// What the package owns: the record and its stable key, the band table and the
// code that computes a band, reach derived from the graph, the three proposal
// shapes with their invariants, the structured output schema a run's model
// emits, the labeller that compares a run against the previous run on the same
// scope, and the renderer that assembles a session's final turn from records.
//
// What it does not own: the run record (activator, resolved component set,
// ceilings, coverage, stop reason) and the run's session. Those are inputs
// here, never derived here.
//
// A finding narrows nothing and permits nothing (D-0167, generalised): no
// authorization or enforcement path reads this record, and nothing in this
// package is reachable from one. It is admissible as persisted LLM judgment in
// joe's own store on D-0163's terms — something reconciles it — with the next
// run as the reconciler: a finding not re-established by a run whose scope
// covered its subject is resolved.
package runfindings

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"time"
)

// Type is what a finding claims about the present. The vocabulary is closed at
// exactly three values; improvement, performance, area of interest, discovery,
// drift and unregistered were each considered and rejected in the design.
type Type string

const (
	// TypeIssue — the system reports that it is not in the state it declares
	// for itself. Reference: the self-report.
	TypeIssue Type = "issue"
	// TypeRisk — the intended state is met now, but a named condition would
	// turn this into an issue. Reference: operational judgment, disciplined by
	// the condition. A risk that cannot name its condition is not emitted.
	TypeRisk Type = "risk"
	// TypeGraphUpdate — there is evidence of a system outside joe's remit, or a
	// relationship no refresher derives. Reference: the graph as known state.
	// Proposal-only: the run applies nothing.
	TypeGraphUpdate Type = "graph_update"
)

// ParseType maps a declared value to the closed vocabulary. The second return
// is false for anything outside it.
func ParseType(s string) (Type, bool) {
	switch Type(strings.ToLower(strings.TrimSpace(s))) {
	case TypeIssue:
		return TypeIssue, true
	case TypeRisk:
		return TypeRisk, true
	case TypeGraphUpdate:
		return TypeGraphUpdate, true
	}
	return "", false
}

// Reference is what a finding was judged against. It is a function of the type
// and is recorded on the record so a reader does not have to know the mapping.
type Reference string

const (
	ReferenceSelfReport          Reference = "self_report"
	ReferenceOperationalJudgment Reference = "operational_judgment"
	ReferenceGraph               Reference = "graph"
)

// ReferenceFor returns the reference a type is judged against.
func ReferenceFor(t Type) Reference {
	switch t {
	case TypeIssue:
		return ReferenceSelfReport
	case TypeRisk:
		return ReferenceOperationalJudgment
	default:
		return ReferenceGraph
	}
}

// Consequence is what the finding claims, classified by the model into a closed
// vocabulary. It is the one place the self-report's own urgency enters ranking.
// A graph update has no consequence (ConsequenceNone).
type Consequence string

const (
	ConsequenceNone               Consequence = ""
	ConsequenceUnavailable        Consequence = "unavailable"
	ConsequenceDegraded           Consequence = "degraded"
	ConsequenceWouldBeUnavailable Consequence = "would_be_unavailable"
	ConsequenceWouldBeDegraded    Consequence = "would_be_degraded"
)

// ParseConsequence maps a declared value to the closed vocabulary. An empty
// value parses to ConsequenceNone with ok=true; the caller decides whether that
// is admissible for the type (AdmissibleFor).
func ParseConsequence(s string) (Consequence, bool) {
	v := strings.ToLower(strings.TrimSpace(s))
	v = strings.ReplaceAll(v, " ", "_")
	v = strings.ReplaceAll(v, "-", "_")
	switch Consequence(v) {
	case ConsequenceNone, ConsequenceUnavailable, ConsequenceDegraded,
		ConsequenceWouldBeUnavailable, ConsequenceWouldBeDegraded:
		return Consequence(v), true
	}
	return "", false
}

// AdmissibleFor reports whether a consequence may be carried by a finding of
// type t: an issue carries unavailable or degraded, a risk carries one of the
// would-be forms, a graph update carries none.
func (c Consequence) AdmissibleFor(t Type) bool {
	switch t {
	case TypeIssue:
		return c == ConsequenceUnavailable || c == ConsequenceDegraded
	case TypeRisk:
		return c == ConsequenceWouldBeUnavailable || c == ConsequenceWouldBeDegraded
	case TypeGraphUpdate:
		return c == ConsequenceNone
	}
	return false
}

// Band is severity on one fixed scale, P0 to P3, the same in every
// installation. The scale is joe's; the operator moves a finding within it by
// weight and never redefines it.
type Band string

const (
	BandP0 Band = "P0"
	BandP1 Band = "P1"
	BandP2 Band = "P2"
	BandP3 Band = "P3"
)

// Bands lists the scale from most to least severe.
var Bands = []Band{BandP0, BandP1, BandP2, BandP3}

// Rank is the band's position on the scale: P0 is 0, P3 is 3. An unknown band
// ranks after P3 so it never sorts ahead of a real one.
func (b Band) Rank() int {
	for i, x := range Bands {
		if x == b {
			return i
		}
	}
	return len(Bands)
}

func bandFromRank(r int) Band {
	if r < 0 {
		r = 0
	}
	if r >= len(Bands) {
		r = len(Bands) - 1
	}
	return Bands[r]
}

// Label is a finding's standing against the previous completed run on the same
// scope. Exactly four values. The previous run labels; it never judges.
type Label string

const (
	// LabelNew — established by this run and not by the previous one.
	LabelNew Label = "new"
	// LabelStanding — established by this run and by the previous one.
	LabelStanding Label = "standing"
	// LabelResolved — established by the previous run, not re-established by
	// this run, and this run covered its component.
	LabelResolved Label = "resolved"
	// LabelUnknown — established by the previous run, not re-established by
	// this run, and this run did NOT cover its component. An absent finding on
	// an uncovered component is never a clean bill.
	LabelUnknown Label = "unknown"
)

// Reach is the set of graph nodes that depend on a finding's subject, read from
// the graph's dependency edges walked in the direction of dependence. Derived
// by code, never asserted by the model (reach.go).
type Reach struct {
	// Dependents are the node ids that depend on the subject, transitively,
	// sorted. Capped at MaxDerivationDependents on the persisted derivation;
	// Count is exact regardless.
	Dependents []string `json:"dependents"`
	// Components are the distinct, non-empty component ids the dependents
	// belong to, sorted.
	Components []string `json:"components"`
	// Count is the number of dependents, before any cap.
	Count int `json:"count"`
}

// MaxDerivationDependents caps the dependent ids written into a derivation. A
// finding on a cluster-wide subject can have thousands; the count is what the
// band keys on and the ids are evidence, so the ids are capped and the count is
// not.
const MaxDerivationDependents = 50

// Derivation is how a finding's band was reached. A finding carries its
// derivation or is not emitted: the consequence class with the one self-report
// line that supports it, the dependents counted, the weight applied, and the
// band movement.
type Derivation struct {
	Consequence  Consequence `json:"consequence"`
	EvidenceLine string      `json:"evidence_line"`
	Reach        Reach       `json:"reach"`
	Weight       float64     `json:"weight"`
	BaseBand     Band        `json:"base_band"`
	Band         Band        `json:"band"`
	// Movement is "raised", "lowered" or "none" — how reach and weight moved
	// the finding from its base band within its type's bounds.
	Movement string `json:"movement"`
}

// Finding is one row of run_findings: a typed, persisted record with a stable
// key, attached to the run that produced it.
type Finding struct {
	// ID is the row identity, unique per (run, key). It is NOT the finding's
	// identity across runs; Key is.
	ID string `json:"id"`
	// Key is the finding's identity across runs, derived from type, subject
	// node and claim and never from the run (ComputeKey). The same finding
	// established by two consecutive runs on one scope has one Key.
	Key string `json:"key"`
	// RunID is the run that produced (or carried forward, for resolved and
	// unknown) this row. The run record is the run side's; this is a pointer
	// to it, and run_finding_sets is the report side's own index of it.
	RunID string `json:"run_id"`
	// ScopeKey identifies the resolved component set the run was over
	// (ScopeKey()). "The previous completed run on the same scope" means the
	// same ScopeKey.
	ScopeKey string `json:"scope_key"`
	// SessionID is the run's session — where this run's final turn is.
	SessionID string `json:"session_id"`

	Type      Type      `json:"type"`
	Reference Reference `json:"reference"`
	// SubjectNodeID is the graph node the finding is about.
	SubjectNodeID string `json:"subject_node_id"`
	// SubjectComponentID is the component that node belongs to, resolved from
	// the graph by code. Coverage and scope are decided on it.
	SubjectComponentID string `json:"subject_component_id"`
	// Claim is the reference claim, in the model's words.
	Claim string `json:"claim"`
	// Condition is the named condition that would turn a risk into an issue.
	// Required for TypeRisk; empty otherwise.
	Condition string `json:"condition,omitempty"`
	// Rationale is the model's prose beside the claim — alternatives, caveats.
	// It is not a proposal and not a derivation.
	Rationale string `json:"rationale,omitempty"`

	Consequence Consequence `json:"consequence,omitempty"`
	Band        Band        `json:"band"`
	// ReachCount and Weight are the two ranking inputs, duplicated out of the
	// derivation so within-band order (reach times weight) is a column sort.
	ReachCount int        `json:"reach_count"`
	Weight     float64    `json:"weight"`
	Derivation Derivation `json:"derivation"`

	// Proposal is the one change the run recommends, or nil. Zero or one per
	// finding; every one unverified (D-0140).
	Proposal *Proposal `json:"proposal,omitempty"`

	Label Label `json:"label"`

	// EvidenceSessionID and EvidenceMessageID point at the turn in a run's
	// session where the finding was established. For a new or standing finding
	// that is this run's session; for a resolved or unknown finding it is the
	// previous run's, carried forward. EvidenceMessageID is empty until the
	// turn is persisted.
	EvidenceSessionID string `json:"evidence_session_id"`
	EvidenceMessageID string `json:"evidence_message_id,omitempty"`
	// EstablishedAt is when the evidence was gathered — the in-band tiebreak.
	EstablishedAt time.Time `json:"established_at"`
	CreatedAt     time.Time `json:"created_at"`
}

// FindingSet is one row of run_finding_sets: the report side's own record that
// a run persisted its findings, written even when there are none, so "the
// latest run on a scope" is exact and a clean run reads as clean rather than
// as the previous run's list.
type FindingSet struct {
	RunID        string    `json:"run_id"`
	ScopeKey     string    `json:"scope_key"`
	SessionID    string    `json:"session_id"`
	CreatedAt    time.Time `json:"created_at"`
	FindingCount int       `json:"finding_count"`
}

// ComputeKey derives a finding's identity from its type, subject node and
// claim. The claim is normalised — trimmed, lower-cased, whitespace collapsed —
// so the same claim phrased with different spacing has one identity. Nothing
// about the run enters.
func ComputeKey(t Type, subjectNodeID, claim string) string {
	h := sha256.New()
	h.Write([]byte(string(t)))
	h.Write([]byte{0})
	h.Write([]byte(strings.TrimSpace(subjectNodeID)))
	h.Write([]byte{0})
	h.Write([]byte(normaliseClaim(claim)))
	return hex.EncodeToString(h.Sum(nil))
}

func normaliseClaim(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// ScopeKey identifies a resolved component set: the sorted, de-duplicated
// component ids hashed. Two runs over the same selection at different times
// compare on the resolved set each recorded, not on the selection.
func ScopeKey(componentIDs []string) string {
	seen := make(map[string]struct{}, len(componentIDs))
	ids := make([]string, 0, len(componentIDs))
	for _, id := range componentIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	h := sha256.New()
	for _, id := range ids {
		h.Write([]byte(id))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Set is a membership predicate over component ids — the run's resolved
// component set, or the components a run covered. A nil Set admits nothing.
type Set func(componentID string) bool

// SetOf builds a Set from a list of component ids.
func SetOf(ids ...string) Set {
	m := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		m[id] = struct{}{}
	}
	return func(id string) bool {
		_, ok := m[id]
		return ok
	}
}

// Sort orders findings for delivery: by band (P0 first), then by reach times
// weight descending, then by how recent the evidence is, then by key so the
// order is total and the same evidence renders the same way on every run.
func Sort(fs []Finding) {
	sort.SliceStable(fs, func(i, j int) bool {
		a, b := fs[i], fs[j]
		if a.Band.Rank() != b.Band.Rank() {
			return a.Band.Rank() < b.Band.Rank()
		}
		ai, bi := float64(a.ReachCount)*a.Weight, float64(b.ReachCount)*b.Weight
		if ai != bi {
			return ai > bi
		}
		if !a.EstablishedAt.Equal(b.EstablishedAt) {
			return a.EstablishedAt.After(b.EstablishedAt)
		}
		return a.Key < b.Key
	})
}
