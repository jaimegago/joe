package runfindings

import (
	"encoding/json"
	"fmt"
	"strings"
)

// The structured output schema.
//
// D-0140 deferred a typed output contract until the first parsing consumer.
// A run's findings are assembled into a session's final turn from structured
// records, which is parsing in that sense, so this package owns the schema and
// docs/backlog/pr-surface-joe-mention.md adopts it rather than minting a
// sibling (docs/backlog/autojoe-ro.md § Schema ownership).
//
// The shape: the model emits ONE fenced block tagged FindingsFence, carrying a
// JSON object with a "findings" array. Each element is a Candidate — the
// model's classification and nothing more. The model classifies the type, the
// subject node, the claim, the consequence, the condition for a risk, the one
// evidence line, and optionally a proposal. It never emits a band, a reach, a
// weight, a key or a label: those are computed by code (Establish, LabelAgainstPrevious), and
// a field for any of them is absent from the schema so there is nothing to be
// tempted by.
//
// The block is looked for anywhere in the turn and the LAST one wins, for the
// reason SplitTurnKind gives: models append trailing prose after a block they
// were told to end with.

// FindingsFence is the info string on the fenced block the model emits.
const FindingsFence = "joe-findings"

// Candidate is one finding as the model declared it, before code establishes
// it. Fields the model does not control are absent by design.
type Candidate struct {
	Type          string             `json:"type"`
	SubjectNodeID string             `json:"subject_node_id"`
	Claim         string             `json:"claim"`
	Consequence   string             `json:"consequence,omitempty"`
	Condition     string             `json:"condition,omitempty"`
	EvidenceLine  string             `json:"evidence_line"`
	Rationale     string             `json:"rationale,omitempty"`
	Proposal      *CandidateProposal `json:"proposal,omitempty"`
}

// CandidateProposal is a proposal as the model declared it. The target's
// component is resolved by code from the node, so it is not a field here.
type CandidateProposal struct {
	Shape          string         `json:"shape"`
	TargetNodeID   string         `json:"target_node_id"`
	TargetSystemID string         `json:"target_system_id"`
	Base           string         `json:"base"`
	Change         string         `json:"change"`
	ExpectedEffect string         `json:"expected_effect"`
	Limits         string         `json:"limits"`
	Paths          []string       `json:"paths,omitempty"`
	LiveObject     *LiveObjectRef `json:"live_object,omitempty"`
}

// candidatesEnvelope is the fenced block's JSON object.
type candidatesEnvelope struct {
	Findings []Candidate `json:"findings"`
}

// OutputContract is the instruction a run's prompt carries so the model emits
// the block this package parses. It is exported so the run side's prompt
// includes it verbatim rather than restating the schema; the prompt is the run
// side's, the schema is this package's.
const OutputContract = "FINDINGS OUTPUT: end your final turn with exactly one fenced code block whose " +
	"info string is `" + FindingsFence + "`, containing a JSON object " +
	`{"findings": [...]}. Each element has: "type" (one of "issue", "risk", "graph_update"); ` +
	`"subject_node_id" (the graph node id the finding is about, exactly as the graph names it); ` +
	`"claim" (one sentence stating what the reference shows); ` +
	`"consequence" ("unavailable" or "degraded" for an issue; "would_be_unavailable" or "would_be_degraded" for a risk; omit for a graph_update); ` +
	`"condition" (for a risk only: the named condition that would turn it into an issue — a risk without one is not emitted); ` +
	`"evidence_line" (the one line of the self-report or the graph that supports the consequence — a finding without one is not emitted); ` +
	`"rationale" (optional prose, alternatives and caveats); ` +
	`and optionally "proposal": {"shape": "repository_change" | "live_object_change" | "operator_action", ` +
	`"target_node_id", "target_system_id", "base" (the observed state you computed the change from: a commit, a generation, or the observed state in words — a proposal with no base is not emitted), ` +
	`"change" (a unified diff, a patch, or steps), "expected_effect" (how the reference would show the finding resolved), ` +
	`"limits" (what you did not check), "paths" (file paths, for a repository_change), ` +
	`"live_object" ({"group","version","resource","namespace","name"}, for a live_object_change)}. ` +
	`Zero or one proposal per finding; every graph_update proposal is an operator_action to register a component or accept a relationship. ` +
	`Never put a secret value in a proposal; name a secret by reference. ` +
	`Do not emit a severity, band, priority, reach, weight or label: those are computed from your classification and the graph. ` +
	`A finding whose subject is outside the run's scope is not emitted.`

// SplitFindingsBlock separates the fenced findings block from the prose it was
// emitted alongside. It returns the block's body, the content with the block
// removed, and whether a block was found. The last block tagged FindingsFence
// wins; a block tagged anything else is left in the prose.
func SplitFindingsBlock(content string) (block, rest string, found bool) {
	lines := strings.Split(content, "\n")
	start, end := -1, -1
	for i := 0; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(t, "```") {
			continue
		}
		info := strings.TrimSpace(strings.TrimPrefix(t, "```"))
		if info != FindingsFence {
			// Skip to this (unrelated) block's close so a findings fence inside
			// it is not mistaken for ours, and so our close is not mistaken for
			// a nested block's.
			if info != "" {
				for i++; i < len(lines); i++ {
					if strings.TrimSpace(lines[i]) == "```" {
						break
					}
				}
			}
			continue
		}
		s := i
		e := -1
		for j := i + 1; j < len(lines); j++ {
			if strings.TrimSpace(lines[j]) == "```" {
				e = j
				break
			}
		}
		if e < 0 {
			break
		}
		start, end = s, e
		i = e
	}
	if start < 0 {
		return "", content, false
	}
	block = strings.Join(lines[start+1:end], "\n")
	rest = strings.Join(append(append([]string{}, lines[:start]...), lines[end+1:]...), "\n")
	return block, strings.TrimSpace(rest), true
}

// ParseCandidates decodes the fenced block's body. An unparseable block is an
// error, not an empty list: a run that emitted a block the parser cannot read
// did not deliver zero findings, and the two must not look alike.
func ParseCandidates(block string) ([]Candidate, error) {
	var env candidatesEnvelope
	dec := json.NewDecoder(strings.NewReader(block))
	if err := dec.Decode(&env); err != nil {
		return nil, fmt.Errorf("parse findings block: %w", err)
	}
	if env.Findings == nil {
		return nil, fmt.Errorf("parse findings block: no \"findings\" array")
	}
	return env.Findings, nil
}
