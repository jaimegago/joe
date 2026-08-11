Git clone freshness: fetch-before-analysis, and the move to a bare mirror
Status: open
Priority: next

Today a git component's clone is fetched **only at Connect** — first use clones,
a subsequent Connect pulls — and there is no scheduled re-fetch anywhere
(`internal/adapters/git/git.go:98-121`). Staleness is therefore unbounded: a
long-lived daemon can answer from a clone that is weeks behind, with nothing in
the answer saying so.

Two pieces of work, one item.

**Fetch before analysis.** A run fetches each repository it consults at the start,
pins the post-fetch HEAD, and cites it. That is what makes "assessed at commit X,
fetched at time T" a true statement rather than a hopeful one.

**Evolve the substrate.** The current shape is a non-bare clone plus `Pull`
fast-forward — a human-workflow shape. The machine shape is a **bare mirror**
with `fetch --prune --force`, which survives force pushes and deleted branches,
and which answers reads and grep from the object database at a pinned commit with
**no worktree on the read path**. Per D-0141.

**Decide the fetch-failure posture in-session**: either proceed against the stale
copy with the staleness stated explicitly in the assessment, or refuse outright.
Pick one; do not leave it implicit.

The recon's open question about worktree fidelity — whether what grep sees matches
what the pinned commit contains — dissolves once the worktree leaves the read path.
