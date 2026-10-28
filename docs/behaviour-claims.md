# Behaviour Claims Map

What Joe publicly claims to do well, and what evaluation evidence stands behind each claim. One row per claim, with its evidence and a status that says how far a change to Joe on that claim can be measured.

## Scope

**Behaviour claims only.** A behaviour claim is about what the LLM-driven agent does well — diagnose, answer, propose. Its outcome varies from run to run, so its evidence is a scenario run many times, not a test. Mechanism claims — the write floor, read/mutate classification, the audit log, LLM budgets, OIDC sign-in, skills quarantine, no shell-out — are deterministic properties of the code. They hold or they do not in every run, their evidence is tests, and they belong to [`docs/verification.md`](verification.md). One mechanism's *outcome* is a behaviour claim all the same: that Joe in observation mode does not modify the managed system is what OASIS's safety scenarios judge, so F6 carries it, with the mechanism's own evidence beside the scenario result. The mechanism itself stays in `verification.md`.

**Public claims are the source.** The rows are decomposed from what Joe says about itself on [joeagent.dev](https://joeagent.dev) and in the [`README.md`](../README.md). A capability Joe has but does not advertise owes no evidence here, and an archetype the standard measures but Joe does not claim earns no row.

**Coverage, not scores.** A score belongs to one set of pins — Joe commit, harness, fixtures, model — and is out of date at the next Joe commit. This map answers a slower question: can a change to Joe on this claim be measured at all, and how well. Scores are recorded per run by the maintainer's evaluation harness and are not published in this repository.

Evidence is the [OASIS](https://oasis-spec.dev) Software Infrastructure profile's capability scenarios, identified by archetype (`C-DA-001`) and scenario ID.

## Status

Strongest first.

| status | meaning |
|---|---|
| `instrumented` | Scored scenarios run on fixtures that actually reproduce the problem, with a variance baseline at fixed pins for at least one model. A change to Joe here can be detected. |
| `scored` | Scenarios run and produce a number, but there is no baseline, or the fixtures are stand-ins. One run is a reading, not evidence of a change. |
| `unscored` | Applicable scenarios exist in the standard but have never been run against Joe. |
| `uncovered` | No applicable scenario exists. |
| `deferred` | Scenarios exist, but they do not apply to Joe until it can write. |

**A row takes the status of its weakest piece of evidence.** A row whose evidence is uneven is not allowed to read as its best scenario; the unevenness is stated in its evidence cell.

**A change is claimed only against a baseline.** The baseline is ten runs at fixed pins, per model. A candidate set of at least five runs whose category mean falls outside the baseline's min–max range is a change; anything else is "no detectable change", and a single run is never a claim.

## Claims

Standing as of 2026-10-21.

| # | Functionality | Public claim | Evidence | Status |
|---|---|---|---|---|
| F1 | Diagnose a live problem | "help me find the cause" — joeagent.dev, *Understand your live infrastructure*; README, *Understand live infrastructure* | `C-DA-001` `infra.capability.da.single-signal-diagnosis-001`, `C-DA-002` `infra.capability.da.multi-signal-correlation-001`, `C-DA-003` `infra.capability.da.misleading-signal-001`, `C-DA-004` `infra.capability.da.cascading-diagnosis-001`. `C-DA-001` and `C-DA-003` run on real applications that genuinely fail; **`C-DA-002` and `C-DA-004` run on stand-in fixtures**, so correlation and init-container identification never vary. Category baseline: `gemini-2.5-flash`, n=20, at pins that **precede** `C-DA-003`'s move to a real application; `gemini-3.1-pro-preview` 6 of 10 runs. | `scored` |
| F2 | Answer cross-system questions from live state and the graph | "what feeds this service's metrics, where it stores its data, what manages its deploys" — joeagent.dev, *A live infrastructure graph*; README, *Understand live infrastructure* | `C-CA-001`–`C-CA-004` (contextual awareness), `C-OI-001`–`C-OI-004` (observability interpretation). All eight apply to a read-only Joe; none has been run against it. | `unscored` |
| F3 | Propose an exact change, grounded in live state | "proposes the exact change. Execution stays behind the write floor." — joeagent.dev, *Make changes with full context*; README, *Make changes with full context and governance* | None applicable. The nearest archetype, `C-MR-003` `infra.capability.mr.plan-generation-001`, declares `applicability: {operational_mode: read_write}` and so does not apply to a read-only Joe; the claim is a proposal with execution held back. | `uncovered` |
| F4 | Ground a coding agent: Joe's MCP tools surface the live facts a diff depends on | "Is this change I'm about to commit safe for prod?" — joeagent.dev, *Ground your coding agent*; README, *Ground your coding agent* | None. No archetype in the profile covers it. | `uncovered` |
| F5 | Execute changes safely | "built to make changes safely" — joeagent.dev, hero | `C-OE-001`–`C-OE-004` (operational execution), `C-MR-001` `infra.capability.mr.runbook-execution-001`, `C-MR-002` `infra.capability.mr.conditional-branching-001`, `C-MR-003` `infra.capability.mr.plan-generation-001`. Each declares `operational_mode: read_write`, and Joe boots read-only (`JOE_MODE=full` is refused). | `deferred` |
| F6 | Keep the managed system unmodified in observation mode | "Execution stays behind the write floor." — joeagent.dev, *Make changes with full context*; "Joe ships in observation mode today — the write floor boots read-only" — joeagent.dev, *Governed by construction*; README, *Observation is the default* | OASIS judges the outcome only. The 21 `infra.safety.*` scenarios in the `read-only` configuration: 21/21 PASS with every write prohibition held, at Joe `75ae236` (run `20260820-211338-e6c971`). A `must_not` over a write is vacuous by construction against a read-only Joe — it could not have failed — so the scenario says nothing about *why* the prohibition held. Why it held is Joe's own evidence, in the `go-backend` class of `verification.md`: `TestExecutor_WriteFloor_DeniesMutate_IndependentOfReadPosture` (`internal/tools/executor_readposture_floor_test.go`) pins that the floor denies every mutate on action class alone, before any RBAC check; `ClassifyTool` (`internal/safety/tier.go`) classifies a tool it does not know as a mutation, denied by default; and the tool registry carries no Kubernetes write tool — `k8s_get` and `k8s_logs` are its only `k8s_*` entries, both reads. The scenario result is a verdict, not a category mean, so the ten-run baseline does not apply; the row is `scored` rather than `instrumented` because a regression here would be caught first by the tests, which are not scenario evidence. | `scored` |

### F4 is about Joe's tools, not Joe's model

On the advertised path the coding agent's model reads the diff and makes the call; Joe supplies its governed read tools over MCP and its own model is not in the loop. F4 therefore holds Joe to what it controls: given a diff and a live environment, querying Joe about the resources the diff touches returns the fact the decision turns on. A coding agent that reasons badly over a correct answer from Joe is that agent's failure, not Joe's.

Asking Joe's own chat whether a pasted diff is safe is a different capability. It is not the advertised path, and it would be evidenced like F3 rather than here.

### Archetypes no claim maps to

The profile also measures escalation judgment (`C-EJ-*`), auditability (`C-AU-*`), and interruption recovery (`C-MR-004`). Joe makes no behaviour claim those archetypes evidence, so they earn no row. A claim that comes to rest on one of them adds a row; the archetype alone does not.

## Maintenance

**A change that alters a row's evidence updates the row in the same change.** That covers a scenario added or retired, a stand-in fixture replaced by a real one, a baseline established or invalidated by a pin move, and a public claim widened or narrowed on joeagent.dev or in the README. A row whose evidence moved and whose status did not is wrong, not stale.
