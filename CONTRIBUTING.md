# Contributing to Joe

## Project status

Joe is a single-maintainer, open-source project. Contributions are welcome, but review latency will vary — this isn't a team with an on-call rotation for PRs. The maintainer may decline changes that conflict with decisions already recorded in the project's decision log, even when the change itself is well-executed.

## Before you start

Open an issue before starting non-trivial work, so the approach can be discussed before you've written the code.

[`docs/project/DECISIONS.md`](docs/project/DECISIONS.md) is the normative, append-only decision log for this project — read it for context on why the codebase looks the way it does. Treat settled decisions there as settled: don't open a PR that re-litigates one. If you think a decision is wrong, raise it in an issue and let the maintainer decide whether to revisit it. The log itself is maintainer-only — contributors don't append to it; if your change is accepted and does warrant a new entry, the maintainer records it.

## Development setup

Requires Go 1.25 or later, plus Node.js 20+ and npm for the web UI. See [README.md](README.md#quick-start) for the full prerequisites and clone/build steps.

**The mandatory frontend rebuild discipline.** The web UI is served from a Go `//go:embed` captured at compile time (`make build-ui` stages the Vite output into `internal/webui/dist`, which `make build` then embeds into the binary). This means editing UI source is invisible until you rebuild and restart:

1. Rebuild the UI bundle (`make build-ui`, or just run `make build`, which includes it).
2. Rebuild the `joe` binary so it embeds the new bundle (`make build`).
3. Restart the running `joe` process — the embed is baked into the binary at compile time, so a stale *running process* serves stale UI even after you've rebuilt the file on disk.

Testing against a binary you forgot to rebuild — or a rebuilt binary you forgot to restart — is the classic mistake here. If your UI change doesn't seem to be taking effect, this is the first thing to check.

## Testing

```bash
go build ./...   # compile
go test ./...    # unit tests, no special environment required
go vet ./...     # vet
gofmt -s -w .    # format
```

Integration tests run under a build tag: `go test -tags=integration ./...`.

See [test/README.md](test/README.md) for the test tree layout, but treat [CLAUDE.md](CLAUDE.md) as the authoritative source for the exact commands if the two ever disagree.

PRs that change behavior are expected to include or update tests covering that behavior. A PR that touches the safety invariants below without a test change will be treated as unfinished, not just unverified.

## Safety invariants contributions must not violate

Joe's core promise — "in observation mode, Joe cannot change anything" — is meant to be a machine-checkable property of the code, not an operator's promise. That's the actual point of the project: these guarantees hold because tests pin them, not because the README says so. The following invariants are load-bearing. A PR that weakens any of them, even incidentally, will be closed rather than merged as-is:

- **The write floor is boot-resolved and runtime-immutable.** Joe resolves a read-only "write floor" once at boot; nothing in the running process can lower it afterward — recovery is always a restart, never a live down-transition. An unconfigured Joe boots into observation (read-only) by default. (`internal/safety/floor.go`)
- **Denial precedence is fixed:** the write floor is checked first, then the incident gate, then RBAC — in that order, every time. (`internal/tools/executor.go`)
- **Every tool is classified Read or Mutate at author time**, in code, not inferred at runtime. Reads pass the floor unconditionally; Mutates are denied unless explicitly enabled in the safety policy. An unrecognized tool name defaults to Mutate and is denied — fail-closed, pinned by `TestClassifyTool_UnknownDefaultIsMutate`. (`internal/safety/tier.go`)
- **No kubeconfig ingestion.** The Kubernetes transport is a hand-built `*rest.Config` from stored cluster coordinates — it never loads a kubeconfig, sets an exec provider, an auth provider, or an impersonation header. This is a structural break-test, not a code review convention: `TestTransport_NoKubeconfigIngestion`, `TestCredentialPackage_NoKubeconfigIngestion`, `TestNoClientcmdOutsideAllowedAdapters`, and `TestTransport_NoForbiddenAuthMechanisms` all fail the build if this is violated. (`internal/adapters/k8s/`)
- **Joe is an MCP server only, never an MCP client.** It exposes its own governed tools over MCP but does not consume external MCP servers' tools, because the protocol has no enforceable way to classify a tool's mutation behavior — see the decision log for the full reasoning. This one is currently a recorded design decision without an automated guard test; if you're touching anything MCP-adjacent, don't add an MCP client.

If you're not sure whether a change touches one of these, ask in the issue before opening the PR.

## PR guidelines

Fork the repo and open a pull request against `main`. Keep changes small and focused — a PR that does one thing is much easier to review than one that mixes a feature with incidental refactoring. Write commit messages that state what changed and why, not just what. You don't need to follow the maintainer's internal session-tag or backlog conventions (visible in `docs/project/` and referenced from `CLAUDE.md`) — those are for the maintainer's own workflow, not a contribution requirement.

## License

Joe is licensed under [Apache License 2.0](LICENSE). By submitting a contribution, you agree it's licensed under the same terms — inbound equals outbound. No CLA and no DCO sign-off are required.

## Conduct

Be respectful and stay on topic. Disagreement about technical direction is fine; personal attacks aren't. The maintainer moderates issues and PRs at their discretion. There's no separate code-of-conduct document — this paragraph is it.
