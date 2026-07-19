How does `joe` actually resolve its home directory?
Status: open

Opened from an observation made during the `v0.1.0` go/no-go
released-binary integrity check (session `release-pipeline-03`, D-0123).

The operator booted the release binary with an overridden `HOME` environment
variable, expecting that override to isolate the boot from any real local
state. It did not: the binary still resolved the operator's real `~/.joe`
directory — database, encryption key, registered components, session
archive — and ran live refreshers against that real data. This contradicts
the assumption that `joe` resolves its home directory (and therefore its
default data directory) from `$HOME`.

Scope: a read-only investigation of how `joe` actually resolves its home /
data directory — read the relevant resolution code, identify what input(s)
it actually honors (`$HOME`, a different env var, a config default, an
absolute fallback, something else), and document the finding. Any change to
that resolution behavior, or to how the release runbook's integrity check
should isolate itself as a result, is a separate future decision — not in
scope for this item.
