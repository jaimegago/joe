Repo registration path — no operator path to a git_read-able repository exists
Status: open
Priority: now

A read-only trace at main 275e810 established that no component type an operator
can register today reaches the clone-holding git adapter. The type `git`
constructs `gitadapter` (`internal/api/components.go` `newAdapterForType`;
`cmd/joe/server.go` `connectSourcesDefault` boot pass) but was removed from the
registrable set by the D-0111 trim (commit f57cb63, 2026-07-16;
`internal/store/constants.go` marks it UNREGISTRABLE, not credential-wired) and
would also be refused at promotion as credential-unwired
(`internal/credential/wiring.go`). The registrable repo-shaped types `github` and
`gitlab` construct provider-API adapters exposing only PR and MR operations,
neither implementing the `gitadapter` interface, so `git_read`, `git_log`, and
`git_diff` fail the guard type-assertion with `ErrWrongAdapterType` for them;
neither type has a refresh case, so no registrable component produces a
`git_repo` anchor node, and the git refresher creates zero edges even when it
fires.

Consequence: `git_read` and siblings are dead for all registrable components; the
git evidence program (D-0141 layered dumb-tools model, `git-clone-freshness` bare-
mirror direction, `change-impact-analysis`) currently builds on an unpopulatable
substrate. D-0141's basis claim that every git component already has a clone is
vacuously true — the trim landed six days before D-0141's recon, which verified
the adapter's behavior but not that the set of git components can be non-empty.

Two fix directions, to be settled in a design session before any build prompt:
wire a credential path for the git type and restore it to the registrable set; or
teach the github and gitlab types to also stand up a clone adapter, which
collides with the one-adapter-per-component guard seam (Go type assertion in
`internal/access/access.go`) and is therefore structural, not additive — this
collision is also the first battle-test of `component-type-contract` element two.

When the fix direction is settled, the resulting decision entry must annotate
D-0141's basis.

Blocks `git-clone-freshness`, `repo-search-tool`, and (transitively, through both
of those) `change-impact-analysis`.
