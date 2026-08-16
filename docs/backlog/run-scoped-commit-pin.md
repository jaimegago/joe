Run-scoped commit pin — one snapshot per run across the three git tools
Status: open

`docs/backlog/repo-search-tool.md` (now archived at
[`done/repo-search-tool.md`](done/repo-search-tool.md)) claimed that the search
"executes at a pinned commit, so search, read, and log all answer from one
snapshot per run". Read strictly that is a **run-level invariant across three
tools**, and `repo_search` cannot deliver it alone — see D-0152, which shipped
the tool and weakened the claim rather than pretending it held.

**Narrowed, not closed, by `pinned-read-and-markers` (D-0154).** The read leg is
delivered: `git_read` now takes an optional `commit` on the same contract
`repo_search` states, honours it in both read and list mode, and reports the
resolved full hash back on every result. What this item is now for is the
**run-scoped** version described below.

What the two tools guarantee today is per call: the commit is an optional
argument, absent it resolves the clone's head and reports it, present it answers
at exactly that commit or fails, and every result carries the commit answered at.
What nothing guarantees is that a later `git_read` or `git_log` in the same run
answers from that same commit.

**A correction to how this item used to describe its own residue.** It called
threading the commit through subsequent calls "loop discipline — an instruction
to the model, not a mechanism". That was wrong when written, and the correction
is the point rather than a detail: `git_read` took no commit at all, so there was
nothing to thread. The residue was not a mechanism the loop might neglect, it was
an **unavailable** one, and a reader picking this item up would have taken the
structural version below for an upgrade over a weaker existing mechanism that did
not exist. With the read leg delivered the original sentence becomes true for
`git_read` — threading it is now loop discipline, with a lever that exists. It
remains false for `git_log`, which still takes no commit.

The gap is not theoretical. Under [`git-clone-freshness`](git-clone-freshness.md)
the clone gains fetch-before-analysis, at which point the substrate can move
underneath a run: two calls a minute apart can answer from different commits with
nothing in either answer saying so.

**The structural version, which is what remains open.** A run pins a commit per
component on first touch, and all three git tools answer from it for the rest of
the run without the loop threading anything. A caller-supplied commit still wins,
and still fails loudly rather than silently answering elsewhere.

**Why this is its own item and not part of the search tool.** It reaches into
three existing tool contracts (`git_read`, `git_log`, `git_diff`) plus wherever
per-run state would live, and folding it into "add a search tool" would be scope
creep. It was **not costed** during the `repo-search-tool` design session: doing
so needs a scoped read-only investigation into the trio's current contracts and
into where a run-scoped store would sit, which that session did not run. No
`Priority:` line is set here for the same reason — triaging it would mean
inventing a horizon nobody has measured.

The `git_read` leg was carved out and delivered separately because a specific
defect made it urgent on its own terms: `repo_search` advertised a citation rule
— re-read at the reported commit before citing — that the tool surface could not
perform. That is a narrower claim than costing this item, and it did not cost it.

Open questions a costing pass would have to close:

- Where the per-run pin lives, and what "the run" is at the boundary between the
  user-task loop and the autonomous agent, which register their tools separately.
- Whether pinning applies to `git_log`, whose natural argument is a count of
  recent commits rather than a point in history.
- What happens when a component is first touched after a fetch has already moved
  the clone mid-run — the pin makes the run self-consistent from that point, not
  retroactively.
