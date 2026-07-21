# Site-claims register: no trigger fires when a newly shipped mechanism falsifies unregistered published copy

Status: open
Priority: next

`docs/project/SITE-CLAIMS.md` states its maintenance duty as bidirectional, and both
halves are keyed to something the register already knows about:

- **Mechanism-side.** A session that changes a *listed* mechanism flags a joeagent.dev
  revision in its session report.
- **Publish-side.** A session that publishes new load-bearing copy to a publication
  source adds the corresponding register entry in the same session.

There is a third case, and neither trigger catches it: a session ships a **new**
operator-facing mechanism, touches **no** published page, and the mechanism falsifies
published copy whose subject was never a registered mechanism. Nothing in the register's
conventions makes that session look at the site at all.

This is not hypothetical. Session `admin-bootstrap-cli` (D-0129) shipped
`joe admin bootstrap`, a second cold-start path to the first admin. Admin provisioning
was not a registered mechanism, so the mechanism-side trigger had nothing to fire on, and
that commit touched no file under `docs/public/`, so the publish-side trigger did not fire
either. It nevertheless falsified statements on the operations page, the install-and-build
page, and the web-UI guide — corrected a session later by `admin-bootstrap-cli-03`
(D-0130), which found them only because a separate reconciliation pass went looking.

**This is a convention question and needs its own decision.** Do not change the register's
conventions as a side effect of some other session's work. Candidate directions to weigh —
recorded as candidates, not as a choice already made:

1. **A ship-side trigger.** Oblige any session that lands a new operator-facing mechanism
   (a subcommand, a config key, an endpoint, a boot rule) to scan the published surface for
   copy the mechanism falsifies, and to say in its session report either what it corrected
   or that it found nothing. Cost: a scan on every such session, most of which find
   nothing. Benefit: it keys on what the session *did*, which is the only thing the session
   reliably knows.
2. **Pre-registering claim families rather than individual claims.** Register the *topic* —
   "how a first admin is obtained", "what a deployment with no identity provider can do" —
   so that a mechanism landing inside a registered family fires the existing mechanism-side
   trigger without the family having had a specific claim entry first. Cost: families are
   fuzzier to scope than mechanisms and could over-fire. Benefit: no new trigger class; it
   widens the existing one.

The two are not exclusive. Either way the decision should say how the trigger is *checked*,
since the register has no enforcement beyond convention — a session's own report is
currently the whole mechanism.

Whatever is decided lands as a decision entry plus an edit to the register's Conventions
block; D-0130 deliberately recorded the gap as an observation and left both untouched.
