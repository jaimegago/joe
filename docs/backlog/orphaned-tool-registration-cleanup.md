# Orphaned tool registration cleanup — safety entries for the removed local-tool tree

Status: open

The `internal/tools/local/` tree (`read_file`, `write_file`, `run_command`,
`local_git_status` / `local_git_diff`, `ask_user`) was removed: no constructor
survives anywhere in the tree, `internal/tools/default.go` registers only the
`shared/` and `core/` tools, and `internal/agentloop/echotool_test.go:11` records
the removal. But the safety layer still carries classification entries, policy
entries, and self-protection guards that exist only to govern those removed tools.
With no backing tool they never execute — they are dead weight, not a live risk —
but they should be cleaned up so the safety surface describes only what the binary
actually ships. This file enumerates them so the code cleanup can be sequenced as
its own slice. It was surfaced by the `docs-reference-audit-02` rewrite of
`docs/reference/security-in-layers.md`; that doc now documents these as orphans and
points here.

Scope note: this is a code-cleanup backlog only. Removing these entries is a
behavior-neutral change (none of them gate a registered tool today), but it touches
the safety package and its tests, so it warrants its own reviewed slice rather than
riding along with a documentation pass.

## Orphaned classification entries — `internal/safety/tier.go`

These map a removed tool name to an action class; `ClassifyTool` will never be
called with these names because no tool registers under them.

- `internal/safety/tier.go:75` — `read_file` → `ActionRead`.
- `internal/safety/tier.go:76` — `local_git_status` → `ActionRead`.
- `internal/safety/tier.go:77` — `local_git_diff` → `ActionRead`.
- `internal/safety/tier.go:78` — `ask_user` → `ActionRead`.
- `internal/safety/tier.go:221` — `write_file` → `ActionMutate`, `PolicyKey: "write_file"`.
- `internal/safety/tier.go:222` — `run_command` → `ActionMutate`, `PolicyKey: "run_command"`.

## Orphaned policy entries — `internal/safety/policy.go`

These parse and default `act` sections for the two removed mutating tools. The
parse is harmless (backward compatibility with existing policy files), but the
keys gate nothing.

- `internal/safety/policy.go:35` — `ActPolicy.WriteFile WriteFilePolicy` field (`yaml:"write_file"`).
- `internal/safety/policy.go:36` — `ActPolicy.RunCommand RunCommandPolicy` field (`yaml:"run_command"`).
- `internal/safety/policy.go:43-47` — `WriteFilePolicy` struct (`Enabled`, `AllowedDirectories`).
- `internal/safety/policy.go:49-53` — `RunCommandPolicy` struct (`Enabled`, `AllowedCommands`).
- `internal/safety/policy.go:73-82` — `DefaultPolicy()` ships `Act.WriteFile{Enabled:false}` and `Act.RunCommand{Enabled:true, AllowedCommands:[ls cat head tail grep find wc]}`. The `run_command` default of `true` is the source of the long-standing "default-deny vs `run_command: enabled: true`" doc tension — it gates no live tool.
- `internal/safety/policy.go:147-148` — `IsT3Allowed` case `"write_file"`.
- `internal/safety/policy.go:149-150` — `IsT3Allowed` case `"run_command"`.
- `internal/safety/policy.go:116-123` — tilde-expansion / absolute-path normalization of `Act.WriteFile.AllowedDirectories` in `LoadPolicy`; only relevant to the removed `write_file` sandbox.

Related (not strictly local-tool orphans, but the same inert-shim class, already
acknowledged in the doc and in D-0018/D-0019/D-0020): the `record:` section
(`RecordPolicy` struct at `internal/safety/policy.go:25-30`, `IsT2Allowed` at
`:129-142`) is the backward-compat shim for model-maintenance tools that are now
classified Read. Listed here for completeness; fold into the same cleanup if
desired.

## Orphaned self-protection guards — `internal/safety/invariants.go`

These functions are correct defense-in-depth and intentionally remain compiled in,
but they currently have **no production caller** — the file/command tools that
would invoke them are removed (verified: no non-test caller; the only reference is
a doc comment at `internal/skills/policy.go:16`). Keep or remove is a deliberate
call; if kept, they should be documented as latent guards rather than active
enforcement.

- `internal/safety/invariants.go:29-93` — `IsPathAllowed` (excludes `~/.joe/`, safety policy, skills policy). Was invoked by `read_file` / `write_file`.
- `internal/safety/invariants.go:110-139` — `IsCommandAllowed` (blocks `joe`, `kill`, `pkill`, `killall`). Was invoked by `run_command`.
- `internal/safety/invariants.go:164-193` — `IsWritePathInAllowedDir` (the `write_file` `allowed_directories` sandbox). No remaining caller.

Recommendation: keep the architectural self-protection intent (invariant #2 in
`security-in-layers.md`) but either re-wire these guards into any reintroduced
file/command tool or annotate them in source as latent defense-in-depth.
