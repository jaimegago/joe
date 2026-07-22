Remediation execution — push a fix branch and open a PR
Status: open
Priority: later
Blocked-by: external (full-capabilities mode not implemented)

Full-mode Joe does not merely propose a fix; it pushes a branch and opens a PR for
a human to review. Proposals are already legal as labeled, unverified session
output (D-0140); this is the execution half.

Two **Mutate**-classed operations on **two components**:

- a **git push-ref tool** on the git component — branch push only, never the
  default branch. Merge stays human and provider-side.
- a **create-PR / create-MR operation** on the provider component.

Both are floor-gated, RBAC-resolved, and audited **per component** — a caller
entitled to push may not thereby be entitled to open a PR, and the audit trail
records which component each mutation landed on.

Transport split. The **code** moves over native git against the bare mirror
(`git-clone-freshness`): commit objects built at a pinned base with no worktree —
go-git's push support against a bare repository is to be verified when this is
built, not assumed. The **PR object** is provider-adapter REST, the surface that
already exists.

Commits are authored as Joe's own identity and carry the **session id**, so the
team-public session is the review artifact: a reviewer reading the PR can read the
reasoning that produced it.

A **generic git remote** (no provider component) degrades to branch-push only, with
no PR surface. State that honestly rather than failing the run.
