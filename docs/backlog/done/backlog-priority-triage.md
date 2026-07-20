# Backlog priority triage — assign Priority lines across all existing items

Status: done — triage pass executed (D-0127)
Priority: now

The backlog file convention gained two optional lines after the status line —
`Priority:` (`now` / `next` / `later`, absent = untriaged) and `Blocked-by:` (a
one-directional dependency edge; see D-0095 and `docs/project/pm-convention.md`).
The convention and the INDEX Priority column landed in session
`backlog-priority-field`, but **no existing item was assigned a priority** — every
row currently renders a blank Priority cell by design.

This item is the deferred triage pass: a chat-driven, maintainer-dictated sweep
over every file directly under `docs/backlog/` to assign each one a `Priority:`
line (or deliberately leave it untriaged). It is `now` because the priority field
is only useful once the backlog has actually been triaged — an all-blank column
gives no picking signal, so the triage should happen before the backlog is next
consulted to choose work.

Scope for the follow-up session:

- Walk the backlog items, maintainer dictates `now` / `next` / `later` per item;
  items the maintainer chooses to leave untriaged keep no Priority line.
- While in each file, normalize any pre-existing informal dependency wording (e.g.
  the `Depends on:` line in `adapter-dispatch-consolidation.md`) to the canonical
  `Blocked-by:` form where it still applies. This normalization belongs here, not
  in the `backlog-priority-field` session, which left existing bodies untouched.
- Regenerate `docs/backlog/INDEX.md` so the Priority column reflects the assigned
  values.
- Execute as a one-shot session; move this file to `docs/backlog/done/` on
  completion.
