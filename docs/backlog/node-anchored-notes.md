# Node-anchored operational notes — Joe's memory surface

Status: open
Priority: next

Build the notes surface designed in
[`docs/reference/DESIGN-NODE-NOTES.md`](../reference/DESIGN-NODE-NOTES.md), under
D-0161 through D-0167 in [`docs/project/DECISIONS.md`](../project/DECISIONS.md).
**The design record holds the requirements (R1–R16); this item holds only the work
breakdown.** Read the record before starting any slice, and re-check any `file:line`
citation it carries — they are at commit `a829e96`.

## Slices

Each slice is independently mergeable, in this order.

**Slices 1 and 2 ship no notes.** They are prerequisites the feature cannot be built
without, and finishing either with no visible notes is the plan, not a stalled
feature.

1. **Classify the delta.** `BuildGraphDelta` gains added / changed-with-before /
   removed for nodes and edges, staying pure. Settles what "changed" means for a
   `metadata` blob. (R6, D-0164.) Ships no notes.
2. **Cap and shape `graph_related`.** Absolute bounds in characters per R13/R14, with
   the exact numbers set against the smallest model configuration; the intent filter
   per R7; the wrapper result per R15, which is a breaking change to the tool's result
   contract. (D-0165.) Ships no notes.
   - **The `(relation, direction) → intent` table is UNVERIFIED** and is this slice's
     to build, against the refresher construction sites, verifying edge direction at
     each. The design reasoned from relation names only.
3. **The notes table and the refresher producer.** R1, R2, the `refresher` author
   kind of R9, R10; a hook in `ApplyGraphDelta` over slice 1's classified delta;
   refresher notes summarised in the payload, not listed. (D-0161, D-0162.)
4. **The session producer.** The `ActionRead` note-writing tool per R4; node-id
   validation against the graph; the audit row in the write's transaction; TTL and
   re-confirmation per R5/R9. (D-0163, D-0166, D-0167.)
5. **The operator producer.** The UI write path, and the operator kind's TTL
   exemption — the only one D-0163 allows.

## Not this item's work

- Making the graph read path per-component — `investigations/agentic-path-rbac-read-enforcement.md`.
  The notes side stays stricter than its carrier until then (D-0162).
- `Edge.ComponentID` persistence — [`component-delete-graph-orphans`](component-delete-graph-orphans.md).
- The dead `Path` store method; `joe_warnings`; findings.
