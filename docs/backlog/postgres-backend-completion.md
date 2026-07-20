Backlog — Make the PostgreSQL (pgx) backend functional
Status: open
Priority: later

PostgreSQL support in Joe is **latent, not shipped**. The configuration surface accepts
`database.driver: "pgx"`, `pgx` is a direct dependency, `store.New` opens the configured
driver, every query site runs through a placeholder rewriter so the repositories are
dialect-aware, and the migration runner (`internal/store/store.go`, `Store.Migrate`) has a
PostgreSQL branch (`migratePostgres.WithInstance`). What is missing is a PostgreSQL-valid
migration set: setting the driver to `pgx` today fails at `Store.Migrate()`, before the
server serves, because the embedded migration SQL is SQLite-dialect-locked. This item
tracks what completion requires. Recorded by the `postgres-backend-truth` session, which
walked back the public and reference docs that overstated this as supported (D-0085); the
honest-wording fix removed the shipped-truth violation without touching the live migration
chain, and completion was deferred here.

## What completion requires

- **Dialect-portable migration rewrites (or per-dialect variants) for the six blocking
  migrations.** The blockers are a closed historical set — `001_initial`, `006_rbac`,
  `015_audit_log`, `018_auth_login`, `020_admin_audit`, and
  `027_audit_session_lifecycle_kind`. Two SQLite-only constructs must go or be branched:
  - `INTEGER PRIMARY KEY AUTOINCREMENT` (SQLite-only spelling of an auto-increment PK) in
    migrations `001`, `006`, `015`, `018`, `020`, `027`. PostgreSQL uses `BIGSERIAL` /
    `GENERATED … AS IDENTITY`.
  - The SQLite append-only trigger DDL in `015_audit_log`, **repeated in the `018` / `020` /
    `027` audit-table rebuilds**: `CREATE TRIGGER … BEFORE UPDATE/DELETE … BEGIN SELECT
    RAISE(ABORT, '…'); END`. PostgreSQL has no `RAISE(ABORT)` statement-trigger form.

- **PostgreSQL-native append-only enforcement for `audit_log`.** Preserve the
  dual-enforcement invariant (application-level plus database-level) documented on the
  `015_audit_log` migration. On PostgreSQL this means a trigger **function** that raises an
  exception on UPDATE/DELETE plus `CREATE TRIGGER … EXECUTE FUNCTION …`. The break-tested
  append-only property must hold identically on both drivers.

- **Driver-value validation in `internal/config/validation.go`.** There is **no** driver
  allow-list today — any string reaches `sql.Open` (only `""` → default and `pgx` do
  anything useful; other values fail opaquely at open time). Add a validated driver set
  (mirroring the LLM-provider allow-list pattern) so an unsupported driver is a clear boot
  error.

- **Decide whether a `JOE_DATABASE_DRIVER` env override should exist.** Today only the DSN
  has an env override (`JOE_DATABASE_DSN`, `internal/config/config.go`); the driver is
  config-only. Decide whether to add the symmetric `JOE_DATABASE_DRIVER` override or keep
  the driver config-file-only on purpose.

- **A CI job running the full migration set against real PostgreSQL.** The cross-driver
  migration test (`internal/sessionmodel/schema_test.go`) is env-gated on
  `JOE_TEST_POSTGRES_DSN` and **skips** when unset, so it does not run in CI. Stand up a
  PostgreSQL service in CI, set the DSN, and un-gate the test so the PostgreSQL migration
  path is continuously proven.

## Note on the portability rule

The migration-portability rule adopted at migration `009` (`009_session_model`) was **never
retrofitted backward** to the earlier migrations and was **not applied to the audit-table
rebuilds** (`018` / `020` / `027`), which re-emit the SQLite trigger DDL. Any completion
work must sweep those rebuilds, not just the original `015`.
