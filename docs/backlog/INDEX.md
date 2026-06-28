# Backlog Index

Active open-work items, one row per file directly under `docs/backlog/` (excluding
this index and the `done/` archive). This is a **derived summary, not a source of
truth** — regenerate it from the directory whenever items are added, removed, or
their status changes.

Per row: **slug** = filename without `.md`; **title** = the file's first line (heading
marker stripped); **status** = the leading clause of the file's `Status:` line
(defaulting to `open` if absent). Follow a slug to its file for the full status and body.

| Slug | Title | Status |
|------|-------|--------|
| [`adapter-dispatch-consolidation`](adapter-dispatch-consolidation.md) | Adapter construction is fragmented across divergent type-keyed paths | open |
| [`aws-credential-provider`](aws-credential-provider.md) | Backlog — AWS-shaped credential provider (deferred from A003) | deferred from A003 |
| [`azure-adapter-connect-skeleton`](azure-adapter-connect-skeleton.md) | Backlog — Azure adapter Connect is a skeleton (no SDK client built) | deferred |
| [`azure-credential-provider-and-connect`](azure-credential-provider-and-connect.md) | Backlog — Azure credential provider + Connect implementation (deferred from A003) | deferred from A003 |
| [`build-version-instrumentation`](build-version-instrumentation.md) | Build & version instrumentation — deferred fast-follows | open |
| [`captain-write-consolidation`](captain-write-consolidation.md) | Backlog — Consolidate the captain detach/attach write patterns behind one tx-aware seam | deferred |
| [`case-study-kiro-redo`](case-study-kiro-redo.md) | Redo the Kiro case study against current Joe architecture | open |
| [`component-registration-guide`](component-registration-guide.md) | Component registration guide — UI-driven public how-to | in-progress |
| [`cross-incident-relink`](cross-incident-relink.md) | Backlog — Attach a former (resolved) incident master as a participant of a new incident | deferred |
| [`datastore-uri-credential-provider`](datastore-uri-credential-provider.md) | Backlog — Datastore URI credential provider (deferred from A003) | deferred from A003 |
| [`denial-feedback-popup`](denial-feedback-popup.md) | Denial-feedback pop-up: a reactive notification when a user action is refused | deferred |
| [`edge-type-literal-consolidation`](edge-type-literal-consolidation.md) | Edge-type literal consolidation and constraining graph_edges.relation | open |
| [`full-mode-rbac-track`](full-mode-rbac-track.md) | Full-capabilities-mode RBAC: fail-closed at empty RBAC + a dedicated autonomous principal | deferred |
| [`governed-connectivity-check-surface`](governed-connectivity-check-surface.md) | Backlog — Governed connectivity-check surface for components | open |
| [`health-readiness-surface`](health-readiness-surface.md) | Health and readiness probe surface (/livez, /readyz) | open |
| [`healthz-endpoint-surface`](healthz-endpoint-surface.md) | Unauthenticated health-probe surface — standards-anchored build spec (/healthz, /livez, /readyz) | open |
| [`incident-view-filter-to-mine`](incident-view-filter-to-mine.md) | Backlog — "Filter to mine" in the incident view | deferred |
| [`launch-positioning-and-employer-decoupling`](launch-positioning-and-employer-decoupling.md) | Launch positioning, the open-source launch-blocker checklist, and former-employer decoupling | open |
| [`launch-ui-polish`](launch-ui-polish.md) | Purpose-built dashboard to replace the retired fabricated-data landing page | open |
| [`learn-from-sessions-fate`](learn-from-sessions-fate.md) | Backlog — Fate of the learn-from-sessions (knowledge extraction) feature | decided |
| [`oasis-relationship`](oasis-relationship.md) | OASIS evaluation relationship and the deferred post-Phase-2 re-score | open |
| [`openai-compat-adapter`](openai-compat-adapter.md) | Backlog — OpenAI-compatible adapter fast-follows | in-progress |
| [`orphaned-tool-registration-cleanup`](orphaned-tool-registration-cleanup.md) | Orphaned tool registration cleanup — safety entries for the removed local-tool tree | open |
| [`posture-endpoint-grants-signal`](posture-endpoint-grants-signal.md) | Posture endpoint: a coarse "any write grants exist" signal (full-mode only) | deferred |
| [`promotion-requirements-single-source`](promotion-requirements-single-source.md) | Backlog — Drive component promotion validation from a single per-Kind requirements source | open |
| [`public-docs-feature-inventory`](public-docs-feature-inventory.md) | Public Docs Feature Inventory | open |
| [`rbac-v2`](rbac-v2.md) | Full RBAC v2 — role indirection, group subjects, and granular permissions | open |
| [`read-posture-latch`](read-posture-latch.md) | Read-posture latch — launch as team_flat, defer the zoned (full-mode) surfaces | in-progress |
| [`register-component-config-default`](register-component-config-default.md) | Config-less registration default — deferred fast-follows | open |
| [`registered-components-required-framing`](registered-components-required-framing.md) | Joe is near-useless without registered components — make that framing explicit | open |
| [`registry-auth-pair-credential-provider`](registry-auth-pair-credential-provider.md) | Backlog — Registry-auth-pair credential provider (deferred from A003) | deferred from A003 |
| [`session-content-search`](session-content-search.md) | Backlog — conversational session content-search | deferred |
| [`session-doc-debt`](session-doc-debt.md) | Session-subsystem documentation debt | open |
| [`sessions-view-paging`](sessions-view-paging.md) | Backlog — P3: paging for the sessions two-view split | deferred |
| [`tilde-expansion-helper-unification`](tilde-expansion-helper-unification.md) | Backlog — Unify the tilde-expansion helpers | deferred |
| [`tool-class-break-tests`](tool-class-break-tests.md) | Break-tests pinning tool action-class for the two currently-unpinned cases | open |
| [`trim-deadonarrival-component-types`](trim-deadonarrival-component-types.md) | Dead-on-arrival component types — deferred wiring and a related Test-control UX bug | open |
| [`unauth-health-surface`](unauth-health-surface.md) | Unauthenticated health surface — auth-posture and information-exposure analysis | open |
