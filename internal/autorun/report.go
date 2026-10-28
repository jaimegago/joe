package autorun

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jaimegago/joe/internal/agentloop"
)

// FindingsPipeline is THE seam between the run side and the report side of the
// read-only Auto Joe build (joe-pm threads/autojoe-ro-run-build.md and
// threads/autojoe-ro-report-build.md). The run owns the record, the session
// and the limits section; the report side owns the finding record, the band
// table, the proposal shapes and the renderer that assembles findings into a
// final turn. Wiring the two — the run invoking the finding pipeline and
// placing the rendered findings beside the limits section — is done by
// whichever of the two threads merges second, by implementing this interface
// and setting Config.Findings.
//
// Findings receives the finished run (its record is final: coverage, stop
// reason, spend) and the model's final prose, and returns the findings
// section of the final turn. It runs after the pass and after any
// reserve-funded report call, under the run's agent:core context. An error
// does not lose the run: the limits section is still written and the error is
// recorded on the turn, because a run that stops early must say so in the same
// place its findings would be.
type FindingsPipeline interface {
	Findings(ctx context.Context, run *Run, answer string) (string, error)
}

// unverifiedSection is what stands where the findings will go until the
// pipeline is wired: the model's prose, labelled as D-0140 requires of
// session output nobody verified.
func unverifiedSection(answer string) string {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return "## Pass\n\nThe pass produced no report."
	}
	return "## Pass (unverified)\n\n" + answer
}

// LimitsSection renders the limits section of a run's final turn from the run
// record — scope covered, completed or stopped on budget, and what was not
// looked at — as D-0140 requires of an assessment and § Delivery of the build
// order requires of a run. It is code over the record, never prose from the
// model, so what the session says about coverage is what the record says.
func LimitsSection(run *Run) string {
	var b strings.Builder
	b.WriteString("## Limits\n\n")

	fmt.Fprintf(&b, "- Scope: %d component(s) resolved at start from the selection", len(run.ResolvedComponents))
	if n := run.Selection.Normalize(); len(n.Components) > 0 || len(n.Zones) > 0 {
		b.WriteString(" (")
		parts := []string{}
		if len(n.Components) > 0 {
			parts = append(parts, "components: "+strings.Join(n.Components, ", "))
		}
		if len(n.Zones) > 0 {
			parts = append(parts, "zones: "+strings.Join(n.Zones, ", "))
		}
		b.WriteString(strings.Join(parts, "; "))
		b.WriteString(")")
	}
	b.WriteString(".\n")

	b.WriteString("- Outcome: ")
	b.WriteString(outcomeLine(run))
	b.WriteString("\n")

	if len(run.Covered) > 0 {
		fmt.Fprintf(&b, "- Covered (%d): %s\n", len(run.Covered), strings.Join(run.Covered, ", "))
	} else {
		b.WriteString("- Covered: none.\n")
	}
	if len(run.NotCovered) > 0 {
		fmt.Fprintf(&b, "- Not looked at (%d): %s. An absent finding on a component that was not looked at is not a clean bill.\n",
			len(run.NotCovered), strings.Join(run.NotCovered, ", "))
	} else {
		b.WriteString("- Not looked at: none.\n")
	}

	fmt.Fprintf(&b, "- Budget: %d of %d tokens used by the pass; wall clock %s of %s",
		run.TokensUsed, run.TokenCeiling, durationOf(run), (time.Duration(run.WallClockSeconds) * time.Second).String())
	if run.ReportTokens > 0 {
		fmt.Fprintf(&b, "; the report spent %d tokens from a reserve of %d held outside the ceiling", run.ReportTokens, run.ReportReserveTokens)
	}
	b.WriteString(".\n")

	fmt.Fprintf(&b, "- Run %s, activated by %s, executed as %s.", run.ID, run.Activator, run.Principal)
	return b.String()
}

func outcomeLine(run *Run) string {
	switch run.StopReason {
	case "":
		if run.State == StateFailed {
			return "failed before the pass could report: " + run.Error + "."
		}
		return "completed: the pass took every component in the scope."
	case agentloop.StopReasonTokenCeiling:
		return fmt.Sprintf("stopped on the token ceiling (%d tokens) with %d of %d component(s) covered; the report is partial.",
			run.TokenCeiling, len(run.Covered), len(run.ResolvedComponents))
	case agentloop.StopReasonWallClock:
		return fmt.Sprintf("stopped on the wall-clock ceiling (%s) with %d of %d component(s) covered; the report is partial.",
			(time.Duration(run.WallClockSeconds) * time.Second).String(), len(run.Covered), len(run.ResolvedComponents))
	case agentloop.StopReasonMaxIterations:
		return fmt.Sprintf("stopped on the loop's step cap with %d of %d component(s) covered; the report is partial.",
			len(run.Covered), len(run.ResolvedComponents))
	case agentloop.StopReasonCancelled:
		return "cancelled: the service stopped while the run was live; nothing was reported."
	case StopReasonInterrupted:
		return "interrupted: the process ended while the run was live; nothing was reported."
	case StopReasonError:
		return "failed: " + run.Error + "."
	}
	return "stopped: " + run.StopReason + "."
}

func durationOf(run *Run) string {
	if run.EndedAt == nil || run.StartedAt.IsZero() {
		return "n/a"
	}
	return run.EndedAt.Sub(run.StartedAt).Round(time.Second).String()
}

// FinalTurn assembles the run's final turn: the findings section (from the
// pipeline, or the unverified prose until one is wired) above the limits
// section. The order is deliberate — a reader meets the limits in the same
// place as the findings, below them, every time.
func FinalTurn(findings string, run *Run) string {
	return strings.TrimSpace(findings) + "\n\n" + LimitsSection(run)
}
