`joe`'s home/data-directory resolution has no override seam for tests or isolated runs

Status: open

Opened from `docs/backlog/done/joe-home-resolution.md` (session `joe-home-resolution`), a
read-only investigation of how `joe` resolves its home and default data directory. That
investigation found the resolution mechanism (`getSecureHomeDir()`,
`internal/paths/secure_unix.go:15-28` / `secure_windows.go:15-27`) deliberately reads the
OS user record via `os/user.Current()` rather than `$HOME`, specifically to prevent an
attacker from bypassing protection with `HOME=/tmp/fake`
([internal/paths/secure_unix.go:11-14](../../internal/paths/secure_unix.go#L11-L14)). That
design choice is sound on its own terms and is not what this item questions.

What the investigation also found: several of the paths reached through this seam have
**no override at all**, config file or environment variable —
[cmd/joe/server.go:595](../../cmd/joe/server.go#L595) (skills directory),
[cmd/joe/server.go:609](../../cmd/joe/server.go#L609) (skills policy file), and
[internal/adapters/git/git.go:150-158](../../internal/adapters/git/git.go#L150-L158) (the git
adapter's local clone cache) all call `paths.JoeDirPath()` (or a value derived from it)
unconditionally. Even an operator who fully specifies `--config` with absolute
`database.dsn`, `database.encryption_key_path`, and `server.session_archive_dir` values
pointing at an isolated tree — the recipe `joe-home-resolution`'s Finding 3 lays out — cannot
relocate these three. The only isolation route that reaches all of them at once is changing
the OS-level home directory record itself (a different user/UID, a container, or a chroot),
which is what `docs/RELEASING.md`'s integrity-check runbook now recommends
(`docs/RELEASING.md:221-224`) as a practical workaround, not a fix.

## The decision this item is for

Whether `joe` should grow an explicit, narrow override for its home/data directory —
something like a `JOE_HOME` environment variable or a `--home`/`--data-dir` flag,
consulted only by `paths.JoeDirPath()` and its dependents, with the anti-bypass posture of
`getSecureHomeDir()` preserved for the *unset* case — versus leaving isolation to
process-level mechanisms (container/chroot/dedicated user) as the only sanctioned route.
Arguments for either side are already partly visible in the codebase:

- `cmd/joe/main.go:99-103` already documents that `joe admin bootstrap`'s tests inject a
  fake store specifically **because** `paths.JoeDirPath` cannot be redirected — the same
  gap that makes an isolated integrity-check boot need a container today likely also makes
  certain classes of test setup more awkward than they would be with a sanctioned override.
- Any override seam reintroduces exactly the shape `getSecureHomeDir()` was written to
  close off (an environment-controllable redirect of state resolution) unless it is scoped
  narrowly enough — e.g. requiring an explicit flag rather than an ambient env var, or
  refusing to honor the override when running with real credentials/against a non-empty
  existing `~/.joe` — to avoid recreating the `HOME=/tmp/fake` bypass class in a different
  name.
- A narrower, lower-risk sub-decision: even without a full home-dir override, giving the
  skills directory, skills policy path, and git clone cache the same config-file override
  seam `database.dsn` / `server.session_archive_dir` already have would close the "even a
  fully-specified `--config` can't fully isolate a boot" gap without touching
  `getSecureHomeDir()`'s anti-bypass behavior at all.

Out of scope for the item that opened this: changing `docs/RELEASING.md`'s runbook text
(it already documents the container workaround) and changing any resolution code — both are
exactly what this item exists to decide, not what closes it.
