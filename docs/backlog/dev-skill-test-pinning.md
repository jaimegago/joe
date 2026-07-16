Strengthen dev standard skill on regression tests and pinning
Status: open
Priority: next

Update the **dev-standards** skill (`~/.claude/skills/dev-standards/`) to state a pinning standard, using the findings of sessions `demo-bugfix-pins` and `rbac-engine-split` as the evidence. **The skill lives in the agent skills repo, not this one — this file is a cross-repo pointer only.** Nothing here is actionable inside `joe/`; the change lands in the skills repo and this item is closed from there.

The gap: the skill already says verification-before-claiming, but it does not say what a fix must *leave behind* so the next change cannot silently undo it. Both sessions below found real defects that existing tests did not catch, for two different structural reasons. Both are worth incorporating as worked examples rather than as an abstract rule.

## 1. A behavior fix ships with a regression test, proven by revert-run-restore

The standard: a commit that corrects behavior ships a test that **fails without the fix and passes with it**, and the author proves it by the revert-run-restore method — revert the fix hunk in the working tree, run the specific test, confirm it fails for the expected reason, restore. A test written against a fix that was never reverted is an assumption, not a pin: it can assert something the fix did not cause, or something that was already true. `demo-bugfix-pins` ran this on nine bug fixes and all seven claimed pins held, but two of them (the double-wrap fix and the declare-affordance fix) could only be reverted *hunk-wise* rather than file-wise, because later commits had changed the same files — worth naming in the skill, since a whole-file revert is the obvious first attempt and it breaks the build.

The counterpart matters as much: **a non-behavior change gets a documented not-pinned rationale, not a contrived test.** `demo-bugfix-pins` deliberately exempted two commits and recorded why — `a4ade60` (swapping the default Vite favicon for Joe's, a static `index.html` asset change with no runtime surface; the only possible test asserts the file's own contents back at itself) and `a4fdac7` (renaming four legacy "source" UI strings to "component" per D-0021, a label rename with no behavior change). The skill should make the exemption an explicit, recorded decision rather than a silent omission, so "no test" is distinguishable from "forgot the test" in review.

## 2. Unit-level pins do not protect the composition root

`rbac-engine-split` is the sharper example, and the one the skill currently has no answer for. D-0041/D-0043 wired the governance resolvers into `NewPolicyEngineWithGovernance`; that engine's **sole transport consumer** was `rbac.EnforcementMiddleware`, which had been a pass-through discarding its engine argument since the D-0008 demotion. The engine actually gating every accessor decision was a **second, bare** one built inside `api.New`, carrying neither resolver. Consequence: the launch-default `team_flat` read admit was structurally unreachable on the transport path — a non-admin principal got `403 no_grant` on `GET /api/v1/graph` under the launch default.

Every piece had tests. The resolvers had tests, the policy engine had tests, the middleware had tests, the accessor had tests — and all of them passed, because **nothing tested the wiring**. The defect lived in which instance got handed to whom at construction, which is exactly the seam unit tests are built to abstract away. Unit tests cannot see a dead consumer, a duplicate instance, or a discarded argument; they instantiate their own subject and are blind to how production assembles it.

The pattern answer to record in the skill, both halves:

- a **static construction guard** — a test asserting the constructor is called only where it may be (`TestGuard_PolicyEngineConstructedOnlyAtCompositionRoot` forbids any `rbac.NewPolicyEngine*` call outside `cmd/joe` and `_test.go` files), which is what makes a second bare instance impossible to reintroduce rather than merely absent today;
- an **end-to-end pin through the production composition path** — the assembly extracted into a named seam (`buildHTTPHandler`) so `main` and the regression test share one path, and the test drives a real request through it and asserts the governed decision. A pin that constructs its own engine proves nothing about the binary.

The generalization for the skill: when a defect's cause is "which instance was injected where", the test must run through the real composition root. Ask of any wiring-shaped fix — *would this test fail if the object under test were never wired into the binary at all?* If not, it is not a pin. See D-0107 for the full decision and D-0008 for the demotion that made the middleware inert.
