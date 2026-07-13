# Releasing Joe

Operator runbook for cutting a Joe release. Not a public doc (barred from
`docs/public` by D-0052 — `docs/reference`/repo-root procedure docs stay
internal), and not a skill: the load-bearing step (pushing the tag) is
irreversible and must be a human decision, not an agent action. Follow this
as a checklist.

## What actually publishes a release

A release publishes when, and only when, an operator pushes a `v`-prefixed
semver tag (e.g. `v0.1.0`) to `origin`. That push is what
`.github/workflows/release.yml` reacts to
([.github/workflows/release.yml:9-11](.github/workflows/release.yml#L9-L11));
the workflow then runs `goreleaser release --clean`
([.github/workflows/release.yml:40-44](.github/workflows/release.yml#L40-L44)),
which builds the archives, stages the real web UI via `.goreleaser.yaml`'s
`before.hooks` ([.goreleaser.yaml:17-24](.goreleaser.yaml#L17-L24)), and
publishes a GitHub Release.

**The operator owns the tag, not CI.** Nothing in this repo tags or pushes
tags automatically. The tag push is the single irreversible trigger for a
public, consumable artifact — that decision belongs to a human, made
deliberately, on a commit the operator has already verified.

**Honesty note.** As of this writing, `release.yml` has never fired — its own
file history is a single commit, and no tag exists in this repository
(`git tag -l` is empty). `v0.1.0` will be its first-ever invocation. Treat
this runbook as a pre-flight checklist, not a description of proven behavior.
Correct it against what actually happens on that first run (see the closing
section).

## Pre-tag checklist

Before tagging anything, confirm all of the following on the exact commit SHA
you intend to release:

- [ ] **CI is green on that SHA**, specifically the `goreleaser-build` job in
      `.github/workflows/tests.yml`
      ([.github/workflows/tests.yml:169-196](.github/workflows/tests.yml#L169-L196)).
      This job runs the same `.goreleaser.yaml` config in snapshot mode and its
      guard step boots the resulting binary, reads `GET /api/v1/version`, and
      checks `ui_digest` against an independently computed digest over the
      staged UI — and against a placeholder-only digest
      ([.github/workflows/tests.yml:198-263](.github/workflows/tests.yml#L198-L263)).
      A green run here is the concrete signal that the release path — UI
      staging included — is proven on this SHA. It is not a live rehearsal of
      `goreleaser release` itself (that command only ever runs from the
      tag-triggered workflow), but it is the closest verified proxy this repo
      has.
- [ ] **Distribution-posture docs are swept to the release-exists state.**
      This is the `release-pipeline-02` doc sweep tracked at
      [docs/backlog/release-pipeline.md](backlog/release-pipeline.md) — see
      its "Update-at-tag-time sites" list for the exact files. Do not
      duplicate that list here; confirm the sweep commit has landed before
      tagging.
- [ ] **A `DECISIONS.md` entry for this tag cut is present** in
      `docs/project/DECISIONS.md`, recording the decision to cut this
      version.
- [ ] **Working tree is clean and checked out on `origin/main`** at the
      commit you intend to tag (`git status` shows nothing pending;
      `git fetch && git log origin/main -1` matches your `HEAD`).

## Cutting the tag

Once every box above is checked, tag the exact commit SHA (not just whatever
`HEAD` happens to be — name it explicitly) and push the tag. This generalizes
the same-SHA procedure `release-pipeline-02` already records
([docs/backlog/release-pipeline.md:15-24](backlog/release-pipeline.md#L15-L24)):
the doc-sweep commit and the tag must be the same commit, so the repo is
never in a state where its own docs claim a release exists before one
actually does.

```sh
# Confirm you're tagging the commit you think you are.
git log -1 <sha>

# Annotated tag, semver, v-prefixed.
git tag -a v0.1.0 <sha> -m "v0.1.0"

# Push the tag. This is the irreversible publish trigger.
git push origin v0.1.0
```

## What happens after the push

The tag push triggers the `Release` workflow
([.github/workflows/release.yml:9-11](.github/workflows/release.yml#L9-L11)),
which checks out full history (`fetch-depth: 0`, needed for `git describe`
and changelog generation), sets up Go 1.25 and Node 22, and runs
`goreleaser/goreleaser-action@v7` with `args: release --clean`, pinned to
goreleaser `~> v2`
([.github/workflows/release.yml:20-44](.github/workflows/release.yml#L20-L44)).

The actual build matrix, archive format, and checksum naming are defined in
`.goreleaser.yaml` — read that file for the current `builds.goos`/`goarch`
list and `checksum.name_template`
([.goreleaser.yaml:26-56](.goreleaser.yaml#L26-L56)) rather than trusting a
restated copy here, which would go stale the next time the matrix changes
(D-0032).

Watch the run under the repository's Actions tab, filtered to the `Release`
workflow, for the run triggered by your tag push.

## Go/no-go verification on the published release

After the workflow completes, confirm all of the following before treating
the release as done:

- [ ] A GitHub Release exists for the tag, with archives for each
      `builds.goos`/`goarch` combination in `.goreleaser.yaml` and a
      `checksums.txt` file (`checksum.name_template`,
      [.goreleaser.yaml:55-56](.goreleaser.yaml#L55-L56)).
- [ ] Release notes were generated (goreleaser's default commit-log
      changelog — no `changelog:` block is configured, so this is
      goreleaser's stock behavior, not custom copy).
- [ ] **Released-binary integrity check:** download one of the published
      archives, extract the `joe` binary, boot it, and call
      `GET /api/v1/version`. Confirm:
  - `ui_digest` is present and non-empty.
  - It matches `buildinfo.Compute` run independently over the UI dist tree
    the release was built from — use `go run ./scripts/verify-ui-digest <dist-dir>`
    ([scripts/verify-ui-digest/main.go:1-24](../scripts/verify-ui-digest/main.go#L1-L24)),
    the same helper CI's `goreleaser-build` guard uses.
  - It does **not** match the placeholder digest (the digest computed over a
    directory containing only `internal/webui/dist/.gitkeep`).
  - `version` and `commit` reported match the tag and SHA you released.

If any of these fail, treat the release as broken — see rollback below.

## Rollback / failure handling

- **Workflow fails mid-publish** (e.g. `goreleaser release` errors out).
  UNVERIFIED operator judgment, not asserted GoReleaser/GitHub behavior: a
  failed run may leave a partial or no GitHub Release depending on how far
  it got. Check the repository's Releases page directly rather than
  assuming either outcome. If a partial/draft Release was created, delete it
  from the GitHub UI before retrying.
- **Tag was pushed prematurely** (e.g. pre-tag checklist was skipped or a
  box was wrongly checked). At minimum:
  ```sh
  git push --delete origin v0.1.0
  git tag -d v0.1.0
  ```
  Then delete the corresponding draft or published GitHub Release from the
  Releases page (UNVERIFIED whether goreleaser auto-creates it as a draft or
  published state on partial failure — confirm in the UI, don't assume).
- **A bad release published successfully** (e.g. the integrity check above
  fails, or a real defect ships). Do not silently delete a release that may
  already have been downloaded/consumed — superseding it with a new patch
  tag (e.g. `v0.1.1`) is the safer default once anyone could plausibly have
  pulled the bad one. Deleting and force-replacing a tag that's already been
  fetched by a third party can desync their view of history; treat deletion
  as a last resort for a release you're confident nobody has consumed yet.

## Correct this after the first real release

This runbook was written before `release.yml` had ever run. Once `v0.1.0`
actually publishes, revisit:

- Whether the `goreleaser-build` CI guard was in fact a faithful proxy for
  the real `release` run, or whether `goreleaser release` surfaced any
  behavior (auth, changelog content, asset naming) the snapshot job didn't.
- The rollback section's UNVERIFIED claims about GitHub's/goreleaser's
  actual behavior on partial failure — replace with what was actually
  observed.
- Timing: how long the workflow actually took, so "watch the run" guidance
  can be more concrete.
- Whether the go/no-go verification steps above were sufficient, or missed
  something only visible on a real published release.
