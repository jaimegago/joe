# Knowledge store maturation — governance, sync disposition, and orphaned-writer disposition

Status: open
Priority: later

Filed from the session `knowledge-store-maturation`, whose Phase 1 recon established the
as-built picture below. That session made two scoped changes (the tier-less create default
flipped curated → derived; `save_knowledge_entry` parked out of the agent:core registry)
and deferred everything else here. See the D-entry for the launch posture decision.

## What the recon found

The knowledge subsystem is substantially **more live** than its reputation. It was not
parked and it is not schema-only.

- **The REST write surface is live and authenticated-only.** `registerKnowledgeRoutes`,
  `registerProposalRoutes`, and `registerDriftRoutes` are all called in
  `api.Server.RegisterRoutes` — sitting directly beneath the D-0081 parked block, so the
  parking pattern was available and deliberately not applied. Entry create/update/delete,
  source create/delete, proposal create/approve/reject, and drift detection are all
  reachable by any authenticated principal. Governance is **edge auth and nothing else**:
  no audit row, no principal stamping, no admin gate, and no component RBAC (the guarded
  accessor is component-scoped; these paths carry no componentID).
- **The proposals arm is live**, including drift detection and the doc-drafter, and
  approval records **no approver identity** — `ApprovedAt` only, with no field on the
  `Proposal` struct to hold one.
- **The agent's knowledge surface is `search_knowledge` plus the doc-copilot trio**
  (`detect_doc_drift`, `generate_doc_draft`, `publish_doc_update`) on the user task loop.
  `generate_doc_draft` is Read-classed, so an observation-mode Joe **does** write proposal
  rows into Joe's own store; `publish_doc_update` is Mutate-classed and floor-denied.
- **Confluence and Notion sync is built but dormant** behind `knowledge.sync_enabled`
  (default `false`). The syncers and the polling coordinator are real; the
  `POST /knowledge/sources/{id}/sync` route is a **stub** that validates and returns 202
  without queueing anything — the background poller is the only trigger.
- **`save_knowledge_entry` was orphaned, and is now parked.** It was registered on the
  agent:core registry but unreachable in fact: nothing drives that registry
  (`Agent.ExecuteTool`/`GetAvailableTools` have no callers) and coreagent runs no LLM loop.
  This session removed the registration per the D-0081 pattern and pinned the absence.
- **`SourceTypeHuman` is never assigned** anywhere in non-test code, so human-authored
  entries land with an empty `source_type`. `SourceTypeSession` was set only by the now-parked
  writer.
- **No UI affordance exists** — `grep -ri knowledge ui/src` returns nothing. The entire
  subsystem is API-only.

## Open work

### 1. Governance posture for knowledge writes

The launch posture accepts thin governance; this is the item that fixes it. Decide and
implement:

- **Audit rows.** The knowledge and proposal handlers write none. Every other governed
  write surface in Joe audits. Decide whether these join the audit repository and at what
  granularity (per write, or per curated write only).
- **Principal stamping.** `Entry.CreatedBy` exists on the model and is never populated from
  the request principal. The provenance field is there; nothing fills it.
- **Admin gate for curated.** Curated entries are permanently immutable, so creating one is
  an irreversible act available to every authenticated principal. The tier default flip
  (this session) removes the *accidental* path to it, but not the deliberate one. Decide
  whether authoring curated is an admin-gated act like the other irreversible surfaces
  (cf. the skills mutating routes, the read-posture flip).
- **Whether the whole surface should be admin-gated** rather than authenticated-only, given
  it has no per-role gate at all today.

### 2. Approver identity on proposals

`Service.Approve` stamps `ApprovedAt`; the `Proposal` struct has no approver field and the
handler passes no principal down. "Who approved this?" is unanswerable from the proposal.
The doc-proposals public guide now states this gap explicitly — landing the feature revises
that copy and its SITE-CLAIMS entry.

### 3. Sync enablement story and the stub sync route

Confluence and Notion are built, tested, and dormant. Decide: is `sync_enabled` a supported
launch feature (needs a connect/configure story, credential handling for the source config
blob, and docs), or does the arm get removed? Related: the `POST /sources/{id}/sync` route
promises a queued sync and delivers a no-op 202 — either make it trigger a real sync or
stop advertising it as a trigger in the api-reference.

### 4. Final disposition of the parked `save_knowledge_entry`

Parking is a holding position, not an answer. Either:

- **Build it properly** — governed (audit, principal stamping), and reachable, which means
  deciding what actually drives the agent:core registry, since nothing does today; or
- **Delete it** along with the dead `Agent.ExecuteTool`/`GetAvailableTools` surface, and
  drop its classifier row.

Note the trap if it is ever unparked as-is: it is **Read-classed** (per D-0020, Joe's own
model-maintenance tools are Reads), so it passes the write floor **unconditionally,
observation mode included**. An observation-mode install would gain an unaudited, unstamped
knowledge writer. That is the stated Read/Mutate boundary working as designed — Joe's own
store is not a managed system — but it means the write floor is **not** the backstop here,
and the governance in item 1 is the only thing that would be.

### 5. `SourceTypeHuman` assignment

Human-authored entries should carry `source_type: human`. The constant exists and is never
used. Trivial on its own; it belongs with the principal-stamping work in item 1, since both
are provenance on the same create path.

### 6. Placeholder — IaC-repo LLM-derived inferences

Future work will have Joe read IaC repositories and derive inferences about infrastructure
(ownership, intent, drift from declared state). Those inferences are **intended to land in
the derived tier**, through whatever governed write path this item produces. This is the
main reason the derived tier and its write path matter beyond tidiness: there is a real
consumer coming, and it should not arrive to find it must invent its own ungoverned path.
Whatever is decided in items 1 and 4 should be checked against this use case before it is
called done.
