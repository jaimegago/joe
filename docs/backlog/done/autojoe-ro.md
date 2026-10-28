# Read-only Auto Joe — first slice: a budgeted run on `agent:core` delivering typed, ranked findings into a session

Status: done — the first slice shipped as two pull requests: the run side as
joe#91 (`autojoe-ro-run-build`) and the report side as joe#93
(`autojoe-ro-report-build`), both merged on `main` on 2026-10-28. Archived by
the joe-pm thread `retire-delivered-items`. The body below is preserved as it
stood; what it defers (the push surface, the weight label) stays deferred and
is not this item's.
Design of record: the joe-pm ledger's `AUTOJOE-RO` milestone (`queue/autojoe-ro.md`) and its two phase items, `queue/autojoe-ro-run.md` and `queue/autojoe-ro-report.md`, both settled in full on 2026-10-27. This entry is the build order for the first slice: it restates the settled contracts as requirements and acceptance criteria and lists what joe has to add. It decides nothing those items left settled, and it reopens nothing.

## Context

**Auto Joe is joe analysing a cluster without a human turn.** A run reads the
cluster, judges what it sees against two references joe already holds — the
system's self-report and the graph — and hands the operator a ranked set of
findings, each typed as an **issue**, a **risk** or a **graph update**, each
with a proposed action the run never executes. The operator aims it only by
scoping through the graph. A run widens neither its own authority nor its own
aim.

The milestone is split on the write boundary. **This item is the read-only
half**, which runs on the principal joe already has and depends on nothing in
full mode. The write half — executing on a proposal, any autonomous mutation
— is `AUTOJOE-RW` in the ledger, blocked on full mode (D-0019), and is out of
scope here in its entirety.

**The first slice** is one run that can be switched on, scoped through the
graph, held to a budget, run as `agent:core`, and that delivers typed, ranked
findings with proposals into a session and a findings view. Two settled
contracts are deliberately **not** in it (§ Out of scope): the push surface and
the component weight label.

## Verified current state — what joe lacks today

Verified 2026-10-27 against `main` at `ab0869d` by two read-only
investigations recorded in the ledger (`threads/autojoe-ro-run.md`,
`threads/autojoe-ro-report.md` § Ground truth). Re-derive exact file:line from
the live tree before acting.

1. **The principal exists; nothing but the refresher carries it.** `agent:core`
   is minted as `svc:agent:core` by `AgentCorePrincipal()` through
   `ServicePrincipal` (`internal/rbac/identity.go:76-98`) and attached to a
   context once, at boot, for the refresher (`cmd/joe/server.go:792-797`,
   `internal/coreagent/refresh.go:127-129`). No HTTP route and nothing in the
   agent loop carries it. Its read-only posture is enforced by four
   mechanisms, none of them a prompt: the accessor refuses any action but
   read and query (`internal/access/access.go:252-261`);
   `auto_promote_reads` admits only read, only for the exact `agent:core`
   principal, only for component types switched on
   (`internal/rbac/policy.go:317-370`,
   `internal/store/migrations/024_agent_read_promotions.up.sql:33-38`);
   the seeded `unassigned` zone allows `["read"]`
   (`006_rbac.up.sql:30-34`); and the boot-resolved write floor has no
   lowering operation (`internal/safety/floor.go:22-54`).
2. **No non-HTTP way to start an agent run exists.** The loop's only entry
   points are `POST {prefix}/tasks` and `/tasks/stream`
   (`internal/api/tasks.go:1181-1187`), run synchronously under the request
   context. The refresher is a ticker (`refresh.go:81-100`, `:155-172`) that
   never calls the LLM. No scheduler, cron, queue, informer or watch exists;
   Kubernetes is polled per resource (`internal/coreagent/k8s_refresh.go:110`).
3. **Budgets are per task and have no reserve.** `DefaultMaxIterations = 20`
   (`internal/agentloop/constants.go:10`); a storage-backed session token
   ceiling checked after every LLM call (`internal/agentloop/limits.go:39`,
   `agent.go:257-261`); a per-task wall-clock timeout of five minutes
   (`tasks.go:276-301`). At the iteration cap one forced synthesis returns a
   partial with `stop_reason = max_iterations` (D-0099, D-0100); at the token
   ceiling the partial is discarded. That discard is the headroom gap
   `loop-budget-exhaustion` § 2 names as its open prerequisite.
4. **A run record exists but is not the task's.** `agent_runs` carries id,
   session, state, `started_at`, `ended_at`
   (`internal/store/migrations/010_run_model.up.sql:13-30`), with no
   started-by column, written only by the `/sessions/{id}/runs` handler
   (`internal/api/runs.go:54-90`); a `/tasks` call creates none. The "who" of
   a session is `agent_sessions.creator_principal`
   (`025_session_schema_rewrite.up.sql:40-46`).
5. **No agent-authored findings store exists, and the dormant tables do not
   fit.** `findings` is a human cross-session annotation — source and target
   session, author principal, body (`011_findings_warnings.up.sql:15-27`,
   `internal/findings/types.go:1-34`) — written only by
   `POST /sessions/{id}/findings` and read by nothing in the UI;
   `joe_warnings` has no production writer; `doc_proposals` was dropped by
   migration 031. Neither carries a type, severity, status or node.
6. **Session output is free text.** `chat_messages` is role, content, tool
   name, tool args (`022_chat_sessions.up.sql:28-38`); `turn-kind` is a
   closed `answer|question|refusal` marker, parsed and not persisted
   (`internal/agentloop/turnkind.go:14-36`). D-0140 keeps proposals as
   labelled, unverified session prose with no typed contract.
7. **No operator weighting exists.** A component carries id, type, name,
   config, status, sync and error fields and nothing else
   (`internal/store/models.go:9-19`); a zone is id, name, description,
   `allowed_actions` (`internal/rbac/zones.go:37-43`). No label, tag, weight,
   priority or tier field on either.
8. **No outbound surface exists.** No mail and no event push anywhere; the
   Slack bot replies and never initiates; `GET /alerts` is still the stub
   D-0038 named (`internal/api/webui.go:846-853`).
9. **The graph carries `first_seen` and `last_seen` on nodes and `created_at`
   on edges, nothing else** (`002_graph.up.sql:2-24`); no persisted delta, no
   change event.

Items 7 and 8 are recorded because the design reacts to them (every component
weighs the same; pull before push), not because this slice fills them.

## Requirements and acceptance criteria

Framed as requirements; the implementer derives file:line and approach. The
section headings follow the two phase items so that each requirement can be
traced to the contract it restates.

### Principal

- **A run executes as `agent:core`** — the identity joe already has, carried
  into the agent loop for the run's duration. No new principal is minted; a
  dedicated `agent:auto` was considered and rejected in the design.
  Acceptance: a run's reads resolve to `svc:agent:core` at the accessor and
  are admitted by exactly the authority the refresher has — grants plus the
  per-type `auto_promote_reads` admit — so **a run sees exactly what the
  refresher sees**, and a scope drawn through the graph can never name a
  component the run is then denied.
- **Read-only is a property of the identity, not of the prompt.** Acceptance:
  `agent:core` holds no write grant after this slice lands; a Mutate attempted
  by a run is denied at the same enforcement seam as everything else, and no
  code path added here grants, promotes or widens anything.
- **The activator is never the principal.** Whoever switches a run on — an
  operator, or a schedule — the run executes as `agent:core`. Acceptance: the
  run record names the activator; the session's `creator_principal` is
  `svc:agent:core`, and that creator is what tells a run's session from a
  person's.

### Activation

- **An operator trigger is the primitive; a schedule is a standing trigger the
  operator installs over a scope.** Cluster events are not a trigger: joe
  polls and does not watch, and an event-driven run has no bound on what a
  day costs. Acceptance: a run can be started by an operator act and by an
  installed schedule, and by nothing else.
- **One run is one scope, one pass, one budget, one report, one record.** The
  scope is a graph selection — components, zones, labels on components —
  resolved at run start to a fixed component set that the run record carries.
  Acceptance: two runs over the same selection at different times compare on
  the resolved set each recorded, not on the selection.
- **At most one live run per scope.** Acceptance: a trigger while a run on
  that scope is live is refused, not queued, and the refusal is visible to
  the activator.
- **A run never widens its own authority or its own aim.** Acceptance: a run
  registers nothing, promotes nothing, applies no change to any scope,
  schedule or weight, and writes nothing to the graph; graph writes stay the
  refreshers' (D-0110).

### Budget

- **A token ceiling and a wall-clock ceiling per run**, set from deployment
  defaults when the trigger or schedule is installed. Acceptance: both
  ceilings are recorded on the run, and a schedule's cost is readable as runs
  times ceiling from what the operator set.
- **At exhaustion the run stops and reports partial.** Degrading — narrowing
  the scope or dropping a finding type to fit — is rejected. Acceptance: a
  run stopped by either ceiling names the components the pass completed and
  the ones it did not, with the ceiling that stopped it as the run's stop
  reason; **an absent finding on an uncovered component is never presented
  as a clean bill**.
- **The report is funded from a reserve outside the run's ceiling**, so a run
  that hits its budget can still say so. This is the reserved-headroom design
  `loop-budget-exhaustion` § 2 names as the open prerequisite for its own
  sibling paths; this slice builds it for the run path. Acceptance: a run
  that exhausts its token ceiling still emits its report with its coverage and
  stop reason, and the report's own tokens are not what breached the ceiling.

### Incrementality and the finding record

- **A finding is a typed, persisted record with a stable key** — its type, the
  graph node or component it is about, and its claim — attached to the run
  that produced it. Its identity is derived from those three and never from
  the run. Acceptance: the same finding established by two consecutive runs on
  one scope has one identity.
- **The record is not joe's `findings` table and not `joe_warnings`.** Both
  match in name and not in shape, and reusing either would make *who wrote
  this* ambiguous on a record whose author is a principal. The implementer
  names the new record; the constraint is that the name does not collide
  with the human annotation.
- **A run remembers exactly one thing: the previous completed run on the same
  scope, through its persisted findings.** The run re-finds — it must, to
  know a finding still holds — and labels each of its findings *new* or
  *standing*, and each previous finding it did not re-find *resolved* if that
  finding's component was covered and *unknown* if it was not. Acceptance:
  "what changed since last time" is the diff of two consecutive runs' finding
  sets on one scope, plus the scope's own delta, and every finding carries
  one of the four labels.
- **The previous run is a reference for labelling, never for judgment.**
  Acceptance: every finding is re-established from the self-report and the
  graph on every run; a previous run's findings are read for labelling only,
  never as evidence, and a previous finding never suppresses a current one.
- **What a run does not remember.** Nothing across scopes; no operator
  reaction to a finding; no learned weights; no graph history — `first_seen`
  and `last_seen` are evidence a finding may cite, not memory the run keeps.
  Acceptance: no persisted state other than the finding records and the run
  record is read by a run.
- **Persisting LLM judgment in joe's own store is admissible on D-0163's
  terms**, with the next run as the reconciler: a finding not re-established
  by a run whose scope covered its subject is resolved, and one whose subject
  has left every scope decays by time. Per D-0167, generalised: **a finding
  narrows nothing and permits nothing.** Acceptance: no authorization or
  enforcement path reads the finding record.

### The run record

- **A run has a record** carrying the activator, the resolved component set,
  both ceilings, the components covered and not covered, the stop reason, and
  the session it wrote. `agent_runs` is the nearest existing shape; whether
  it is extended or a sibling is added is the implementer's, under the
  constraint that a `/tasks` call still creates none. Acceptance: delivery
  reads coverage and stop reason from the run record, never from prose.

### The finding model

- **Every finding carries a type, the reference it was judged against, a
  severity, and a proposed action.** Three types, defined by what the finding
  claims about the present and by nothing else:
  - **issue** — the system reports that it is not in the state it declares
    for itself. Reference: the self-report — spec against status, conditions,
    probes, reconciliation not converged, errors emitted. Proposed action: a
    fix, attached and never executed here.
  - **risk** — the intended state is met now, but a named condition would
    turn this into an issue. Reference: joe's operational judgment,
    disciplined by the condition. **A risk that cannot name its condition is
    not emitted.** Improvement and performance findings live here.
  - **graph update** — there is evidence of a system outside joe's remit, or
    a relationship no refresher derives. Reference: the graph as known state.
    **Proposal-only**: the proposed action is to register a component — an
    admission, operator-only by D-0029 — or to accept a relationship; the run
    applies neither.
- **Impact feeds severity, not type.** A broken thing nobody uses is a
  low-severity issue, not a non-issue.
- **Not a type:** improvement, performance (a severity); area of interest (a
  pointer, not a finding — a "look here" must say why, which makes it a risk
  with a condition); discovery, drift, unregistered (each rejected in the
  design). Acceptance: the type vocabulary is closed at three.
- **Operator input is structural only.** There is no goal file, no
  natural-language reference, no per-run instruction. The run carries its own
  standing questions, one per type. Acceptance: nothing the operator types in
  prose reaches a run.

### Ranking

- **Severity is a band on one fixed scale, P0 to P3, the same in every
  installation.** The scale is joe's; the operator moves a finding within it
  by weight, never redefines it. Acceptance: a band means the same thing in
  every install, so bands compare across runs.
- **Three inputs, and no others.** *Consequence* — what the finding claims,
  classified by the LLM into a closed vocabulary: `unavailable` or `degraded`
  for an issue; `would be unavailable` or `would be degraded` for a risk; a
  graph update has none. The self-report's own urgency enters here and only
  here. *Reach* — the components that depend on the subject, read from the
  graph's dependency edges walked in the direction of dependence, derived by
  code and never asserted by the model. *Weight* — the operator's label on a
  component; **no such field exists today, so every component weighs the
  same until it does** (§ Out of scope), and the computation takes weight as
  an input from the start so the label slots in without a redesign.
- **The band is computed by code from (type, consequence, reach, weight)
  through a fixed table; the model classifies, it never ranks.** Type bounds
  the band: an issue may reach P0; a risk is at most P1; a graph update is
  always P3 and is not ranked against the other two. Reach and weight move a
  finding within its type's bounds and never past them. The indicative table
  the build starts from — **the cells are the build's, the bounds are not**:

  | type | consequence | base band | with reach or weight |
  |---|---|---|---|
  | issue | unavailable | P1 | P0 |
  | issue | degraded | P2 | P1 |
  | risk | would be unavailable | P2 | P1 |
  | risk | would be degraded | P3 | P3 |
  | graph update | — | P3 | P3 |

  Acceptance: the same evidence yields the same band on every run; a test
  pins the bounds per type and fails if any cell crosses them.
- **A finding carries its derivation or is not emitted**: the consequence
  class with the one self-report line that supports it, the dependents
  counted, the weight applied. Within a band, order is by reach times
  weight, then by how recent the evidence is.

### Delivery

- **A run's output is a session**, created by `svc:agent:core`, team-public,
  durable, linkable to an incident, rendered by the UI today. Labelled
  unverified session output is already legal (D-0140), so no new write
  surface is needed for a finding to exist. Acceptance: a run's findings are
  readable in the Sessions page as any other session is.
- **Findings are structured, and the structure is the run's output
  contract.** A finding is a record — type, subject node, reference claim,
  consequence, band, derivation, proposal, evidence pointer — persisted with
  the run and rendered into the session's final turn, not prose alone.
  Acceptance: the final turn is assembled from the records; nothing in the
  rendered list exists only in prose.
- **A standing finding is delivered on every run, with its label.** The list a
  human reads is the current state of the scope, not a changelog.
- **A findings view in the UI, by band**, carrying *new*, *standing* and
  *resolved*, with a link from each finding to its subject node and to its
  evidence. This is the honest replacement for the alerts widget D-0038
  retired — a dashboard of data backed by a real endpoint, as
  `launch-ui-polish` asks for. Acceptance: the view reads the finding records
  through an endpoint of its own; no stub, no placeholder widget.
- **Graph updates are delivered apart.** Not in the ranked list: a separate
  *proposed admissions* list addressed to admins, placed where registration
  happens, each carrying its evidence. A proposal naming a type the allow-list
  blocks says so, so it reads as information and not as an action.
- **Every delivered finding links to its evidence, and every run states its
  limits.** The turn in the run's session where the finding was established,
  and the graph node it is about, are reachable from the finding. The run's
  report carries a limits section, as D-0140 requires of an assessment: the
  scope covered, whether the run completed or stopped on its budget, and
  what it did not look at. Acceptance: a run that stops early says so in the
  same place its findings are.
- **Not an incident.** Declaring one is a human act in the regime-control
  zone and joe's own declare seam is closed by design. A P0 finding is
  delivered so that a human can declare, with the run's session as the one
  to promote. Acceptance: no code path added here declares, promotes to, or
  links an incident on its own.

### Proposed solutions

- **A proposal is the change that would resolve one finding, stated against
  a pinned base, and never executed here.** Zero or one per finding — the one
  the run recommends; alternatives are prose in the finding's rationale, not
  proposals. Every proposal is labelled unverified (D-0140).
- **Three shapes, by where the declared state lives.**
  - **Repository change** — a unified diff against a pinned commit of a
    registered git component, file paths named. The shape
    `remediation-execution` executes as a branch push and a pull request
    without re-deriving anything. Preferred whenever the subject is anchored
    to a repository; until `iac-graph-ingestion` anchors workloads to
    repositories, a run reaches the manifest through the git read tools or
    does not propose this shape.
  - **Live-object change** — the target by its own identifier (for
    Kubernetes: group/version/resource, namespace, name), the base by the
    observed generation, the change as a patch. For a subject no repository
    anchors. Executable only by an apply operation `AUTOJOE-RW` would own;
    nothing in joe applies one today, and nothing here adds one.
  - **Operator action** — steps for a human, where joe cannot execute. Every
    graph update's proposal is this shape. Marked not executable.
- **Common to every shape**: the **target** (graph node id and the system's
  own identifier); the **base** (the observed state the proposal was computed
  from); the **change**; the **expected effect** (the identity of the finding
  it resolves, and how the reference would show it resolved); and **limits**
  (what was not checked). **A proposal with no base is not emitted.**
  Acceptance: a test rejects a proposal missing any of the five.
- **Invariants.** A proposal never carries a secret value; it names a secret
  by reference. Its target lies within the run's scope. Acceptance: a
  proposal whose target is outside the run's resolved component set is not
  emitted, and a test pins that.
- **The shapes and their fields are the whole of what this item shares with
  the write half.** Commit identity, the session id on commits, and the floor
  and RBAC gating of execution are `remediation-execution`'s and are not
  restated here.

## What this slice adds to joe

The list the two phase items' § What remains enumerate, as one build:

1. **A non-HTTP way to start an agent run** — the loop reached by an operator
   trigger and by a schedule, not only by `POST /tasks`.
2. **The agent loop carrying `agent:core`** for a run's duration, attached
   today only to the refresher.
3. **A per-run budget seam** — token and wall-clock ceilings — **with reserved
   report headroom**, so exhaustion yields a partial report rather than a
   discarded one.
4. **The run record** — activator, resolved scope, ceilings, coverage, stop
   reason, session.
5. **The typed finding record** with its stable key and its four labels, under
   a name of its own.
6. **The band table** and the code that computes a band from (type,
   consequence, reach, weight), with reach derived from the graph.
7. **The session rendering** of a run's findings and limits from the records.
8. **The findings view** in the UI, by band, with the separate proposed
   admissions list for admins.
9. **The three proposal shapes** and their common fields, with the no-base and
   in-scope invariants enforced.

## Out of scope — stated, not left to inference

- **The push surface.** Decided and deferred by the design: push fires on a
  band threshold the operator sets, default P0 only, over one generic signed
  webhook, with PagerDuty, Slack and the rest as adapters over it; push fires
  on *new* only, and on a standing finding whose band rose; a graph update
  never pushes. joe has no outbound surface of any kind, and building one is
  its own feature with its own credential and auth story. **Its own backlog
  item, after this slice delivers pull.** Nothing here precludes it: the
  band, the labels and the run record are what it would read.
- **The component weight label.** The operator's one input to ranking. No
  field exists on a component or a zone today; the nearest item is
  `component-description-field`, blocked on `component-type-contract`, and
  weight is a different field that plausibly shares the blocker. **Not filed
  by this item.** Until it lands every component weighs the same, and the
  band computation takes weight as an input from the start so the label is a
  field addition, not a redesign. Zones are not a stand-in: a zone says who
  may act, not what matters.
- **Execution of any proposal** — `AUTOJOE-RW`, blocked on full mode.
- **Autonomous graph maintenance.** The refreshers already create, move and
  retire nodes under an admitted component; the LLM cannot write the graph
  (D-0110). A run proposes an admission and applies nothing.
- **Cluster events as a trigger**, and any informer or watch.
- **A per-installation severity scale, or any prose input** to scope, aim or
  rank a run.
- **The deployment-level iteration-cap knob** (`loop-budget-exhaustion` § 1)
  and the sibling termination paths adopting the synthesis seam
  (`loop-budget-exhaustion` § 2, § 3). The headroom this slice builds for the
  run path is the prerequisite those name; adopting it there is theirs.

## Schema ownership — a note for `pr-surface-joe-mention`

D-0140 deferred a typed input contract and a structured output schema until
the first parsing consumer, and `pr-surface-joe-mention` expects to be that
consumer and to own the schema. **This item is a second candidate**: a run's
findings are assembled into a session's final turn from structured records,
which is parsing in the same sense. Whichever builds first owns the schema;
the other adopts it rather than minting a sibling. Neither item is blocked on
the other by this note.

## Relations

- `loop-budget-exhaustion` — names the reserved-headroom design as its open
  prerequisite; this slice builds that headroom for the run path.
- `remediation-execution` — the consumer of the repository-change shape;
  `Blocked-by: external (full-capabilities mode not implemented)`, so the
  shape is produced here long before anything executes it.
- `iac-graph-ingestion` — anchors workloads to repositories; until it lands,
  the repository-change shape is reachable only through the git read tools.
- `launch-ui-polish` — the findings view is a dashboard of data backed by a
  real endpoint, which is what that item asks for in place of the retired
  alerts widget.
- `full-mode-rbac-track` — the write half's principal track. Its "verified
  current state" obstacle 4 (no autonomous principal exists) is stale at
  `ab0869d`: `agent:core` exists under the `svc:` prefix. Its requirements
  stand; it is not needed for this half and nothing here touches it.
- `component-description-field`, `component-type-contract` — where the weight
  label plausibly lands; see § Out of scope.
- `pr-surface-joe-mention`, `change-impact-analysis` — § Schema ownership.

## References (link, do not duplicate)

- `docs/project/DECISIONS.md` **D-0019** — the trust model: the autonomous
  principal graduates to writes by an operator's grant on the capability
  ladder; that grant is `AUTOJOE-RW`'s activation and ends the read-only
  half by the operator's act.
- **D-0029** — component registration is an admission, admin-gated; why a
  graph update is proposal-only.
- **D-0038** — the alerts widget was retired pending a purpose-built surface.
- **D-0099**, **D-0100** — forced synthesis and `stop_reason`; the partial
  report builds on them.
- **D-0110** — the graph is deterministic-only; a run never writes it.
- **D-0140** — labelled unverified proposals are legal session material; the
  limits section; the deferred output schema.
- **D-0163**, **D-0167** — the terms on which persisted LLM judgment is
  admissible, and that it never permits.
