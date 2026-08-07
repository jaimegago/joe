Change-impact analysis — a capability of the existing agent loop
Status: open
Priority: next
Blocked-by: repo-search-tool

The capability as decided in D-0140 (the capability, its scope split, its
five-step method, and its honest-limits requirements) and D-0141 (the purity
model for the evidence tools it runs on).

`git-clone-freshness` is a **co-requisite**: fetch-before-analysis is part of the
method's truth discipline, not an optimisation. The `Blocked-by` line above
carries the search edge; this sentence carries the freshness edge. Those two are
now the only edges: the operator path to a `git_read`-able repository exists as of
D-0150, so `repo-registration-path` is no longer the transitive root blocker and
the substrate this item builds on can be populated.

Deliverables.

- The method packaged as a **first-party skill**, published in a trusted-source
  repository operators install from.
- **Quickstart documentation of that trusted source** — the skill is useless if
  nobody knows where to get it.
- A **v1.x line item for remediation proposals**: suggested patches carried inside
  an assessment, explicitly labeled as unverified. Legal today as session material
  (D-0140); execution is `remediation-execution`.

Upgrade path. Identifier-level cross-repo detection and deployed-version grounding
arrive with `iac-graph-ingestion`. The assessment's limits section is designed to
**shrink as those edges land** — each edge family retires one stated limitation
rather than requiring the method to be rewritten.

Deliberately deferred: a typed input contract and a structured output schema. Both
wait for the first parsing consumer, which is expected to be the PR surface
(`pr-surface-joe-mention`).
