Dumb content-search tool over the local git clones
Status: open
Priority: now
Blocked-by: repo-registration-path

A content-search tool over the clones Joe already keeps on disk for every registered
git component. Substring and regex matching, no ranking, no model inside the tool
boundary — dumb in the D-0141 sense: output is a deterministic function of the
arguments plus the substrate.

Shape. Per-component, with caller-principal read resolution through the governed
accessor, Read-classed at authorship (an explicit classifier row, not a default).
Fleet-wide search is achieved by **loop iteration over components**, never by one
global query — that keeps every hit attributable to a component whose read the
caller is actually entitled to. Results are bounded, with an explicit truncation
marker when the bound bites, so the loop can see that it did not get everything.
Optional path scoping. The search executes **at a pinned commit**, so search, read,
and log all answer from one snapshot per run.

Hits are leads, never citable. A search result is input to the loop's triage; a
claim is cited only after a `git_read` re-read at the known commit.

Registered on the user-task registry beside the existing git trio. Independently
useful well beyond change-impact analysis — it is the missing primitive for any
question that starts "where in our repos does X appear".
