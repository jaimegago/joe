# Backlog Index

Active open-work items, one row per file directly under `docs/backlog/` (excluding
this index and the `done/` archive). This is a **derived summary, not a source of
truth** — regenerate it from the directory whenever items are added, removed, or
their status changes.

Per row: **slug** = filename without `.md`; **title** = the file's first line (heading
marker stripped); **status** = the leading clause of the file's `Status:` line
(defaulting to `open` if absent); **priority** = the value of the file's `Priority:`
line (exactly one of `now`, `next`, `later`) — an item with no `Priority:` line renders
as a **blank cell, never a default value**. The `Blocked-by:` line is deliberately not
carried here; it is detail-pass information read from the item file. Follow a slug to its
file for the full status, priority, blocked-by, and body.

| Slug | Title | Status | Priority |
|------|-------|--------|----------|
| [`adapter-dispatch-consolidation`](adapter-dispatch-consolidation.md) | Adapter construction is fragmented across divergent type-keyed paths | open |  |
| [`agent-identity-doc`](agent-identity-doc.md) | Agent identity and authentication — explanation doc and the implementation it trickles into | open |  |
| [`aws-credential-provider`](aws-credential-provider.md) | Backlog — AWS-shaped credential provider (deferred from A003) | deferred from A003 |  |
| [`azure-adapter-connect-skeleton`](azure-adapter-connect-skeleton.md) | Backlog — Azure adapter Connect is a skeleton (no SDK client built) | deferred |  |
| [`azure-credential-provider-and-connect`](azure-credential-provider-and-connect.md) | Backlog — Azure credential provider + Connect implementation (deferred from A003) | deferred from A003 |  |
| [`backlog-priority-triage`](backlog-priority-triage.md) | Backlog priority triage — assign Priority lines across all existing items | open | now |
| [`build-version-instrumentation`](build-version-instrumentation.md) | Build & version instrumentation — deferred fast-follows | open |  |
| [`capability-map`](capability-map.md) | Capability-map Concepts page — deferred follow-ups | open |  |
| [`captain-write-consolidation`](captain-write-consolidation.md) | Backlog — Consolidate the captain detach/attach write patterns behind one tx-aware seam | deferred |  |
| [`case-study-kiro-redo`](case-study-kiro-redo.md) | Redo the Kiro case study against current Joe architecture | open |  |
| [`component-registration-guide`](component-registration-guide.md) | Component registration guide — UI-driven public how-to | in-progress |  |
| [`components-page-restructure`](components-page-restructure.md) | Components page restructure — deferred remainder | open |  |
| [`console-brand-tokens`](console-brand-tokens.md) | Console brand token layer | open |  |
| [`credential-stderr-surface-teardown`](credential-stderr-surface-teardown.md) | Backlog — Tear down the vestigial credential-stderr surface | open |  |
| [`crd-gvr-resolution`](crd-gvr-resolution.md) | CRD GVR resolution in the on-demand core tools (deferred from crd-gvr-resolution / D-0094) | open |  |
| [`cross-incident-relink`](cross-incident-relink.md) | Backlog — Attach a former (resolved) incident master as a participant of a new incident | deferred |  |
| [`datastore-uri-credential-provider`](datastore-uri-credential-provider.md) | Backlog — Datastore URI credential provider (deferred from A003) | deferred from A003 |  |
| [`db-retention-story`](db-retention-story.md) | Backlog — Whole-database retention story (audit rotation, LLM-usage/review-jobs/clarifications pruning, legacy-session disposition, DB-size observability) | open |  |
| [`denial-feedback-popup`](denial-feedback-popup.md) | Denial-feedback pop-up: a reactive notification when a user action is refused | deferred |  |
| [`discovery-clarifications-pipeline`](discovery-clarifications-pipeline.md) | Discovery-to-clarifications pipeline — unpark and finish the onboarding, facts, and needs-human surfaces | open |  |
| [`docs-public-refit`](docs-public-refit.md) | Public docs refit — deferred follow-ups | open |  |
| [`edge-type-literal-consolidation`](edge-type-literal-consolidation.md) | Edge-type literal consolidation and constraining graph_edges.relation | open |  |
| [`feature-clips`](feature-clips.md) | Landing-page demo clips — record, encode, and commit to joeagent.dev | in-progress |  |
| [`full-mode-rbac-track`](full-mode-rbac-track.md) | Full-capabilities-mode RBAC: fail-closed at empty RBAC + a dedicated autonomous principal | deferred |  |
| [`governed-connectivity-check-surface`](governed-connectivity-check-surface.md) | Backlog — Governed connectivity-check surface for components | open |  |
| [`health-readiness-surface`](health-readiness-surface.md) | Health and readiness probe surface (/livez, /readyz) | open |  |
| [`healthz-endpoint-surface`](healthz-endpoint-surface.md) | Unauthenticated health-probe surface — standards-anchored build spec (/healthz, /livez, /readyz) | open |  |
| [`incident-view-filter-to-mine`](incident-view-filter-to-mine.md) | Backlog — "Filter to mine" in the incident view | deferred |  |
| [`launch-positioning-and-employer-decoupling`](launch-positioning-and-employer-decoupling.md) | Launch positioning, the open-source launch-blocker checklist, and decoupling from a former employer | open |  |
| [`launch-ui-polish`](launch-ui-polish.md) | Purpose-built dashboard to replace the retired fabricated-data landing page | open |  |
| [`learn-from-sessions-fate`](learn-from-sessions-fate.md) | Backlog — Fate of the learn-from-sessions (knowledge extraction) feature | decided |  |
| [`llm-observed-health-surface`](llm-observed-health-surface.md) | LLM observed-health surface — last-successful-call display and failure recording | open |  |
| [`loop-budget-exhaustion`](loop-budget-exhaustion.md) | Backlog — Loop budget-exhaustion follow-ups (deferred from loop-budget-exhaustion / D-0096–D-0100) | open |  |
| [`mcp-client-absence-guard`](mcp-client-absence-guard.md) | Guard test pinning the no-MCP-client invariant | open |  |
| [`oasis-relationship`](oasis-relationship.md) | OASIS evaluation relationship and the deferred post-Phase-2 re-score | open |  |
| [`observation-default`](observation-default.md) | Full-mode boot posture: resolve the write floor down under the full-mode-requires-auth fail-closed guarantee | open |  |
| [`openai-compat-adapter`](openai-compat-adapter.md) | Backlog — OpenAI-compatible adapter fast-follows | in-progress |  |
| [`postgres-backend-completion`](postgres-backend-completion.md) | Backlog — Make the PostgreSQL (pgx) backend functional | open |  |
| [`posture-endpoint-grants-signal`](posture-endpoint-grants-signal.md) | Posture endpoint: a coarse "any write grants exist" signal (full-mode only) | deferred |  |
| [`posture-prompt-conflation`](posture-prompt-conflation.md) | Observation-posture conflation — deferred follow-ups | open | later |
| [`promotion-requirements-single-source`](promotion-requirements-single-source.md) | Backlog — Drive component promotion validation from a single per-Kind requirements source | open |  |
| [`public-docs-feature-inventory`](public-docs-feature-inventory.md) | Public Docs Feature Inventory | open |  |
| [`rbac-v2`](rbac-v2.md) | Full RBAC v2 — role indirection, group subjects, and granular permissions | open |  |
| [`read-posture-latch`](read-posture-latch.md) | Read-posture latch — launch as team_flat, defer the zoned (full-mode) surfaces | in-progress |  |
| [`refresher-rbac-degradation`](refresher-rbac-degradation.md) | Refresher per-resource-type degradation follow-ups (deferred from refresher-rbac-degradation / D-0093) | open |  |
| [`register-component-config-default`](register-component-config-default.md) | Config-less registration default — deferred fast-follows | open |  |
| [`registered-components-required-framing`](registered-components-required-framing.md) | Joe is near-useless without registered components — make that framing explicit | open |  |
| [`registry-auth-pair-credential-provider`](registry-auth-pair-credential-provider.md) | Backlog — Registry-auth-pair credential provider (deferred from A003) | deferred from A003 |  |
| [`release-pipeline`](release-pipeline.md) | Release pipeline — tag cut and distribution-posture doc sweep (-02) | in-progress |  |
| [`remote-host-diagnostics`](remote-host-diagnostics.md) | Remote host diagnostics — OS-level stats of managed hosts as a future component type | deferred |  |
| [`session-content-search`](session-content-search.md) | Backlog — conversational session content-search | deferred |  |
| [`session-doc-debt`](session-doc-debt.md) | Session-subsystem documentation debt | open |  |
| [`sessions-view-paging`](sessions-view-paging.md) | Backlog — P3: paging for the sessions two-view split | deferred |  |
| [`skills-governance-hardening`](skills-governance-hardening.md) | Skills governance hardening — admin-gate the HTTP surface, audit lifecycle events, load-time integrity | open |  |
| [`tool-class-break-tests`](tool-class-break-tests.md) | Break-tests pinning tool action-class for the two currently-unpinned cases | open |  |
| [`trim-deadonarrival-component-types`](trim-deadonarrival-component-types.md) | Dead-on-arrival component types — deferred wiring and a related Test-control UX bug | open |  |
| [`unauth-health-surface`](unauth-health-surface.md) | Unauthenticated health surface — auth-posture and information-exposure analysis | open |  |
| [`web-search-tool`](web-search-tool.md) | Backlog — Web-search tool fast-follows | open |  |
