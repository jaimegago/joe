# Verification Map

What a change to this repository owes in verification, by class of work. Each class carries five fields:

- **Scope** — a classifier decidable from a diff.
- **Verify** — commands that are runnable today. Runnable, not enforced: a command that works locally but no machine re-runs still belongs here.
- **CI** — whether a machine currently re-runs each command. This is what makes the untrusted-until-re-run rule operable.
- **Evidence** — the one line that proves the check passed, plus the command as run.
- **Gaps** — classes or commands with no automated check, and the manual criteria standing in.

## Rules

- Work spanning classes takes the union of their requirements, with the most demanding discipline winning. Overlap is expected and is not a defect: `.goreleaser.yaml` is in both `release-packaging` and `verification-infrastructure`, and a diff touching it owes both.
- Unclassifiable work is a stop-and-ask. It never defaults to no verification.
- The map co-evolves. A diff that changes **what runs** must modify `docs/verification.md` in the same push. A diff that only touches a file in the `verification-infrastructure` path list without changing what runs — a comment, a formatting fix — does not.
- Agent-reported verification is untrusted until a machine re-runs it. The CI field states, per row, whether one does. Rows marked no are unverified claims from the ledger's point of view, however green the session reported them.
- CI red on `main` caused by a change's push flips that work from done back to blocked. No exceptions.

Class names are stable identifiers; other tooling references them by name.

## Classes

### `go-backend`

**Scope.** Any `.go` file under `cmd/`, `internal/`, or `joecored/` carrying no build tag, plus `go.mod` and `go.sum`.

**Verify.** `make vet` (`Makefile:124`); `gofmt -s -l .` returning empty (`.github/workflows/tests.yml:147-152`); `make test-unit` (`Makefile:66`); golangci-lint v2.12.2 (`.github/workflows/tests.yml:154-158`).

**CI.** Yes, all four, via the `unit-tests` and `lint` jobs.

**Evidence.** The final pass/fail line of `make test-unit` and the empty output of the gofmt check, each with the command as run.

**Gaps.** golangci-lint runs with only `ineffassign`, `govet`, and `unused` enabled; `errcheck` and `staticcheck` are explicitly disabled (`.golangci.yml:15-17`). A green lint does not mean unchecked errors are absent. golangci-lint is additionally a heavy-tier command for map-authoring purposes: its presence in CI is cited above, but it was not executed when this map was written — CI pins v2.12.2 through the action, so a differently-versioned local binary produces a non-comparable result and proves nothing about the gate.

### `go-tagged-tests`

**Scope.** Any file under `test/integration/` or `test/e2e/`, or any `.go` file carrying `//go:build integration` or `//go:build e2e`.

**Verify.** `make vet-tagged` (`Makefile:131-133`); `make test-integration` (`Makefile:71`); `make test-e2e` (`Makefile:75`, which depends on `make build`).

**CI.** The two test commands yes (`.github/workflows/tests.yml:63`, `:115-116`); **`make vet-tagged` no** — it is reachable only through `make precommit` (`Makefile:140`), which is not wired as a git hook and which nothing enforces.

**Evidence.** The per-suite pass line for each command run.

**Gaps.** A bare `go test ./...` or `go vet ./...` never compiles either tree, so default commands passing says nothing about these files — the comment at `Makefile:126-130` records this as a real incident, citing D-0118. `test/e2e/` is a single test file plus its harness, with no matrix. CI e2e runs against placeholder keys falling back to the literal `test-key` (`.github/workflows/tests.yml:81-82`), so real-provider paths are unexercised. `make test-e2e` is heavy tier: its presence is cited above, but it was not executed when this map was written — it depends on `make build`, which runs `npm ci` (network) and spawns real binaries.

### `frontend`

**Scope.** Anything under `ui/` other than its build and test configuration, which belongs to `verification-infrastructure`.

**Verify.** `npm run build` (`ui/package.json:8`); `npm run lint` (`:9`); `npm test` (`:11`).

**CI.** **Build only.** The single UI command anywhere in CI is `npm ci && npm run build` via `make build-ui` (`Makefile:52`). Lint and tests are re-run by no machine.

**Evidence.** The build success line, the eslint summary, and the vitest summary, each with the command as run.

**Gaps.** `npm run format` is `prettier --write` (`ui/package.json:10`) — it mutates rather than failing, so it cannot gate anything and is not listed under Verify. All three Verify commands are heavy tier: their presence is cited above, but none was executed when this map was written — each requires `npm ci` first.

### `release-packaging`

**Scope.** `.goreleaser.yaml`, the embedded UI asset path under `internal/webui/dist`, `scripts/stage-ui-for-release.sh`, and `scripts/verify-ui-digest`.

**Verify.** `goreleaser build --snapshot --clean` (`.github/workflows/tests.yml:196`) and the boot-and-diff digest check via `go run ./scripts/verify-ui-digest` (`.github/workflows/tests.yml:246-263`).

**CI.** Yes, in the `goreleaser-build` job.

**Evidence.** The digest comparison result line plus the command.

**Gaps.** `scripts/verify-ui-digest` is reachable from CI only — no Makefile target invokes it, so a contributor cannot run this check through `make` and must reproduce the workflow steps by hand. Both Verify commands are heavy tier: their presence is cited above, but neither was executed when this map was written — they need an external toolchain and boot a server on `localhost:7777`.

### `rbac`

**Scope.** `test/integration/rbac_test.go`, and any change to authorization or permission logic under `internal/`. This class has no distinct verification command; it exists so that the governance floor has a membership test.

**Verify.** `make test-integration` (`Makefile:71`).

**CI.** Yes, but only as part of the aggregate `integration-tests` job — RBAC pass/fail is not separately visible.

**Evidence.** The result lines for `rbac_test.go` specifically, quoted from the suite output. An aggregate integration pass is not acceptable evidence for this class.

**Gaps.** No isolated runner exists. Evidence is therefore a filtered result rather than a dedicated command. A dedicated target would fix this.

### `verification-infrastructure`

**Scope.** `Makefile`, `.github/workflows/**`, `.golangci.yml`, `.markdownlint.json`, `.goreleaser.yaml`, `ui/eslint.config.js`, `ui/vitest.config.ts`, and `ui/tsconfig*.json`.

**Verify.** The union of the checks the change affects, plus the co-evolution requirement — if the diff changes what runs, `docs/verification.md` is modified in the same diff.

**CI.** **No.** Nothing enforces the co-evolution requirement today.

**Evidence.** The affected checks' own evidence, plus the map diff itself.

**Gaps.** The co-evolution requirement is mechanically checkable — touched paths intersect this list, map file absent from the diff, fail — but is not wired. It stands as a conformance criterion awaiting the gated-main work, not as something in force.

### `docs`

**Scope.** `docs/**` and root-level `*.md`.

**Verify.** Nothing.

**CI.** No.

**Evidence.** Manual — the report states which claims were checked and against what.

**Gaps.** This is the only class with no runnable check at all. `.markdownlint.json` exists but has no runner anywhere in the repo — no make target, no npm script, no CI job. No link checking exists. Manual criterion: any claim a doc makes about a command is checked against the file that defines it, and the citation goes in the report. The known live instance is `test/fixtures/README.md`, which documents a `configs/`/`conversations/`/`llm_responses/`/`test_repos/` layout that does not match the flat files actually present.
