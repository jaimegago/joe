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

**Honesty note.** `release.yml` has fired once: the `v0.1.0` tag push
(commit fa8e3ba) triggered run 29697892534, which succeeded. Its behavior on
that run is recorded below and in the closing section; everything else in
this runbook — repeat runs, partial-failure behavior, multi-release
sequencing — remains a pre-flight checklist, not proven behavior.

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
workflow, for the run triggered by your tag push. **Observed timing (run 1,
`v0.1.0`):** total run 9m27s; the `GoReleaser Release` job itself was 9m22s
of that. Treat this as one data point, not a guaranteed bound.

**Observed run-1 annotation.** The run carried one warning-level annotation,
not a failure: GitHub's runner reported that `actions/checkout@v4`,
`actions/setup-go@v5`, and `actions/setup-node@v4` each internally target the
now-deprecated Node.js 20 and were force-upgraded to Node.js 24 by the
runner. This is unrelated to the workflow's own `actions/setup-node` step,
which requests Node 22 for the workflow's script steps — the warning is about
those three actions' own internal implementation, not the workflow's
requested runtime. Harmless as observed (the run succeeded); revisit if a
future run's annotation set changes shape.

## Go/no-go verification on the published release

After the workflow completes, confirm all of the following before treating
the release as done:

- [ ] A GitHub Release exists for the tag, with archives for each
      `builds.goos`/`goarch` combination in `.goreleaser.yaml` and a
      `checksums.txt` file (`checksum.name_template`,
      [.goreleaser.yaml:55-56](.goreleaser.yaml#L55-L56)). **Observed on run 1
      (`v0.1.0`, historical, not a restated spec — `.goreleaser.yaml` is
      current truth per D-0032):** four archives named
      `joe_0.1.0_<os>_<arch>.tar.gz` (darwin/amd64, darwin/arm64,
      linux/amd64, linux/arm64), each containing the `joe` binary plus
      `LICENSE` and `README.md`; `checksums.txt` named all four and verified
      clean with `shasum -a 256 --check` (four `OK` lines).
- [ ] Release notes were generated (goreleaser's default commit-log
      changelog — no `changelog:` block is configured, so this is
      goreleaser's stock behavior, not custom copy). **Observed on run 1:**
      because no prior tag existed, the changelog enumerated the entire
      commit history back to the repository root, not just commits since a
      previous release. Expected stock behavior for a first release, not a
      defect — but note for future releases the changelog will instead scope
      to commits since the prior tag. Whether to configure changelog
      trimming (e.g. excluding certain commit prefixes) for future releases
      is an open consideration, not decided here.
- [ ] **Released-binary integrity check:** download one of the published
      archives, extract the `joe` binary, boot it, and call
      `GET /api/v1/version`. Confirm:
  - `ui_digest` is present and non-empty.
  - It matches `buildinfo.Compute` run independently over the UI dist tree
    the release was built from — use `go run ./scripts/verify-ui-digest <dist-dir>`
    ([scripts/verify-ui-digest/main.go:1-24](../scripts/verify-ui-digest/main.go#L1-L24)),
    the same helper CI's `goreleaser-build` guard uses.
  - It does **not** match the placeholder digest (the digest computed over a
    directory containing only `internal/webui/dist/.gitkeep`). On run 1 the
    placeholder-mismatch half was discharged transitively rather than
    re-run standalone: CI's `goreleaser-build` guard already proves it on
    the same SHA (pre-tag checklist item above), so the operator's own check
    was the positive match only.
  - `version` and `commit` reported match the tag and SHA you released.
  - **Credential prerequisite (undocumented until run 1):** if the operator's
    config has auth enabled — the expected posture for a real install —
    `GET /api/v1/version` requires an authenticated request. The operator
    needs a live session against the configured identity provider (dex, in
    the run-1 environment) before this call will succeed; a bare `curl`
    against an auth-enabled instance returns a 401, not the version payload.
    Plan for this before starting the integrity check, not after hitting the
    401.
  - **Isolation caution.** Booting the release binary with an overridden
    `HOME` environment variable does **not** isolate it from the operator's
    real state: on run 1, doing this still resolved the operator's actual
    `~/.joe` (database, encryption key, registered components, session
    archive) and ran live refreshers against real data. Do not assume a
    `HOME` override is sufficient sandboxing for this check. How `joe`
    actually resolves its home directory is an open investigation
    ([`docs/backlog/joe-home-resolution.md`](backlog/joe-home-resolution.md)),
    not settled here — until it is, treat every integrity-check boot as
    touching real state and plan the check accordingly (e.g. a disposable
    machine or container, not just an env var).
  - **Run-1 result:** `version` 0.1.0, `commit` fa8e3baaded73cae4cdf7043d89235970b3ca0eb,
    `ui_digest` ffc3ec0c8328b1ed735e837d81e03accb6a9e955e434770790bcf9d8bed46e3e —
    matched an independent `scripts/verify-ui-digest` run over
    `internal/webui/dist` staged at the tag via
    `scripts/stage-ui-for-release.sh`.

If any of these fail, treat the release as broken — see rollback below.

## Rollback / failure handling

One real run exists (`v0.1.0`, run 29697892534) and it succeeded end to end
— nothing below about failure or partial-failure handling was exercised by
it. These claims remain UNVERIFIED operator judgment, not observed behavior;
do not treat run 1's clean success as evidence either way about how a failed
run behaves.

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

The first-run correction pass has run (session `release-pipeline-03`,
D-0123), against `v0.1.0` run 29697892534 (success). What it found is folded
into the sections above: the Honesty note, run timing, the Node.js 20
annotation, release-notes/changelog behavior, observed archive/checksum
defaults, the auth prerequisite on the version-endpoint check, and the
`HOME`-override isolation caution.

What remains unverified after this pass, because run 1 succeeded and
exercised none of it:

- The rollback section's claims about GitHub's/goreleaser's actual behavior
  on partial or mid-publish failure — still UNVERIFIED operator judgment,
  not observed. A future failed run is what would settle these.
- Whether the `goreleaser-build` CI guard remains a faithful proxy for the
  real `release` run on a *second* release — run 1 is one data point; a
  second tag cut may surface behavior (e.g. changelog scoping now that a
  prior tag exists) the snapshot job still doesn't model.
- How `joe` actually resolves its home directory — opened as its own
  investigation, [`docs/backlog/joe-home-resolution.md`](backlog/joe-home-resolution.md),
  not settled here.
- Whether changelog trimming is worth configuring for future releases, given
  run 1's full-history changelog — recorded as an open consideration above,
  not decided.
