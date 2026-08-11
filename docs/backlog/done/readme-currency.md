# Knowledge-store residue in `docs/integrations.md`

Status: done — both knowledge-store claims removed from `docs/integrations.md`: the `joe_knowledge_search` row dropped with the table's row set re-derived from `internal/mcp/tools.go`, and the knowledge-store clause cut from the `/joe ask` description; merged as `6e3b9fe` (jaimegago/joe#29), thread `integrations-doc-residue`

Priority: next

The `readme-currency` sweep (D-0149) removed the D-0113 knowledge-store residue from
`README.md` and from the config sample in `docs/configuration.md`. Two stale claims in
`docs/integrations.md` were found in the same pass and left untouched, because the session's
scope was those two files.

Both matter more than internal-doc drift usually does: `docs/integrations.md` is linked from
the README's Documentation table and from its Components section, so it is reachable in two
clicks from the repo front door.

## 1 — The MCP tool table lists a tool that does not exist

`docs/integrations.md:46` lists `joe_knowledge_search` ("Semantic search over runbooks and
docs") as an MCP tool. D-0113 deleted it across tool listing, dispatcher, and client. The live
roster is the seven tools constructed in `internal/mcp/tools.go:10-72` — `joe_graph_query`,
`joe_graph_related`, `joe_k8s`, `joe_metrics`, `joe_logs`, `joe_traces`, `joe_alerts` — pinned
at exactly seven by `TestNewServer_ToolCount` (`internal/mcp/server_test.go:33-40`). The table
row is a factual overclaim about the binary, not merely stale prose: a reader configuring an
MCP client would look for a tool that cannot be advertised.

Note the whole table was verified as correct in an earlier sweep, recorded in
`done/docs-reconcile-sweep.md:74` as "All 8 MCP tool names ... VERIFIED". That verification was
true when written and pre-dates D-0113. Re-derive the row set from `internal/mcp/tools.go`
rather than editing the one row, so the count and the names are checked together.

## 2 — The Slack command table names the deleted subsystem

`docs/integrations.md:67` describes `/joe ask <query>` as "Query the infrastructure graph and
knowledge store". The graph half is accurate; the knowledge-store half has no referent. The
Slack package carries no knowledge call — D-0113 removed `SearchKnowledge` and the
related-knowledge rendering, confirmed by a grep of `internal/slack/` finding no match. Cut the
clause and keep the claim, per the current-state convention.

## Already tracked elsewhere — do not open a second item

`docs/reference/joe-architecture.md` carries knowledge-store references at `:288`, `:541`, and
the blockquote at `:569`. These are **already** covered by
[`reference-docs-prune-reconcile`](../reference-docs-prune-reconcile.md) §3, which names lines 288
and 541 among its AMBIGUOUS candidates (a historical clause wrapping a live claim — cut the
clause, keep the claim) and the ~569 blockquote among the three units structurally organized
around a removal. That item also carries the CLAUDE.md convention-widening sub-task the sweep
must land with. Fold this work into that sweep rather than duplicating it.

`docs/project/SITE-CLAIMS.md:85` cites the knowledge store as the reason the act-policy seam is
reachable by no registered tool. That is a register entry recording the mechanism's basis, which
is what the register is for, and is **not** drift.

## Closed

Landed as `6e3b9fe` on `main` via `jaimegago/joe#29`. The body above is kept as the
historical statement of the drift; this section records what the fix covered, because
**only §1 and §2 above were ever this item's own work** and a reader of the archive would
otherwise take §3 as closed too.

**Delivered.** Both claims in `docs/integrations.md` are gone. The `joe_knowledge_search`
row was removed by re-deriving the table's row set from `internal/mcp/tools.go` rather than
deleting the single row, per §1's own instruction — the table now carries exactly the seven
constructed tools, in the same order, matching the count `TestNewServer_ToolCount` pins. The
`/joe ask` description lost the knowledge-store clause and kept the accurate graph half,
licensed by `internal/slack/` carrying no knowledge call. `grep -ni knowledge
docs/integrations.md` returns nothing.

**Never this item's work, and still open elsewhere.** §3 above is an instruction, not a
task list: the `docs/reference/joe-architecture.md` residue at `:288`, `:541` and the ~`:569`
blockquote belongs to [`reference-docs-prune-reconcile`](../reference-docs-prune-reconcile.md)
§3, which was confirmed still open at `next` and still naming those lines when this item was
archived. That item also carries the CLAUDE.md convention-widening sub-task its sweep must
land with. `docs/project/SITE-CLAIMS.md:85` is a register entry, not drift, and needs nothing.

**Surfaced by the fix, not fixed by it.** `internal/mcp/tools.go:5` reads "all 8 Joe tools"
while the function below it constructs seven — the same D-0113 residue, in Go source. The
implementing thread left it deliberately: a `.go` file widens the diff from the `docs` class
into `go-backend`. It is filed separately and is not tracked by this item.
