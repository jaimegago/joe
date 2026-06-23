Session-subsystem documentation debt
Status: open

Rehomes the two concrete doc-cleanup items from the retired
`JOE_PROJECT_KNOWLEDGE.md` §11.3. Both were **re-verified against the live tree
on 2026-06-24** rather than copied from JPK's figures — and that verification
changed the picture: item 1 is already resolved, only item 2 remains open.

## 1. Stale "migration 009" comments in webui.go — RESOLVED (verified)

JPK §11.3 reported that `internal/api/webui.go:229` and `:769` still cited
"migration 009" for the `incident_state` / `linked_incident_id` CHECKs that
migration **025** now owns (stale comments; behavior correct).

**Verified 2026-06-24: this is already fixed.** There is no remaining "migration
009" reference in `internal/api/webui.go` (`grep -n "migration 009\|009_"` returns
nothing). The comments now cite migration 025 correctly at
[internal/api/webui.go:249](../../internal/api/webui.go:249) ("the CHECK in
migration 025 keeps it null there") and
[internal/api/webui.go:806](../../internal/api/webui.go:806) ("migration-025").
The JPK line numbers (229, 769) no longer point at these comments — the file has
since shifted. **No action remains for this item; recorded for closure only.**

## 2. Phantom B001 inventory reports — STILL OPEN (verified absent)

JPK §11.3 reported that `docs/DESIGN-CHAT-SESSIONS.md` claimed three `b001-*`
inventory reports had landed under `docs/investigations/`, but no such files
exist on disk.

**Verified 2026-06-24: the reports are still absent.** `ls docs/investigations/ |
grep -i b001` returns nothing. The design doc itself has since been updated to
acknowledge the gap rather than assert the reports exist — at
[docs/DESIGN-CHAT-SESSIONS.md:827](../DESIGN-CHAT-SESSIONS.md:827): "The three
B001 inventory reports were never produced and are **not** present under
`docs/investigations`; landing the dated as-built snapshots remains open
doc-debt." (Earlier sessions correctly declined to fabricate them.)

**Open work:** either produce the three dated as-built B001 inventory snapshots
under `docs/investigations/`, or formally close the doc-debt by removing the
"remains open doc-debt" claim at `docs/DESIGN-CHAT-SESSIONS.md:827`. The as-built
session reality is already captured inline in that design doc (§10, §12 notes)
and verified in code, so the snapshots are a completeness nicety, not a
correctness gap.
