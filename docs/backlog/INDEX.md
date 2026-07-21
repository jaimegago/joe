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
| [`act-policy-vestigial`](act-policy-vestigial.md) | ActPolicy opt-in seam: vestigial after knowledge prune | open | later |
| [`adapter-dispatch-consolidation`](adapter-dispatch-consolidation.md) | Adapter construction is fragmented across divergent type-keyed paths | open | later |
| [`admin-bootstrap-cli`](admin-bootstrap-cli.md) | Admin bootstrap CLI — open residue | open | next |
| [`admin-bootstrap-cli-04`](admin-bootstrap-cli-04.md) | The `cfg.Database` override block is written out three times | open | later |
| [`agent-identity-doc`](agent-identity-doc.md) | Agent identity and authentication — explanation doc and the implementation it trickles into | open | next |
| [`aws-credential-provider`](aws-credential-provider.md) | Backlog — AWS-shaped credential provider (deferred from A003) | deferred from A003 | later |
| [`azure-adapter-connect-skeleton`](azure-adapter-connect-skeleton.md) | Backlog — Azure adapter Connect is a skeleton (no SDK client built) | deferred | later |
| [`azure-credential-provider-and-connect`](azure-credential-provider-and-connect.md) | Backlog — Azure credential provider + Connect implementation (deferred from A003) | deferred from A003 | later |
| [`build-version-instrumentation`](build-version-instrumentation.md) | Build & version instrumentation — deferred fast-follows | open | later |
| [`capability-map`](capability-map.md) | Capability-map Concepts page — deferred follow-ups | open | later |
| [`captain-write-consolidation`](captain-write-consolidation.md) | Backlog — Consolidate the captain detach/attach write patterns behind one tx-aware seam | deferred | later |
| [`case-study-kiro-redo`](case-study-kiro-redo.md) | Redo the Kiro case study against current Joe architecture | open | next |
| [`component-delete-graph-orphans`](component-delete-graph-orphans.md) | Component-delete graph-orphans — deferred residue (cross-component edges, write-only Edge.ComponentID, refresher UI visibility, other FK-less component_id tables) | open | later |
| [`component-registration-guide`](component-registration-guide.md) | Component registration guide — UI-driven public how-to | in-progress | now |
| [`components-page-restructure`](components-page-restructure.md) | Components page restructure — deferred remainder | open | later |
| [`console-brand-tokens`](console-brand-tokens.md) | Console brand token layer | open | later |
| [`credential-stderr-surface-teardown`](credential-stderr-surface-teardown.md) | Backlog — Tear down the vestigial credential-stderr surface | open | later |
| [`crd-gvr-resolution`](crd-gvr-resolution.md) | CRD GVR resolution in the on-demand core tools (deferred from crd-gvr-resolution / D-0094) | open | later |
| [`cross-incident-relink`](cross-incident-relink.md) | Backlog — Attach a former (resolved) incident master as a participant of a new incident | deferred | later |
| [`datastore-uri-credential-provider`](datastore-uri-credential-provider.md) | Backlog — Datastore URI credential provider (deferred from A003) | deferred from A003 | later |
| [`db-retention-story`](db-retention-story.md) | Backlog — Whole-database retention story (audit rotation, LLM-usage/review-jobs pruning, legacy-session disposition, DB-size observability) | open | later |
| [`demo-runbooks-e2e`](demo-runbooks-e2e.md) | Demo runbooks as E2E tests | open | later |
| [`denial-feedback-popup`](denial-feedback-popup.md) | Denial-feedback pop-up: a reactive notification when a user action is refused | deferred | later |
| [`dev-skill-test-pinning`](dev-skill-test-pinning.md) | Strengthen dev standard skill on regression tests and pinning | open | next |
| [`docs-public-refit`](docs-public-refit.md) | Public docs refit — deferred follow-ups | open | later |
| [`edge-type-literal-consolidation`](edge-type-literal-consolidation.md) | Edge-type literal consolidation and constraining graph_edges.relation | open | later |
| [`export-import-components`](export-import-components.md) | Component export/import — a portable registration format | open | later |
| [`full-mode-rbac-track`](full-mode-rbac-track.md) | Full-capabilities-mode RBAC: fail-closed at empty RBAC + a dedicated autonomous principal | deferred | later |
| [`governed-connectivity-check-surface`](governed-connectivity-check-surface.md) | Backlog — Governed connectivity-check surface for components | open | later |
| [`healthz-endpoint-surface`](healthz-endpoint-surface.md) | Unauthenticated health-probe surface — standards-anchored build spec (/healthz, /livez, /readyz) | open | next |
| [`iac-graph-ingestion`](iac-graph-ingestion.md) | IaC declared-to-live bridge upgrade — identifier-derived edges, git_repo anchoring, and flux | open | later |
| [`incident-view-filter-to-mine`](incident-view-filter-to-mine.md) | Backlog — "Filter to mine" in the incident view | deferred | later |
| [`joe-home-resolution`](joe-home-resolution.md) | How does `joe` actually resolve its home directory? | open | next |
| [`launch-ui-polish`](launch-ui-polish.md) | Purpose-built dashboard to replace the retired fabricated-data landing page | open | next |
| [`learn-from-sessions-fate`](learn-from-sessions-fate.md) | Backlog — Fate of the learn-from-sessions (knowledge extraction) feature | open | next |
| [`llm-observed-health-surface`](llm-observed-health-surface.md) | LLM observed-health surface — last-successful-call display and failure recording | open | later |
| [`loop-budget-exhaustion`](loop-budget-exhaustion.md) | Backlog — Loop budget-exhaustion follow-ups (deferred from loop-budget-exhaustion / D-0096–D-0100) | open | later |
| [`mcp-client-absence-guard`](mcp-client-absence-guard.md) | Guard test pinning the no-MCP-client invariant | open | next |
| [`oasis-relationship`](oasis-relationship.md) | OASIS evaluation relationship and the deferred post-Phase-2 re-score | open | next |
| [`observation-default`](observation-default.md) | Full-mode boot posture: resolve the write floor down under the full-mode-requires-auth fail-closed guarantee | open | next |
| [`observe-k8s-resolver`](observe-k8s-resolver.md) | Node-type vocabulary re-encoded by consumers — the gitops provides-matcher's phantom arms and the test fixtures that green them | open | next |
| [`openai-compat-adapter`](openai-compat-adapter.md) | Backlog — OpenAI-compatible adapter fast-follows | in-progress | now |
| [`postgres-backend-completion`](postgres-backend-completion.md) | Backlog — Make the PostgreSQL (pgx) backend functional | open | later |
| [`posture-endpoint-grants-signal`](posture-endpoint-grants-signal.md) | Posture endpoint: a coarse "any write grants exist" signal (full-mode only) | deferred | later |
| [`posture-prompt-conflation`](posture-prompt-conflation.md) | Observation-posture conflation — deferred follow-ups | open | later |
| [`promotion-requirements-single-source`](promotion-requirements-single-source.md) | Backlog — Drive component promotion validation from a single per-Kind requirements source | open | later |
| [`rbac-v2`](rbac-v2.md) | Full RBAC v2 — role indirection, group subjects, and granular permissions | open | later |
| [`read-posture-latch`](read-posture-latch.md) | Read-posture latch — launch as team_flat, defer the zoned (full-mode) surfaces | in-progress | now |
| [`read-posture-visibility`](read-posture-visibility.md) | Read posture is invisible in every UI and CLI surface | open | next |
| [`reference-docs-prune-reconcile`](reference-docs-prune-reconcile.md) | Backlog — Residue from the reference-docs prune reconcile | open | next |
| [`refresher-rbac-degradation`](refresher-rbac-degradation.md) | Backlog — Refresher per-resource-type degradation follow-ups (deferred from refresher-rbac-degradation / D-0093) | open | later |
| [`register-component-config-default`](register-component-config-default.md) | Config-less registration default — deferred fast-follows | open | later |
| [`registered-components-required-framing`](registered-components-required-framing.md) | Joe is near-useless without registered components — make that framing explicit | open | later |
| [`registry-auth-pair-credential-provider`](registry-auth-pair-credential-provider.md) | Backlog — Registry-auth-pair credential provider (deferred from A003) | deferred from A003 | later |
| [`remote-host-diagnostics`](remote-host-diagnostics.md) | Remote host diagnostics — OS-level stats of managed hosts as a future component type | deferred | later |
| [`review-jobs-orphaned-table`](review-jobs-orphaned-table.md) | review_jobs: orphaned table disposition | open | next |
| [`session-content-search`](session-content-search.md) | Backlog — conversational session content-search | deferred | later |
| [`session-doc-debt`](session-doc-debt.md) | Session-subsystem documentation debt | open | next |
| [`sessions-admin-delete-affordance`](sessions-admin-delete-affordance.md) | Admin delete affordance gap on non-owned sessions in the Sessions list | open | later |
| [`sessions-view-paging`](sessions-view-paging.md) | Backlog — P3: paging for the sessions two-view split | deferred | later |
| [`site-claims-ship-trigger`](site-claims-ship-trigger.md) | Site-claims register: no trigger fires when a newly shipped mechanism falsifies unregistered published copy | open | next |
| [`skills-governance-hardening`](skills-governance-hardening.md) | Skills governance hardening — admin-gate the HTTP surface, audit lifecycle events, load-time integrity | open | next |
| [`tool-class-break-tests`](tool-class-break-tests.md) | Break-tests pinning tool action-class for the two currently-unpinned cases | open | next |
| [`trim-deadonarrival-component-types`](trim-deadonarrival-component-types.md) | Unregistrable component types — deferred wiring, a Test-control UX bug, and a latent read-promotion residue | open | later |
| [`web-search-tool`](web-search-tool.md) | Backlog — Web-search tool fast-follows | open | later |
