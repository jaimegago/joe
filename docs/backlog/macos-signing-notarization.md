# Sign and notarize released macOS binaries

Status: open
Priority: later

The `quickstart-corrections` session (2026-07-23) documented a macOS Gatekeeper
quarantine workaround in `docs/public/quickstart/_index.md` Step 1
(`xattr -d com.apple.quarantine ./joe`) because the released archives are unsigned —
`.goreleaser.yaml` carries no `signs:` block (per CLAUDE.md's distribution-posture
invariant and the corresponding `docs/project/SITE-CLAIMS.md` distribution-posture
entry). The workaround documents the symptom; this item is the actual fix: sign and
notarize the macOS build outputs so a downloaded release binary runs without the
reader needing to know about quarantine attributes at all.

Likely route, to be verified when picked up: goreleaser has built-in notarization
support (`notarize:` / the `gon`/`quill` integration path, exact mechanism to
re-check against the goreleaser version in use) that can sign and notarize the
`darwin` build outputs as part of the existing `goreleaser release --clean` pipeline.
Requires an Apple Developer ID certificate and notarization credentials to be
provisioned in CI secrets — a standing cost this item should weigh against the
xattr workaround's one-line cost before committing to it.

If adopted, this changes the distribution-posture claim in `docs/project/SITE-CLAIMS.md`
(currently "no signing") and the corresponding CLAUDE.md line — both need a revision in
the session that lands this, per the register's bidirectional maintenance duty. The
Quickstart's xattr note should be removed or scoped to "if you built from source"
once signed archives ship.
