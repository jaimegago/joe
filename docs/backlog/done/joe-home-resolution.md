How does `joe` actually resolve its home directory?
Status: done — `getSecureHomeDir()` resolves via `os/user.Current()`, deliberately
bypassing `$HOME`; session `joe-home-resolution`
Priority: next

Opened from an observation made during the `v0.1.0` go/no-go
released-binary integrity check (session `release-pipeline-03`, D-0123).

The operator booted the release binary with an overridden `HOME` environment
variable, expecting that override to isolate the boot from any real local
state. It did not: the binary still resolved the operator's real `~/.joe`
directory — database, encryption key, registered components, session
archive — and ran live refreshers against that real data. This contradicts
the assumption that `joe` resolves its home directory (and therefore its
default data directory) from `$HOME`.

Scope: a read-only investigation of how `joe` actually resolves its home /
data directory — read the relevant resolution code, identify what input(s)
it actually honors (`$HOME`, a different env var, a config default, an
absolute fallback, something else), and document the finding. Any change to
that resolution behavior, or to how the release runbook's integrity check
should isolate itself as a result, is a separate future decision — not in
scope for this item.

## Findings

### 1. Every default-resolution path funnels through one function, and it deliberately ignores `HOME`

`internal/paths/defaults.go` is the single seam. `JoeDirPath()`
([internal/paths/defaults.go:33-39](../../../internal/paths/defaults.go#L33-L39))
returns `<home>/.joe` by calling `getSecureHomeDir()`. `DefaultConfigPath()`
([internal/paths/defaults.go:21-28](../../../internal/paths/defaults.go#L21-L28)),
`DatabasePath()` ([internal/paths/defaults.go:59-65](../../../internal/paths/defaults.go#L59-L65)),
`EncryptionKeyPath()` ([internal/paths/defaults.go:71-77](../../../internal/paths/defaults.go#L71-L77)),
and `ExpandPath()`'s `~`-expansion branch
([internal/paths/defaults.go:44-53](../../../internal/paths/defaults.go#L44-L53)) all call
`JoeDirPath()` or `getSecureHomeDir()` directly or transitively — there is no second
implementation anywhere in the tree.

`getSecureHomeDir` is platform-specific but uniform in mechanism:
`internal/paths/secure_unix.go:15-28` (darwin/linux) and
`internal/paths/secure_windows.go:15-27` (windows) both call `os/user.Current()` and
return `currentUser.HomeDir`, **not** `os.Getenv("HOME")` or `os.UserHomeDir()` (which
itself reads `$HOME` on Unix). The unix implementation's own comment states the intent
plainly: "uses system APIs that cannot be manipulated via environment variables... This
prevents HOME env var bypass attacks where an attacker could set HOME=/tmp/fake"
([internal/paths/secure_unix.go:11-14](../../../internal/paths/secure_unix.go#L11-L14)).
`os/user.Current()` on Unix resolves via `getpwuid_r()` against the process's real UID —
i.e. `/etc/passwd` (or the platform's NSS-backed equivalent) — which is unaffected by any
environment variable the process's own shell or launcher sets.

Consumers reached through this seam, all with no independent path logic of their own:

- **Config file default.** `resolveConfigPath` falls back to `""`
  ([cmd/joe/server.go:184-200](../../../cmd/joe/server.go#L184-L200)); `runServerWithDeps`
  then calls `paths.DefaultConfigPath()` when that's empty
  ([cmd/joe/server.go:210-213](../../../cmd/joe/server.go#L210-L213)). Every offline
  subcommand (`db backup`, `db restore`, `admin bootstrap`, `panic`, `unlock`, `skills`,
  `incident`) shares the same default through `resolveConfigFlag`
  ([cmd/joe/main.go:762](../../../cmd/joe/main.go#L762)).
- **Database DSN default.** `databaseConfigFor` seeds `dbCfg.DSN` from
  `paths.DatabasePath()` before applying `cfg.Database.DSN` as an override
  ([cmd/joe/db.go:84-99](../../../cmd/joe/db.go#L84-L99)); the daemon boot path
  (`runServerWithDeps`) does the identical seed-then-override with
  `deps.databasePath()`, wired to `paths.DatabasePath` by default
  ([cmd/joe/server.go:107-109](../../../cmd/joe/server.go#L107-L109),
  [cmd/joe/server.go:274-289](../../../cmd/joe/server.go#L274-L289)); `joe panic`/`joe unlock`
  share the same shape via `resolveDatabaseConfig`
  ([cmd/joe/main.go:73,128,150-167](../../../cmd/joe/main.go#L73)).
- **Encryption key default.** `encryptionKeyPathFor` returns
  `cfg.Database.EncryptionKeyPath` when set, else `paths.EncryptionKeyPath()`
  ([cmd/joe/db.go:117-121](../../../cmd/joe/db.go#L117-L121)); the daemon's boot key load
  calls the same resolver ([cmd/joe/server.go:451](../../../cmd/joe/server.go#L451)).
- **Session archive default.** `cfg.Server.SessionArchiveDir` when set, else
  `filepath.Join(joeDir, paths.SessionArchiveDirName)`
  ([cmd/joe/server.go:554-557](../../../cmd/joe/server.go#L554-L557)), where `joeDir` is
  `deps.joeDirPath()` — `paths.JoeDirPath` by default
  ([cmd/joe/server.go:107](../../../cmd/joe/server.go#L107),
  [cmd/joe/server.go:263](../../../cmd/joe/server.go#L263)).
- **Registered-component state.** Lives in `components`/`graph_*` tables inside the same
  SQLite file the database DSN default names — no separate resolution path.
- **Skills directory and skills policy.** `filepath.Join(joeDir, "skills")`
  ([cmd/joe/server.go:595](../../../cmd/joe/server.go#L595)) and `skills.LoadPolicy(joeDir)`
  ([cmd/joe/server.go:609](../../../cmd/joe/server.go#L609)), same `joeDir`. **Neither has a
  config-file override field** — grep of `internal/config/config.go` finds no
  `skills_dir`/`skills_policy` yaml key.
- **Git adapter local clone cache.** `repoDir` builds `<joeDir>/repos/<hash>` by calling
  `paths.JoeDirPath()` directly, unconditionally
  ([internal/adapters/git/git.go:150-158](../../../internal/adapters/git/git.go#L150-L158)).
  No config override field exists for this path either.

The codebase already carries a same-conclusion comment at
[cmd/joe/main.go:99-103](../../../cmd/joe/main.go#L99-L103), written before this
investigation, documenting that `paths.JoeDirPath` does not honour `$HOME` and that this
is why `joe admin bootstrap`'s tests inject a fake store rather than trying to relocate a
real one via the environment.

### 2. Why an overridden `HOME` did not redirect the D-0123 run

`getSecureHomeDir()` is the mechanism, exactly as designed:
[internal/paths/secure_unix.go:15-28](../../../internal/paths/secure_unix.go#L15-L28) resolves
the home directory via `os/user.Current()` → `getpwuid_r()` against the OS user record, not
via `os.Getenv("HOME")`. Setting `HOME=/tmp/isolated` before invoking the release binary
changes what the shell and most other tools consider the home directory, but `joe` never
reads that variable for this purpose — it asks the OS for the real account's home directory
by UID and gets the operator's actual home back regardless of the environment. This is not
a bug or an ordering accident (nothing is "resolved and persisted before the override could
matter" — the resolution happens fresh on each call, at boot); it is a deliberate anti-bypass
design, per the unix implementation's own comment. No config file was in play on that run to
separately pin an absolute path — the observed behavior is fully accounted for by this one
function, confirmed by reading the source; no further reproduction was necessary or
attempted (out of scope: this session made no code changes and ran no binary).

### 3. What a correct isolation recipe needs, stated as inputs (not a runbook edit)

Given the resolution paths in Finding 1, isolating a `joe` boot requires acting on inputs
`getSecureHomeDir()` cannot see, not `HOME`:

- **The account's actual home directory record** (`/etc/passwd`'s home field or the
  platform equivalent) is the only input `getSecureHomeDir()` reads for the `~/.joe`
  default. Nothing in-process can override it; isolation at this layer means running as a
  different OS user/UID or inside a container/chroot with its own passwd entry — which is
  what the runbook's "run 2" container route already does (`docs/RELEASING.md:221-224`),
  independently of this investigation.
- **`--config` / `JOE_CONFIG`** redirect the config *file* location only
  ([cmd/joe/server.go:184-200](../../../cmd/joe/server.go#L184-L200)) and, if the path starts
  with `~`, that `~` is *also* expanded via `getSecureHomeDir()`
  ([internal/paths/defaults.go:44-53](../../../internal/paths/defaults.go#L44-L53)) — an
  absolute, non-tilde path is required for this override to actually land outside the real
  home.
- **`database.dsn` / `JOE_DATABASE_DSN`** and **`database.encryption_key_path` /
  `JOE_DATABASE_ENCRYPTION_KEY_PATH`** must each be set to an absolute path inside the
  isolated tree ([cmd/joe/db.go:96-98](../../../cmd/joe/db.go#L96-L98),
  [internal/config/config.go:563-573](../../../internal/config/config.go#L563-L573)) — leaving
  either unset falls back to the real-home default even with an isolated `--config` file in
  use for everything else.
- **`server.session_archive_dir`** must likewise be set explicitly
  ([internal/config/config.go:185](../../../internal/config/config.go#L185),
  [cmd/joe/server.go:554-557](../../../cmd/joe/server.go#L554-L557)); it has no env-var
  override, config-file-only.
- **The skills directory, skills policy file, and the git adapter's local clone cache have
  no override at all** — config file, env var, or otherwise. Even a fully-specified
  `--config` pointing every other setting at an isolated tree cannot relocate these three;
  full isolation is unreachable through configuration alone today. This gap is the subject
  of the new follow-up item below
  ([`docs/backlog/home-dir-override-seam.md`](../home-dir-override-seam.md)).
