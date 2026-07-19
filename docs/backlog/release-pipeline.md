Release pipeline — tag cut and distribution-posture doc sweep (-02)
Status: in-progress

`release-pipeline-01` armed the publish pipeline (D-0091): `.goreleaser.yaml`
publishes a GitHub Release on a `v`-prefixed tag push via the new
`.github/workflows/release.yml`; UI staging is guaranteed by a goreleaser
`before.hooks` entry (`scripts/stage-ui-for-release.sh`) so every goreleaser
invocation — release, local build, and the CI snapshot job — embeds the real
UI; the CI snapshot job (`goreleaser-build` in `.github/workflows/tests.yml`)
now proves this with a `ui_digest`-based guard step. No tag has been pushed and
no release has published. This file tracks the remaining `-02` work: cutting
the `v0.1.0` tag and sweeping the copy sites that stay true only until that tag
lands.

## Tag-cut procedure (operator-performed, per `release-pipeline-01`'s reserved
## decision on the two-session split)

1. `-02` lands its doc-sweep commit on `main` (the update-at-tag-time sites
   below, rewritten to the post-launch state).
2. Push that commit to `origin/main`.
3. The operator tags that exact commit SHA `v0.1.0` and pushes the tag:
   `git tag v0.1.0 <sha> && git push origin v0.1.0`.
4. The tag push triggers `.github/workflows/release.yml`, which runs
   `goreleaser release --clean` and publishes the GitHub Release.

## Update-at-tag-time sites (stay true until the tag; `-02` rewrites these to
## reflect a published release existing)

- `README.md:210` — "It is distributed build-from-source only; there are no
  published release binaries."
- `docs/public/install-and-build/_index.md` — the "there are no published
  release binaries" factual clause (the surrounding "deliberately configured
  not to publish" framing was already corrected in `release-pipeline-01`); also
  revise the "Why nothing is published" section heading/body once a release
  exists.
- `docs/backlog/unauth-health-surface.md:118-122` — the reconnaissance-surface
  argument that leans on "Joe is Apache-2.0, build-from-source."
- `docs/project/SITE-CLAIMS.md` — the Install and Build / Distribution entry
  added in `release-pipeline-01` is launch-bound; revise its claim text once
  `v0.1.0` publishes (per the D-0077/D-0088 bidirectional register obligation).

## Doc-footer version stamping — still deferred (D-0052)

Doc-footer version stamping on published pages stays deferred. Re-open
condition (unchanged): the first post-launch release. `-02`'s tag-cut is that
release, so this becomes actionable once `v0.1.0` publishes — evaluate then,
don't build it speculatively now.

## Operator runbook and first-run corrections

The operator release procedure is captured at `docs/RELEASING.md` (session
`releasing-runbook`, D-0092). The runbook's "Correct this after the first real
release" section (bottom of that file) owns the first-run correction obligation
for this tag cut: once `v0.1.0` publishes, revisit that section to replace
UNVERIFIED claims about GitHub/goreleaser behavior with what was observed.

## Out of scope for `-02`

Homebrew/Scoop taps, `install.sh`, and binary signing were not decided or
built by `release-pipeline-01` and are not implied by the tag cut — the
pipeline publishes raw archives + checksums only. A future decision to add any
of these is its own posture item, not folded into the tag-cut session.
