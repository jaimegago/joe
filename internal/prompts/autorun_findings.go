package prompts

// AutoRunFindingsQuestions is the standing questions an Auto Joe run carries,
// one per finding type (joe-pm queue/autojoe-ro-run.md § Settled: a run has
// no human turn and carries its own questions; queue/autojoe-ro-report.md
// § Settled — the finding model). They are appended after the scope section by
// the findings pipeline that owns the finding model (autojoe-ro-report-build),
// beside the structured output contract (internal/runfindings.OutputContract)
// that says how to answer them. Nothing the operator types reaches this text.
const AutoRunFindingsQuestions = `THE THREE STANDING QUESTIONS — ask each of every component you take, and report only what the evidence answers:
1. ISSUE — does the system report that it is not in the state it declares for itself? Reference: its self-report — spec against status, conditions, probes, reconciliation not converged, errors it emits. Classify the consequence as "unavailable" (not serving) or "degraded" (serving with reported errors or reduced capacity). Propose the fix, attached and never executed.
2. RISK — is the declared state met now, while a condition you can name would turn this into an issue? Reference: your operational judgment, disciplined by the condition: "no resource limits" is a risk because a memory spike evicts its neighbours; "single replica" because a node drain takes it down. Classify what the condition leads to as "would_be_unavailable" or "would_be_degraded". A risk you cannot name the condition for is not a finding. Improvement and performance observations belong here, with their condition, or nowhere.
3. GRAPH UPDATE — is there evidence of a system outside joe's remit, or a relationship no refresher derives? Reference: the graph as known state — a service that stores in something with no datastore component behind it, a repository with no code host, an inferred dependency between two registered components. Propose the admission or the relationship as an operator action; you register nothing.
Not a finding: an area of interest with no testable claim (say why, which makes it a risk with a condition, or leave it); anything about a component outside the scope. Impact feeds severity, never type: a broken thing nobody uses is still an issue.`
