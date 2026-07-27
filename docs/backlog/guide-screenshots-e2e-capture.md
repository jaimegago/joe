# Deterministic guide screenshots from the E2E harness
Status: open
Priority: later

If guide screenshots are ever wanted, they must be captured deterministically from
the `joe-oasis-e2e` Playwright harness and regenerated per release as a build
artifact — never captured manually against an ad hoc running instance. Manual
screenshots were rejected and their placeholders removed from
`docs/public/guides/register-kubernetes.md` in the `guide-screenshot-placeholders`
session (D-0148): the guides are self-sufficient click-by-click prose, and
screenshots of a fast-moving v0.x UI go stale and become walk-backable claims.
