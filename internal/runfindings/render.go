package runfindings

import (
	"fmt"
	"sort"
	"strings"
)

// Coverage is which components the run's pass completed and which it did not.
// It is the run record's; the renderer takes it as an input of its own and
// never derives it from the findings.
type Coverage struct {
	Covered    []string
	NotCovered []string
}

// RenderInput is everything the renderer needs to assemble a run's findings
// section. The run record supplies coverage and the stop reason; the limits
// section those feed is the run side's, rendered beside this output.
type RenderInput struct {
	Findings []Finding
	Coverage Coverage
	// StopReason is the ceiling that stopped the run, or empty when the run
	// completed.
	StopReason string
	// Dropped are the candidates and proposals not emitted. They are counted,
	// not listed: the reasons are in the record of the run, and a dropped
	// candidate is not a finding.
	Dropped []Dropped
}

// Render assembles the findings half of a run's final turn from the records.
// Nothing in the rendered list exists only in prose: every line is a field of
// a Finding, so a reader of the turn and a reader of the records see the same
// thing. Graph updates are delivered apart, as a proposed-admissions list
// addressed to admins, and never ranked against issues and risks.
func Render(in RenderInput) string {
	var ranked, admissions []Finding
	for _, f := range in.Findings {
		if f.Type == TypeGraphUpdate {
			admissions = append(admissions, f)
		} else {
			ranked = append(ranked, f)
		}
	}
	Sort(ranked)
	Sort(admissions)

	var b strings.Builder
	b.WriteString("## Findings\n\n")
	b.WriteString(summaryLine(ranked))
	b.WriteString("\n\n")

	for _, band := range Bands {
		var inBand []Finding
		for _, f := range ranked {
			if f.Band == band {
				inBand = append(inBand, f)
			}
		}
		fmt.Fprintf(&b, "### %s\n\n", band)
		if len(inBand) == 0 {
			fmt.Fprintf(&b, "No %s findings.\n\n", band)
			continue
		}
		for _, f := range inBand {
			writeFinding(&b, f)
		}
	}

	if len(in.Coverage.NotCovered) > 0 || in.StopReason != "" {
		b.WriteString("### Not covered\n\n")
		if len(in.Coverage.NotCovered) > 0 {
			nc := append([]string{}, in.Coverage.NotCovered...)
			sort.Strings(nc)
			fmt.Fprintf(&b, "Components this run did not cover: %s. An absent finding on an uncovered component is not a clean bill.\n", strings.Join(nc, ", "))
		}
		if in.StopReason != "" {
			fmt.Fprintf(&b, "The run stopped before completing its pass: %s.\n", in.StopReason)
		}
		b.WriteString("\n")
	}
	if n := len(in.Dropped); n > 0 {
		findings, proposals := 0, 0
		for _, d := range in.Dropped {
			if d.Kind == DroppedProposal {
				proposals++
			} else {
				findings++
			}
		}
		fmt.Fprintf(&b, "Not emitted: %d candidate finding(s) and %d proposal(s) that did not meet the finding contract.\n\n", findings, proposals)
	}

	b.WriteString("## Proposed admissions\n\n")
	if len(admissions) == 0 {
		b.WriteString("None.\n")
	} else {
		b.WriteString("For admins: evidence of systems outside joe's remit, or relationships no refresher derives. Registering a component is an admission (D-0029); the run applies nothing.\n\n")
		for _, f := range admissions {
			writeFinding(&b, f)
		}
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

func summaryLine(ranked []Finding) string {
	counts := map[Label]int{}
	comps := map[string]struct{}{}
	for _, f := range ranked {
		counts[f.Label]++
		comps[f.SubjectComponentID] = struct{}{}
	}
	return fmt.Sprintf("%d finding(s) on %d component(s): %d new, %d standing, %d resolved, %d unknown.",
		len(ranked), len(comps), counts[LabelNew], counts[LabelStanding], counts[LabelResolved], counts[LabelUnknown])
}

func writeFinding(b *strings.Builder, f Finding) {
	label := string(f.Label)
	if label == "" {
		label = "unlabelled"
	}
	fmt.Fprintf(b, "- **[%s] %s** `%s` — %s\n", label, f.Type, f.SubjectNodeID, f.Claim)
	if f.Condition != "" {
		fmt.Fprintf(b, "  - condition: %s\n", f.Condition)
	}
	d := f.Derivation
	switch f.Type {
	case TypeGraphUpdate:
		fmt.Fprintf(b, "  - band: %s (graph update, always %s)\n", f.Band, BandP3)
	default:
		fmt.Fprintf(b, "  - band: %s (base %s from %s; reach %d dependent(s); weight %g; %s)\n",
			f.Band, d.BaseBand, d.Consequence, d.Reach.Count, d.Weight, d.Movement)
	}
	fmt.Fprintf(b, "  - evidence: %q (session %s", d.EvidenceLine, f.EvidenceSessionID)
	if f.EvidenceMessageID != "" {
		fmt.Fprintf(b, ", turn %s", f.EvidenceMessageID)
	}
	b.WriteString(")\n")
	if f.Rationale != "" {
		fmt.Fprintf(b, "  - rationale: %s\n", f.Rationale)
	}
	if p := f.Proposal; p != nil {
		exec := "not executable"
		if p.Executable {
			exec = "executable by shape"
		}
		fmt.Fprintf(b, "  - proposal (%s, %s, %s): target `%s` (%s); base %s\n",
			p.Status, p.Shape, exec, p.Target.NodeID, p.Target.SystemID, p.Base)
		fmt.Fprintf(b, "    - change: %s\n", indentBlock(p.Change))
		fmt.Fprintf(b, "    - expected effect: %s\n", p.ExpectedEffect)
		fmt.Fprintf(b, "    - limits: %s\n", p.Limits)
	}
}

// indentBlock keeps a multi-line change inside its list item.
func indentBlock(s string) string {
	s = strings.TrimRight(s, "\n")
	if !strings.Contains(s, "\n") {
		return s
	}
	return "\n      " + strings.ReplaceAll(s, "\n", "\n      ")
}
