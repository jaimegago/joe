# review_jobs: orphaned table disposition

Status: open

The `review_jobs` table has **zero Go references in the tree**. A repo-wide search for
`review_jobs` / `reviewjobs` / `ReviewJob` across every `.go` file returns nothing: no
reader, no writer, no repository, no struct. There is no code-review package under
`internal/` either.

It was created by migration `007_review_jobs.up.sql` for the Phase 10 Code Review
Integration — a PR/MR review job queue keyed by `event_id`
(`<platform>:<owner>/<repo>#<pr_number>:<head_sha>`). That subsystem's Go side is gone; the
table outlived it. The only migration to touch it since is `023_source_to_component.up.sql`,
which renamed `source_id` → `component_id` as part of the D-0021 lexical sweep — a rename
applied for consistency, not evidence of a live consumer.

Filed by session `knowledge-store-prune`, which found it while mapping the doc-proposals
pipeline. It is **not** part of that pipeline: doc proposals live in `doc_proposals`
(migration 005), and their approve/reject path is `proposals.Service`, unrelated to this
queue. The prune's drop migration (031) therefore deliberately left `review_jobs` standing
rather than bury an unrelated disposition inside a knowledge-store migration.

Decide drop-or-revive in its own session. If the code-review integration is not coming
back, this is a drop migration in the shape of 029/031. If it is, the table is the schema
half of a feature whose Go half needs rebuilding, and that decision should be made
deliberately rather than by leaving the table as an accident.

Note it is also one of the **unbounded tables with no deletion path** — nothing writes it
today, but nothing would ever delete from it either, so if it is revived it belongs in the
v2 retention story alongside the other tables tracked there.
