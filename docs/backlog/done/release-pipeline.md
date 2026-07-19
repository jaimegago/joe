Release pipeline — tag cut and distribution-posture doc sweep (-02)
Status: in-progress

`release-pipeline-01` armed the publish pipeline (D-0091): `.goreleaser.yaml`
publishes a GitHub Release on a `v`-prefixed tag push via
`.github/workflows/release.yml`; UI staging is guaranteed by a goreleaser
`before.hooks` entry (`scripts/stage-ui-for-release.sh`) so every goreleaser
invocation — release, local build, and the CI snapshot job — embeds the real
UI; the CI snapshot job (`goreleaser-build` in `.github/workflows/tests.yml`)
proves this with a `ui_digest`-based guard step.

`release-pipeline-02` has landed the **distribution-posture doc sweep**
(D-0122). Every copy site that was true only until a release existed has been
rewritten to the release-exists state, on the commit the operator tags
`v0.1.0`. **The doc sweep is done. What remains is post-tag.**

## Tag-cut procedure (operator-performed, per `release-pipeline-01`'s reserved
## decision on the two-session split)

1. ~~`-02` lands its doc-sweep commit on `main`.~~ Done (D-0122).
2. ~~Push that commit to `origin/main`.~~ Done.
3. The operator tags that exact commit SHA `v0.1.0` and pushes the tag, per
   the checklist in `docs/RELEASING.md`:
   `git tag -a v0.1.0 <sha> -m "v0.1.0" && git push origin v0.1.0`.
4. The tag push triggers `.github/workflows/release.yml`, which runs
   `goreleaser release --clean` and publishes the GitHub Release.
5. In the joeagent.dev repo, re-seed the published docs from a joe checkout at
   the tag — `./scripts/sync-docs.sh --seed-from <joe checkout at v0.1.0>` — and
   push. This flips the published doc footer to `v0.1.0` (D-0121); the tag push
   alone does not.

Steps 3–5 are the operator's and are why this item is not yet done.

## Doc sweep — complete (D-0122)

Swept to the release-exists state, all on one commit:

- `README.md` — the License-heading distribution statement.
- `docs/public/install-and-build/_index.md` — restructured download-first, with
  build-from-source retained as a first-class peer; prerequisites split so the
  download path is not gated on Go/Node/git; the "Why nothing is published yet"
  section deleted outright; front matter updated. Also **removed the unfounded
  claim that the pipeline publishes *signed* archives** — `.goreleaser.yaml` has
  no `signs:` block, so that claim would have stayed false even after `v0.1.0`
  published. A tree-wide sweep confirmed this page was its only site.
- `docs/backlog/unauth-health-surface.md` — the reconnaissance-surface
  parenthetical that leaned on "build-from-source".
- `docs/project/SITE-CLAIMS.md` — the Install and Build / Distribution entry;
  its launch-bound binding note is discharged and replaced with a two-direction
  mechanism binding (D-0077/D-0088).
- `docs/public/_index.md` and `docs/public/overview/_index.md` — two inbound
  blurbs that narrowed the page to source-only.
- `docs/public/quickstart/_index.md` — minimal scope: the false "there are no
  release downloads" sentence removed, prerequisites reframed as build-path
  requirements. Deliberately **not** restructured; see the open question below.
- `CLAUDE.md` and `.goreleaser.yaml`'s header comment — both asserted
  build-from-source-only distribution.

## Open: should Quickstart lead with the download? — question, not a decision

`-02` deliberately left Quickstart on its build-from-source path and only
corrected what had become false. Worth deciding post-tag: **should the guided
first run lead with downloading a released binary instead of `make build`?**

The case for: the download path needs neither a Go toolchain nor Node, which
are currently three of Quickstart's prerequisites, and it reaches a running
daemon in fewer steps — a better on-rails first experience for a reader who
wants one answer, not a build.

The case against, and what to weigh: Quickstart is the on-rails path for a
reader evaluating Joe, who may well want the source tree anyway; and the guide
currently assumes a repository checkout in later steps. Flipping it is a
restructure, not an edit.

**This is recorded as a question to decide, not a decided change.** Resolve it
deliberately; do not treat it as implied by the tag cut.

## Operator runbook and first-run corrections — survives the tag

The operator release procedure is captured at `docs/RELEASING.md` (session
`releasing-runbook`, D-0092). The runbook's "Correct this after the first real
release" section (bottom of that file) owns the first-run correction obligation
for this tag cut: once `v0.1.0` publishes, revisit that section to replace
UNVERIFIED claims about GitHub/goreleaser behavior with what was observed. Its
"Honesty note" (`release.yml` has never fired) is also correct only until the
tag lands, and is corrected in that same pass.

## Doc-footer version stamping — built (D-0121), not a `-02` obligation

Doc-footer version stamping is **built and live** as seed-time stamping in the
joeagent.dev repo (D-0121), which discharged D-0052's deferral outright. It is
not deferred, not gated on the first release, and nothing about it is evaluated
or built here. Its only bearing on this item is the tag-time re-seed at step 5
above.

## Out of scope for `-02`

Homebrew/Scoop taps, `install.sh`, and binary signing were not decided or
built by `release-pipeline-01` and are not implied by the tag cut — the
pipeline publishes raw archives + checksums only. A future decision to add any
of these is its own posture item, not folded into the tag-cut session. The
published docs and the SITE-CLAIMS entry now state these absences affirmatively,
so adding any of them obliges a copy revision.

## What closes this item

All three of: the operator has pushed the `v0.1.0` tag and the release
published (steps 3–4); the joeagent.dev re-seed has flipped the published
footer (step 5); and `docs/RELEASING.md`'s first-run correction pass has run
against what was actually observed. The Quickstart question above can be split
to its own item rather than blocking closure.
