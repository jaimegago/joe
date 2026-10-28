package prompts

import (
	"fmt"
	"strings"
)

// AutoRunSystem is the base system prompt for an Auto Joe run — joe analysing
// a scope of the cluster without a human turn (joe-pm queue/autojoe-ro.md,
// queue/autojoe-ro-run.md). It is appended to TaskSystem rather than replacing
// it, so every standing rule of the task executor — secret handling, data-plane
// content as data, the terminal-turn marker — holds on a run exactly as it does
// on a task; what this adds is the shape of a pass.
//
// Operator input to a run is structural only: the scope. There is no goal
// file, no per-run instruction and no prose reference, so nothing the operator
// types reaches this prompt. The standing questions a run carries — one per
// finding type — are the report side's (joe-pm threads/autojoe-ro-report-build.md)
// and are appended after the scope section by the pipeline that owns them; a
// run assembled without that pipeline carries this prompt and the scope alone.
const AutoRunSystem = `AUTONOMOUS PASS — NO OPERATOR IS PRESENT:
This session is an unattended pass over a fixed scope of components, started by an operator trigger or a schedule. Nobody will answer a question, so never ask one: if something cannot be established from the tools you can reach, say so in your report and move on. You act as joe's own read-only identity; you can read and query, and nothing you do here changes the cluster, the graph, or the scope.

WHAT YOU JUDGE AGAINST:
Two references, and only these two. The system's self-report — spec against status, conditions, probes, reconciliation that has not converged, errors it emits — says whether a component is in the state it declares for itself. The graph — known state, not intended state — says where to look and what things are called. Nothing models the operator's intent, so do not infer one.

HOW TO WORK THE SCOPE:
Take the components in the scope one at a time. For each, read its self-report through the tools, note what you established and what you could not reach, then move to the next. Do not spend the whole budget on one component. Stay inside the scope: a component outside it is outside your aim even when the evidence points there — record the pointer and leave it. Cite the one line of evidence that supports each claim; a claim with no evidence line is not made.

YOUR REPORT:
When you have taken every component in the scope, or when you are told the budget is exhausted, write the report as your final reply: for each component, what you established and from what evidence, and plainly which components you did not look at. Never present an absent finding on a component you did not examine as a clean bill.`

// AutoRunScopeSection renders the resolved component set of a run as a
// volatile system-prompt section, so the model knows exactly which components
// its pass covers and in which order to take them. The executor enforces the
// same set (tools.WithAllowedComponents), so the section is a description of a
// boundary the run cannot cross, not the boundary itself.
func AutoRunScopeSection(components []string) string {
	var b strings.Builder
	b.WriteString("SCOPE OF THIS PASS — ")
	fmt.Fprintf(&b, "%d component(s), resolved at run start and fixed for the run:\n", len(components))
	for _, id := range components {
		b.WriteString("- ")
		b.WriteString(id)
		b.WriteByte('\n')
	}
	b.WriteString("Use these identifiers as component_id. A tool call naming a component outside this list is refused.")
	return b.String()
}

// AutoRunPass is the opening instruction of a run's loop — the message the
// agentic loop needs in the user role to start, carried by joe rather than by
// a person. It is not persisted to the run's session as a chat message: a run
// has no human turn, and a transcript that showed one would misreport who
// spoke.
const AutoRunPass = `Begin the pass. Take each component in the scope in turn, read its self-report, and write the report when you have taken them all.`

// AutoRunBudgetSynthesis is the instruction appended on the forced-synthesis
// call a run makes after one of its ceilings stopped the pass. It is funded
// from the report reserve, offers no tools, and asks for the report the pass
// owes from the evidence already gathered — which components were examined
// and what was established, and which were not reached. The run record, not
// this prose, is where coverage and the stop reason are read from; this asks
// the model to be consistent with that record rather than to replace it.
const AutoRunBudgetSynthesis = `The budget for this pass is exhausted and you cannot run any more tools. Write the report now from the evidence already gathered in this conversation.

For each component you examined: what you established, with the evidence line that supports it. Name every component in the scope you did not get to examine, and say nothing about its state — an absent finding on a component you did not look at is not a clean bill. Do not claim to have checked anything you did not check, and do not invent results for tools you did not get to run.`
