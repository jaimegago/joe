# Verification Map

What a change to this repository owes in verification, by class of work. Each class carries five fields:

- **Scope** — a classifier decidable from a diff.
- **Verify** — commands that are runnable today. Runnable, not enforced: a command that works locally but no machine re-runs still belongs here.
- **CI** — whether a machine currently re-runs each command, and in which tier. This is what makes the untrusted-until-re-run rule operable.
- **Evidence** — the one line that proves the check passed, plus the command as run.
- **Gaps** — classes or commands with no automated check, and the manual criteria standing in.

## Tiers

Since `gated-main`, a machine re-running a command is not the whole answer. Where it runs decides what the run can prevent.

- **Gate** — runs before a merge, on the pull request, and aggregates into the `Test Summary` check that `refs/heads/main`'s ruleset requires. Nothing reaches `main` past a red gate, including the maintainer: the ruleset has no bypass actors. The gate answers exactly one question — is `main` broken.
- **Post-merge** — runs after a change has landed, on the push to `main` or on the nightly schedule. It catches things but prevents nothing, so red here is discovered rather than blocked.

The gate is path-filtered on pull requests, keyed to the class scopes below by the `classify` job in `.github/workflows/tests.yml`. Every other trigger — pushes to `main`, the nightly schedule, manual dispatch — runs the full unfiltered matrix. Filtering buys merge latency and nothing else, and only a pull request is waiting on it.

Filtering errs broad by rule: each pattern is wider than the class it stands for. The nightly unfiltered run is the complement, so that a misclassified diff which skipped the job that would have caught it surfaces within a day rather than at the next release.

Two surfaces are held out of hands-off merging entirely by the `Held Paths` check in `.github/workflows/held-paths.yml`, and are released only by an approving review from the maintainer on the pull request's head commit:

- **Instruction surfaces** — `docs/backlog/**`, `CLAUDE.md`, `docs/verification.md`. joe-pm's trust boundary founds its second work source on joe's `main` being maintainer-merged; a green build says nothing about whether a backlog edit is a legitimate order.
- **Governance-class paths** — `internal/rbac/**`, `internal/access/**`, `test/integration/rbac_test.go`. A broad path approximation of the `rbac` class, whose real scope is not path-decidable.

## Rules

- Work spanning classes takes the union of their requirements, with the most demanding discipline winning. Overlap is expected and is not a defect: `.goreleaser.yaml` is in both `release-packaging` and `verification-infrastructure`, and a diff touching it owes both.
- Unclassifiable work is a stop-and-ask. It never defaults to no verification.
- The map co-evolves. A diff that changes **what runs** must modify `docs/verification.md` in the same push. A diff that only touches a file in the `verification-infrastructure` path list without changing what runs — a comment, a formatting fix — does not.
- Agent-reported verification is untrusted until a machine re-runs it. The CI field states, per row, whether one does and in which tier. Rows marked no are unverified claims from the ledger's point of view, however green the session reported them.
- CI red on `main` caused by a change's push flips that work from done back to blocked. No exceptions. Since `gated-main` this reaches only the post-merge tier: a gate-tier failure cannot produce a red `main`, because it cannot produce a merge.

Class names are stable identifiers; other tooling references them by name.

## Classes

### `go-backend`

**Scope.** Any `.go` file under `cmd/`, `internal/`, or `joecored/` carrying no build tag, plus `go.mod` and `go.sum`.

**Verify.** `make vet` (`Makefile:124`); `gofmt -s -l .` returning empty (`.github/workflows/tests.yml:247-253`); `make test-unit` (`Makefile:66`); golangci-lint v2.12.2 (`.github/workflows/tests.yml:255-259`).

**CI.** **Gate**, all four, via the `unit-tests` and `lint` jobs. Both are pulled in by the `go` filter, which matches any `.go` file anywhere, `go.mod`, `go.sum`, `Makefile`, `.golangci.yml`, or `.github/workflows/**`.

**Evidence.** The final pass/fail line of `make test-unit` and the empty output of the gofmt check, each with the command as run.

**Gaps.** golangci-lint runs with only `ineffassign`, `govet`, and `unused` enabled; `errcheck` and `staticcheck` are explicitly disabled (`.golangci.yml:15-17`). A green lint does not mean unchecked errors are absent. golangci-lint is additionally a heavy-tier command for map-authoring purposes: its presence in CI is cited above, but it was not executed when this map was written — CI pins v2.12.2 through the action, so a differently-versioned local binary produces a non-comparable result and proves nothing about the gate.

### `go-tagged-tests`

**Scope.** Any file under `test/integration/` or `test/e2e/`, or any `.go` file carrying `//go:build integration` or `//go:build e2e`.

**Verify.** `make vet-tagged` (`Makefile:131-133`); `make test-integration` (`Makefile:71`); `make test-e2e` (`Makefile:75`, which depends on `make build`).

**CI.** **Gate**, all three. `make test-integration` and `make vet-tagged` run under the `go` filter (`.github/workflows/tests.yml:154`, `:245`); `make test-e2e` runs under the `e2e` filter, which is the union of `go` and `frontend` because the job boots `cmd/joe` and builds `ui/` (`:198`).

**Evidence.** The per-suite pass line for each command run.

**Gaps.** A bare `go test ./...` or `go vet ./...` never compiles either tree, so default commands passing says nothing about these files — the comment at `Makefile:126-130` records this as a real incident, citing D-0118. `make vet-tagged` was the check standing behind that incident with no machine running it; `gated-main` folded it into the `lint` job, and it is now gate-tier. `test/e2e/` is a single test file plus its harness, with no matrix. CI e2e runs against placeholder keys falling back to the literal `test-key` (`.github/workflows/tests.yml:164-165`), so real-provider paths are unexercised. `make test-e2e` is heavy tier: its presence is cited above, but it was not executed when this map was written — it depends on `make build`, which runs `npm ci` (network) and spawns real binaries.

### `frontend`

**Scope.** Anything under `ui/` other than its build and test configuration, which belongs to `verification-infrastructure`.

**Verify.** `npm run build` (`ui/package.json:8`); `npm run lint` (`:9`); `npm test` (`:11`).

**CI.** **Gate**, all three. `npm run lint` and `npm test -- --run` run in the `frontend-checks` job (`.github/workflows/tests.yml:284`, `:289`); `npm run build` runs as `npm ci && npm run build` via `make build-ui` (`Makefile:52`) inside the `e2e-tests` job. `frontend-checks` is pulled in by the `frontend` filter, which matches the whole `ui/` subtree.

**Evidence.** The build success line, the eslint summary, and the vitest summary, each with the command as run.

**Gaps.** `npm run format` is `prettier --write` (`ui/package.json:10`) — it mutates rather than failing, so it cannot gate anything and is not listed under Verify. Until `gated-main`, build was the only one of the three any machine ran, so `ui/` had no lint or unit coverage at all; the `frontend-checks` job closed that. `npm test` is `vitest` with no subcommand (`ui/package.json:11`), which enters watch mode outside CI — the job passes `--run` explicitly rather than relying on vitest inferring the environment.

### `release-packaging`

**Scope.** `.goreleaser.yaml`, the embedded UI asset path under `internal/webui/dist`, `scripts/stage-ui-for-release.sh`, and `scripts/verify-ui-digest`.

**Verify.** `goreleaser build --snapshot --clean` (`.github/workflows/tests.yml:339`) and the boot-and-diff digest check via `go run ./scripts/verify-ui-digest` (`.github/workflows/tests.yml:389-406`).

**CI.** **Both**, in the `goreleaser-build` job, depending on the diff. **Post-merge by default** — the job left the gate because at a ~540s median it set the entire gate wall clock, twice the next job, to answer a releasability question the gate does not ask. It is **gate**-tier when the diff touches `release-packaging` or `frontend` paths, where it is the check that actually matters; the `goreleaser` filter also errs broad over `cmd/`, `scripts/`, `internal/webui/`, `go.mod` and `go.sum`. When it is not pulled into the gate it still runs on the push to `main`, as part of that push's full unfiltered matrix.

**Evidence.** The digest comparison result line plus the command.

**Gaps.** `scripts/verify-ui-digest` is reachable from CI only — no Makefile target invokes it, so a contributor cannot run this check through `make` and must reproduce the workflow steps by hand. Both Verify commands are heavy tier: their presence is cited above, but neither was executed when this map was written — they need an external toolchain and boot a server on `localhost:7777`.

### `rbac`

**Scope.** `test/integration/rbac_test.go`, and any change to authorization or permission logic under `internal/`. This class has no distinct verification command; it exists so that the governance floor has a membership test.

**Verify.** `make test-integration` (`Makefile:71`).

**CI.** **Gate**, but only as part of the aggregate `integration-tests` job — RBAC pass/fail is not separately visible.

**Evidence.** The result lines for `rbac_test.go` specifically, quoted from the suite output. An aggregate integration pass is not acceptable evidence for this class.

**Gaps.** No isolated runner exists. Evidence is therefore a filtered result rather than a dedicated command. A dedicated target would fix this. Separately, the `Held Paths` check approximates this class by path — `internal/rbac/**`, `internal/access/**`, `test/integration/rbac_test.go` — in order to hold such a change for maintainer approval. That approximation is knowingly wrong in both directions: this class's real scope is any change to authorization or permission logic under `internal/`, which no path list decides. It errs broad by rule, and a governance change living outside those three paths is held by nothing.

### `verification-infrastructure`

**Scope.** `Makefile`, `.github/workflows/**`, `.golangci.yml`, `.markdownlint.json`, `.goreleaser.yaml`, `ui/eslint.config.js`, `ui/vitest.config.ts`, and `ui/tsconfig*.json`.

**Verify.** The union of the checks the change affects, plus the co-evolution requirement — if the diff changes what runs, `docs/verification.md` is modified in the same diff.

**CI.** **Gate** for the affected checks: every path in this list except `.markdownlint.json` is in the `go`, `frontend` or `goreleaser` filter, so touching it pulls the corresponding jobs in. **No** for the co-evolution requirement itself, which nothing enforces.

**Evidence.** The affected checks' own evidence, plus the map diff itself.

**Gaps.** The co-evolution requirement is mechanically checkable — touched paths intersect this list, map file absent from the diff, fail — but is not wired. It was recorded as awaiting the `gated-main` work; `gated-main` landed without wiring it, and that scope call is the reason rather than an oversight. It stands as a conformance criterion, not as something in force. `docs/verification.md` is, however, an instruction surface held by the `Held Paths` check, so a diff that edits the map cannot merge without maintainer approval — which puts a human on the co-evolution question in one direction and nobody on it in the other, the direction that matters.

### `docs`

**Scope.** `docs/**` and root-level `*.md`.

**Verify.** Nothing.

**CI.** **No**, in either tier. A docs-only diff matches no filter, so every gate job skips and `Test Summary` passes on skips alone. That is the map's own answer — this class has no runnable check — but it is worth saying plainly that such a change merges with nothing having run.

**Evidence.** Manual — the report states which claims were checked and against what.

**Gaps.** This is the only class with no runnable check at all. `.markdownlint.json` exists but has no runner anywhere in the repo — no make target, no npm script, no CI job. No link checking exists. Manual criterion: any claim a doc makes about a command is checked against the file that defines it, and the citation goes in the report. The known live instance is `test/fixtures/README.md`, which documents a `configs/`/`conversations/`/`llm_responses/`/`test_repos/` layout that does not match the flat files actually present.
