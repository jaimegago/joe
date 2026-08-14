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

**Honesty note.** `release.yml` has fired twice, both successes: the `v0.1.0`
tag push (commit fa8e3ba) triggered run 29697892534, and the `v0.2.0` tag push
(commit 0dbcbb9) triggered run 29861956252. Their behavior is recorded below
and in the closing section, and the two agree on every observable — timing,
annotations, archive shape, digest verification. Repeat runs and multi-release
sequencing are therefore now observed rather than projected.
**Partial-failure behavior is not**: two clean runs say nothing about how a
failed one behaves, and the rollback section remains a pre-flight judgment
call rather than proven behavior.

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
      [docs/backlog/done/release-pipeline.md](backlog/done/release-pipeline.md) — see
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
([docs/backlog/done/release-pipeline.md:15-24](backlog/done/release-pipeline.md#L15-L24)):
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
workflow, for the run triggered by your tag push. **Observed timing:** run 1
(`v0.1.0`, run 29697892534) total 9m27s, `GoReleaser Release` job 9m22s; run 2
(`v0.2.0`, run 29861956252) total 9m37s, job 9m33s. Two data points ten
seconds apart is a stable ~9.5m, not a guaranteed bound.

**Observed annotation, both runs.** Each run carried exactly one
warning-level annotation, not a failure, and the two are identical: GitHub's
runner reported that `actions/checkout@v4`, `actions/setup-go@v5`, and
`actions/setup-node@v4` each internally target the now-deprecated Node.js 20
and were force-upgraded to Node.js 24 by the runner. This is unrelated to the
workflow's own `actions/setup-node` step, which requests Node 22 for the
workflow's script steps — the warning is about those three actions' own
internal implementation, not the workflow's requested runtime. Harmless as
observed (both runs succeeded); revisit if a future run's annotation set
changes shape. Note the release workflow's annotation set is narrower than
the `Tests` workflow's on the same SHA, which additionally carried
`actions/cache@v4` and `codecov/codecov-action@v4` in the same warning plus
unrelated cache-restore noise — do not expect the two workflows' annotations
to match.

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
      clean with `shasum -a 256 --check` (four `OK` lines). **Run 2 (`v0.2.0`)
      reproduced this shape exactly** — same four platform pairs, same
      `joe_<version>_<os>_<arch>.tar.gz` naming, same three members per
      archive (`joe`, `LICENSE`, `README.md`), `checksums.txt` naming all four
      and checking clean.
- [ ] Release notes were generated (goreleaser's default commit-log
      changelog — no `changelog:` block is configured, so this is
      goreleaser's stock behavior, not custom copy). **Observed on run 1:**
      because no prior tag existed, the changelog enumerated the entire
      commit history back to the repository root. **Run 2 settles what run 1
      could only predict:** with a prior tag present, goreleaser scoped the
      changelog to exactly `v0.1.0..v0.2.0` — 20 entries, one line per commit
      as `* <full-sha> <subject>`, newest first, nothing from before the prior
      tag. Changelog bounding works with no configuration. Whether to
      configure changelog *trimming* (e.g. excluding certain commit prefixes)
      remains an open consideration, not decided here; run 2's list is legible
      but flat — because this repo's commit subjects are session slugs rather
      than Conventional Commit types, there is no feat/fix grouping, so one
      user-facing change and a dozen documentation commits render alike.
- [ ] **If the release carries a behaviour change the changelog states only
      implicitly, append a note to the release body after publishing.** The
      generated changelog is commit subjects and nothing else, so a change
      whose subject line states a new rule without naming what moved will not
      be legible to a consumer. Publish first, then
      `gh release edit <tag> --notes-file <file>`. Established by D-0137 for
      `v0.2.0`'s skills-family exit-code change; a permanent
      `release.header` in `.goreleaser.yaml` was rejected there as a standing
      cost carried for a one-off note.
      **Trap, hit on run 2:** `gh release edit --notes-file` **replaces** the
      body outright, so the append is read-modify-write, and `gh release view`
      needs a cwd inside the repository or it fails with
      `not a git repository`. Chaining the read and the append with `&&` after
      a `cd` into a scratch directory silently produced an empty file and the
      subsequent edit **wiped the generated changelog**. It is recoverable —
      `printf '## Changelog\n' && git log --format='* %H %s' <prev>..<tag>`
      reproduces goreleaser's default body byte-for-byte — but verify the file
      is non-empty before passing it to `--notes-file`.
- [ ] **Released-binary integrity check:** download one of the published
      archives, extract the `joe` binary, boot it, and call
      `GET /api/v1/version`. Confirm:
  - `ui_digest` is present and non-empty.
  - It matches `buildinfo.Compute` run independently over the UI dist tree
    the release was built from — use `go run ./scripts/verify-ui-digest <dist-dir>`
    ([scripts/verify-ui-digest/main.go:1-24](../scripts/verify-ui-digest/main.go#L1-L24)),
    the same helper CI's `goreleaser-build` guard uses.
  - **A digest identical to a previous release's is not a defect.** `ui_digest`
    is content-addressed over the embedded UI bytes, so two releases whose UI
    source did not change **must** produce the same digest — a repeat is
    evidence the embed is correct, not evidence it is stale. Check
    `git diff --stat <prev-tag>..HEAD -- ui/ internal/webui/` before reading
    anything into a repeat: empty means expect equality. First observed on
    `v0.2.0`, whose range carried no UI change and which therefore reproduced
    `v0.1.0`'s digest exactly. The check that actually catches a stale or
    placeholder embed is the pair below (match the independently computed
    staged digest; differ from the placeholder), not novelty against history.
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
    ([`docs/backlog/joe-home-resolution.md`](backlog/done/joe-home-resolution.md)),
    not settled here — until it is, treat every integrity-check boot as
    touching real state and plan the check accordingly (e.g. a disposable
    machine or container, not just an env var).
    **Run 2 took the container route and it works — prefer it.** Extract the
    `linux/<arch>` archive, mount only the extracted directory, and boot the
    binary inside a throwaway container
    (`docker run --rm -v "$PWD":/work -w /work alpine:3 ...`) with a config
    whose `database.dsn` points inside the mount. The binary resolved
    `/root/.joe` *within the container* for the key, skills, and session
    archive, and the operator's real `~/.joe` was structurally unreachable
    rather than merely un-targeted. This sidesteps the open home-resolution
    question entirely instead of depending on its answer.
    **Config trap, hit on run 2:** do not both set `JOE_API_KEY` and declare a
    `server.service_accounts` entry carrying the same key — boot fails with
    `service account "server": key already used by "svc:<name>"`. Use one or
    the other; the CI guard's shape (`JOE_API_KEY` alone, no
    `service_accounts` block) is the simpler one for this check.
  - **Run-1 result:** `version` 0.1.0, `commit` fa8e3baaded73cae4cdf7043d89235970b3ca0eb,
    `ui_digest` ffc3ec0c8328b1ed735e837d81e03accb6a9e955e434770790bcf9d8bed46e3e —
    matched an independent `scripts/verify-ui-digest` run over
    `internal/webui/dist` staged at the tag via
    `scripts/stage-ui-for-release.sh`.
  - **Run-2 result:** `version` 0.2.0, `commit` 0dbcbb9 (the short form, matching
    `.goreleaser.yaml`'s `{{ .ShortCommit }}` injection — run 1's entry above
    records a full SHA because that is what the operator compared against, not
    because the field's shape changed), `ui_digest`
    ffc3ec0c8328b1ed735e837d81e03accb6a9e955e434770790bcf9d8bed46e3e —
    equal to `v0.1.0`'s, correctly, per the content-addressing note above; it
    matched both the local pre-tag `scripts/verify-ui-digest` run and the CI
    guard's independently computed value on the tagged SHA, and differed from
    the placeholder 2eeaca4cbbd1fe5f6d692e0e0a3d1196650b481ac1e5b5f7757544222ccaf023.
    The 401-without-credentials half of the auth prerequisite was observed
    directly this time: an unauthenticated `curl` to `/api/v1/version` returned
    401 while the same call with `Authorization: Bearer` returned the payload.
- [ ] **Confirm the released binary carries what the published documentation
      instructs.** Run this against the artifact, not the tree — the tree
      cannot catch it. Before `v0.2.0` the published Quickstart instructed
      `joe admin bootstrap`, which existed in `main` but not in the latest
      *released* binary, so a reader following the current docs against the
      current download hit a command that did not exist, at a step with no
      alternative path. Invoke only far enough to confirm presence and usage
      output — never far enough to perform the action. A no-argument
      invocation (usage error, exit 2) and an explicitly-named missing
      `--config` path (exit 1, message naming the flag) both prove presence
      while doing no work, since a usage error is decided before any config
      is loaded and a config failure before any store is opened.
      **Run 2 confirmed** on the `v0.2.0` linux/arm64 binary: `admin bootstrap`
      present with the documented usage line; `--config` present and
      exit-1-on-missing-path across `panic`, `unlock`, `skills list`,
      `incident status`, `db backup`, `db restore`, and `admin bootstrap`;
      `--config` correctly *rejected* by `mcp` and `slack` (exit 2,
      `flag provided but not defined`) per D-0132's deliberate withholding;
      and the skills family exiting 2 on both a surplus positional and a
      missing one, matching the release note's claim.

If any of these fail, treat the release as broken — see rollback below.

## Rollback / failure handling

Two real runs exist (`v0.1.0` run 29697892534, `v0.2.0` run 29861956252) and
**both succeeded end to end** — nothing below about failure or partial-failure
handling was exercised by either. These claims remain UNVERIFIED operator
judgment, not observed behavior. **Two clean runs are still zero observations
of failure handling**, and the second success is not evidence about the first
failure; do not let a growing run count erode these tags. Only a genuinely
failed run settles them.

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

## Correction passes

Two have run. The **first-run pass** (session `release-pipeline-03`, D-0123)
against `v0.1.0` run 29697892534 folded in the Honesty note, run timing, the
Node.js 20 annotation, release-notes/changelog behavior, observed
archive/checksum defaults, the auth prerequisite on the version-endpoint
check, and the `HOME`-override isolation caution. The **second-run pass**
(session `release-v0.2.0`, D-0137) against `v0.2.0` run 29861956252 added run-2
timing beside run-1's, the confirmation that changelog bounding works with no
configuration, the container-isolation route for the integrity check, the
`ui_digest`-repeats-legitimately note, the artifact-versus-tree documentation
check, and two traps it hit directly (the `gh release edit --notes-file`
whole-body replacement, and the `JOE_API_KEY`/`service_accounts` key
collision). It also fixed two dead links into `backlog/release-pipeline.md`,
which had closed and moved to `backlog/done/`.

What remains unverified after both passes:

- The rollback section's claims about GitHub's/goreleaser's actual behavior
  on partial or mid-publish failure — still UNVERIFIED operator judgment, not
  observed. Both runs succeeded, so the count of clean runs has grown while
  the evidence about failure has not moved at all. A future failed run is
  what would settle these, and nothing else will.
- How `joe` actually resolves its home directory —
  [`docs/backlog/joe-home-resolution.md`](backlog/done/joe-home-resolution.md),
  still open. Run 2 routed around it with a container rather than answering
  it; the question is untouched.
- Whether changelog trimming is worth configuring — now a sharper question
  than after run 1, since bounding is confirmed working and the residual
  complaint is only that session-slug subjects give the list no grouping.
  Still not decided.

Settled by run 2, no longer open: whether the `goreleaser-build` CI guard
remains a faithful proxy on a second release. It did — the guard's digest
triple on the tagged SHA matched the released binary's reported `ui_digest`
exactly, and changelog scoping, the one behavior run 1 flagged as unmodeled by
the snapshot job, behaved as predicted.
