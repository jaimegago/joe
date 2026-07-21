package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	sqlitedrv "modernc.org/sqlite"

	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/paths"
	"github.com/jaimegago/joe/internal/store"
)

// sqliteBusy is SQLite's SQLITE_BUSY result code. The occupancy probe classifies
// the driver's typed error by code rather than matching its message text, for the
// reason the kubernetes refresher's forbidden-detection does the same: message
// text is not an API.
const sqliteBusy = 5

// backupStore is the narrow surface `joe db backup` needs from the store: the
// resolved driver name (backup is SQLite-only) and a statement executor for the
// VACUUM INTO. It exists so routing and error-path tests can inject a fake
// without opening a real database, mirroring panicRowStore for `joe unlock`.
type backupStore interface {
	Driver() string
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// storeBackupHandle adapts *store.Store to backupStore: Driver comes from the
// embedded store, ExecContext from the *sql.DB underneath it.
type storeBackupHandle struct{ *store.Store }

func (h storeBackupHandle) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return h.Store.DB().ExecContext(ctx, query, args...)
}

// defaultOpenBackupStore opens the database `joe db backup` copies from, honoring
// the config the command loaded (cfg.Database overrides, else the .joe
// directory's joe.db), and returns it with a closer. Taking the already-loaded
// config is what lets `--config` name which database gets copied.
//
// It deliberately does NOT run migrations, diverging from defaultOpenPanicStore.
// Backup is read-only with respect to the source: VACUUM INTO copies whatever
// schema version it finds and is indifferent to which one that is, so migrating
// would buy nothing and would silently upgrade the operator's database as a side
// effect of a command whose name promises a copy. That matters most in the case
// the command exists for — an operator backing up precisely because they suspect
// the database is damaged, who must not have it altered on the way out.
//
// Opening alongside a running daemon is safe for the reason D-0018 established
// for `joe unlock`: WAL plus busy_timeout let a second connection read while the
// daemon writes. Backup generalizes that argument from one row to the whole file.
func defaultOpenBackupStore(cfg *config.Config) (backupStore, func() error, error) {
	dbCfg, err := databaseConfigFor(cfg)
	if err != nil {
		return nil, nil, err
	}
	s, err := store.New(dbCfg, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("open database: %w", err)
	}
	return storeBackupHandle{s}, s.Close, nil
}

// databaseConfigFor reports the driver and DSN the daemon itself would use for
// the given config: the .joe directory's joe.db by default, overridden by
// cfg.Database where set. Every offline command that touches the database
// resolves through it so they provably act on the same file.
//
// It takes a config rather than loading one because its callers must not re-read
// it: a command that validates or reports against one config and writes to the
// database named by another is exactly the failure `--config` exists to prevent,
// and a resolver that loads its own config makes that failure representable.
//
// The same override shape also lives in cmd/joe/server.go and in
// defaultOpenPanicStore; folding all three together is a separate change,
// deliberately not attempted here (docs/backlog/admin-bootstrap-cli-04.md).
func databaseConfigFor(cfg *config.Config) (store.DatabaseConfig, error) {
	dbPath, err := paths.DatabasePath()
	if err != nil {
		return store.DatabaseConfig{}, fmt.Errorf("resolve database path: %w", err)
	}
	dbCfg := store.DatabaseConfig{Driver: store.DriverSQLite, DSN: dbPath}
	if cfg == nil {
		return dbCfg, nil
	}
	if cfg.Database.Driver != "" {
		dbCfg.Driver = cfg.Database.Driver
	}
	if cfg.Database.DSN != "" {
		dbCfg.DSN = cfg.Database.DSN
	}
	return dbCfg, nil
}

// encryptionKeyPathFor reports where the component-config encryption key lives
// for the given config: database.encryption_key_path when set, else
// ~/.joe/encryption.key.
//
// It is the single resolver every consumer goes through — the daemon's boot key
// load and `joe db restore`'s missing-key pre-flight — so the command that warns
// about a missing key and the process that would mint one always name the same
// file. A second resolution site is how the two would drift apart.
//
// It takes a config for the reason databaseConfigFor does: restore must check
// the key belonging to the same config that named the database it is about to
// replace, and a resolver that loaded its own config could not promise that.
//
// The configured value is used verbatim: no "~" expansion, matching
// database.dsn. See config.DatabaseConfig.EncryptionKeyPath.
func encryptionKeyPathFor(cfg *config.Config) (string, error) {
	if cfg != nil && cfg.Database.EncryptionKeyPath != "" {
		return cfg.Database.EncryptionKeyPath, nil
	}
	return paths.EncryptionKeyPath()
}

// componentInspection is what the read-only pre-flight open learns about SRC's
// component rows without decrypting anything.
type componentInspection struct {
	total     int
	encrypted int
}

// errNoComponentsTable reports that SRC carries no components table, i.e. it is
// not recognizable as a Joe database.
var errNoComponentsTable = errors.New("no components table")

// sourceDB is the narrow surface `joe db restore` needs from a read-only open of
// SRC. It is deliberately NOT backupStore: backup opens the CONFIGURED database
// through store.New, which is exactly what restore must never do to its target —
// that open would create the very sidecars restore exists to delete.
//
// The interface is behavioural rather than a raw statement executor so routing
// and refusal tests can inject a fake; *sql.Row cannot be constructed by a test,
// so a query-shaped seam would not be fakeable.
type sourceDB interface {
	IntegrityCheck(ctx context.Context) (string, error)
	InspectComponents(ctx context.Context) (componentInspection, error)
	CopyTo(ctx context.Context, dest string) error
}

// roSourceDB is the real sourceDB: a read-only handle on SRC.
type roSourceDB struct{ db *sql.DB }

// defaultOpenSourceDB opens path read-only for pre-flight and for the copy.
//
// The DSN is minimal and deliberate on two counts. It uses the `file:` URI form
// with mode=ro, which the driver enforces at open — a write attempt fails with
// "attempt to write a readonly database" rather than relying on a pragma a later
// statement could flip. It does NOT use immutable=1, which reads only the main
// file and silently ignores a -wal: pointed at a WAL-carrying database, an
// immutable open reports it as empty or schemaless, which is precisely the
// misreading restore exists to catch. joe's own pragmas are not applied either;
// journal_mode(WAL) is meaningless on a read-only handle.
func defaultOpenSourceDB(path string) (sourceDB, func() error, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return nil, nil, fmt.Errorf("open read-only: %w", err)
	}
	// One connection: the copy runs VACUUM INTO, and a pool that opens a second
	// connection mid-statement buys nothing here.
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, nil, fmt.Errorf("open read-only: %w", err)
	}
	return &roSourceDB{db: db}, db.Close, nil
}

func (s *roSourceDB) IntegrityCheck(ctx context.Context) (string, error) {
	var result string
	if err := s.db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result); err != nil {
		return "", err
	}
	return result, nil
}

// InspectComponents counts component rows and how many carry an encrypted config.
//
// The table-existence check is here because it is SQLite-specific (sqlite_master)
// and restore is the only caller inspecting a FOREIGN file that might not be a
// Joe database at all. The counting itself delegates to store.ScanComponentConfigs,
// which is the single home for the encrypted-marker detection — the daemon's boot
// key loader asks the same question of the live database, and two copies of that
// subtle unmarshal-then-test rule would be one copy too many.
func (s *roSourceDB) InspectComponents(ctx context.Context) (componentInspection, error) {
	var name string
	err := s.db.QueryRowContext(ctx,
		`SELECT name FROM sqlite_master WHERE type='table' AND name='components'`).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return componentInspection{}, errNoComponentsTable
	}
	if err != nil {
		return componentInspection{}, err
	}

	scan, err := store.ScanComponentConfigs(ctx, s.db)
	if err != nil {
		return componentInspection{}, err
	}
	return componentInspection{total: scan.Total, encrypted: scan.Encrypted}, nil
}

// CopyTo writes a consistent copy of SRC to dest with VACUUM INTO, executed from
// the read-only handle. This is the verified mechanism rather than a byte copy:
// it reads through a real transaction, so a SRC that still carries a live -wal is
// normalized into one complete file at dest instead of losing whatever had not
// been checkpointed. dest is bound, never interpolated.
func (s *roSourceDB) CopyTo(ctx context.Context, dest string) error {
	_, err := s.db.ExecContext(ctx, "VACUUM INTO ?", dest)
	return err
}

// defaultProbeTargetOccupied reports whether another process currently holds the
// database at path open. It is the discriminator between the two states that look
// identical on disk — a running daemon and an unclean shutdown, both of which
// leave sidecars behind.
//
// In WAL mode a connection holds a shared lock on the main file for its whole
// lifetime, so an exclusive-lock attempt observes any attached process, including
// a completely idle one. A plain write probe does not: it succeeds against an idle
// daemon and detects only one caught mid-write.
//
// It writes nothing: BEGIN IMMEDIATE takes the lock and the transaction is rolled
// back at once. The pool is pinned to a single connection because under
// locking_mode=exclusive a second pooled connection contends with the first and
// reports the caller's own probe as a busy database.
func defaultProbeTargetOccupied(path string) (bool, error) {
	db, err := sql.Open("sqlite",
		path+"?_pragma=busy_timeout(300)&_pragma=locking_mode(exclusive)&_txlock=immediate")
	if err != nil {
		return false, err
	}
	db.SetMaxOpenConns(1)
	defer func() { _ = db.Close() }()

	tx, err := db.Begin()
	if err != nil {
		// Only a BUSY answers the question. Any other failure (an unreadable or
		// non-database target, an I/O error) must surface as itself rather than
		// be reported to the operator as "a daemon is running", which would send
		// them looking for a process that is not there.
		var serr *sqlitedrv.Error
		if errors.As(err, &serr) && serr.Code() == sqliteBusy {
			return true, nil
		}
		return false, err
	}
	_ = tx.Rollback()
	return false, nil
}

// runDBCommand implements `joe db <backup|restore>` — operator utilities that act
// on Joe's database file directly rather than through the running daemon. The
// namespace is deliberately open-ended.
func runDBCommand(ctx context.Context, args []string, stdout, stderr io.Writer, deps runDeps) int {
	usage := func() {
		fmt.Fprintln(stderr, "Usage: joe db <backup|restore> [flags]")
		fmt.Fprintln(stderr, "  backup <dest> [--force] [--config <path>]")
		fmt.Fprintln(stderr, "                              Write a consistent copy of Joe's database to <dest>.")
		fmt.Fprintln(stderr, "                              Safe to run against a live Joe. Refuses an existing")
		fmt.Fprintln(stderr, "                              <dest> unless --force is given.")
		fmt.Fprintln(stderr, "  restore <src> [--force] [--allow-missing-key] [--config <path>]")
		fmt.Fprintln(stderr, "                              Restore <src> over Joe's configured database. Stop Joe")
		fmt.Fprintln(stderr, "                              first. Checks <src> and refuses an existing database")
		fmt.Fprintln(stderr, "                              unless --force.")
		fmt.Fprintln(stderr, "")
		fmt.Fprintln(stderr, "  --config names the same config file the daemon is started with; it decides")
		fmt.Fprintln(stderr, "  WHICH database these act on. Without it, ~/.joe/config.yaml is used.")
	}
	if len(args) == 0 {
		usage()
		return 2
	}

	switch args[0] {
	case "backup":
		return runDBBackup(ctx, args[1:], stdout, stderr, deps)
	case "restore":
		return runDBRestore(ctx, args[1:], stdout, stderr, deps)
	default:
		fmt.Fprintf(stderr, "Unknown db subcommand: %s\n\n", args[0])
		usage()
		return 2
	}
}

// runDBBackup implements `joe db backup <dest>`: a consistent copy of the SQLite
// store, taken with VACUUM INTO so it is safe against a live Joe. Copying the
// database file by hand is not equivalent — under WAL, committed data can still
// live in the -wal sidecar, so a file copy can yield a plausible-looking backup
// missing recent or all data. This command is that trap's answer.
func runDBBackup(ctx context.Context, args []string, stdout, stderr io.Writer, deps runDeps) int {
	fs := flag.NewFlagSet("joe db backup", flag.ContinueOnError)
	fs.SetOutput(stderr)
	force := fs.Bool("force", false, "replace an existing file at <dest>")
	// --config decides WHICH database is copied. Without it this command was the
	// sharpest case in the CLI: an operator running Joe from another config file
	// got a successful-looking backup of the default database, which is not the
	// one they were backing up.
	configPath := fs.String("config", "", "path to the config file (default ~/.joe/config.yaml)")
	// --config takes a following token and the operator writes <dest> first, so
	// the flag has to survive appearing after the positional.
	if err := fs.Parse(reorderFlagsFirst(args, map[string]bool{"config": true})); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "Error: backup requires exactly one <dest> path.")
		fmt.Fprintln(stderr, "Usage: joe db backup <dest> [--force] [--config <path>]")
		return 2
	}
	dest := fs.Arg(0)

	cfgPath, ok := resolveConfigFlag(*configPath, stderr)
	if !ok {
		return 1
	}
	cfg, err := deps.loadConfig(cfgPath)
	if err != nil {
		fmt.Fprintf(stderr, "Error: failed to load config: %v\n", err)
		return 1
	}

	// A missing parent directory surfaces from the engine as "unable to open
	// database file", which reads like a fault in the source database. Name the
	// real gap instead, and leave creating it to the operator: a mistyped path
	// should fail loudly, not quietly deposit a backup in a directory nobody
	// meant to exist.
	parent := filepath.Dir(dest)
	fi, err := os.Stat(parent)
	switch {
	case errors.Is(err, os.ErrNotExist):
		fmt.Fprintf(stderr, "Error: directory %s does not exist.\n", parent)
		fmt.Fprintln(stderr, "Create it first (joe db backup will not), then re-run.")
		return 1
	case err != nil:
		fmt.Fprintf(stderr, "Error: cannot inspect directory %s: %v\n", parent, err)
		return 1
	case !fi.IsDir():
		fmt.Fprintf(stderr, "Error: %s is not a directory, so %s cannot be written there.\n", parent, dest)
		return 1
	}

	// Check the destination BEFORE any SQL. The engine's own guard is not a
	// substitute for this one. VACUUM INTO refuses a destination it recognizes
	// as a database ("output file already exists"), but a 0- or 1-byte file —
	// what `touch dest.db` or a `> dest.db` redirect leaves behind — is
	// indistinguishable from a freshly created database to it, and is silently
	// overwritten. Any other occupied path fails with "file is not a database",
	// which names neither the problem nor the fix. One up-front stat closes the
	// clobber window and gives every occupied case the same instructive error.
	if _, err := os.Stat(dest); err == nil {
		if !*force {
			fmt.Fprintf(stderr, "Error: %s already exists; refusing to overwrite it.\n", dest)
			fmt.Fprintln(stderr, "Choose a path that does not exist, or pass --force to replace the file.")
			return 1
		}
		if err := os.Remove(dest); err != nil {
			fmt.Fprintf(stderr, "Error: cannot replace %s: %v\n", dest, err)
			fmt.Fprintln(stderr, "Remove it by hand, or choose a destination that does not exist.")
			return 1
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(stderr, "Error: cannot inspect %s: %v\n", dest, err)
		return 1
	}

	bs, closeStore, err := deps.openBackupStore(cfg)
	if err != nil {
		fmt.Fprintf(stderr, "Error: failed to open database: %v\n", err)
		fmt.Fprintln(stderr, "Check that the account running this command can read Joe's database and config.")
		return 1
	}
	defer func() { _ = closeStore() }()

	// VACUUM INTO is a SQLite statement; there is no equivalent to reach for on
	// another engine, so refuse rather than emit something that is not a backup.
	if drv := bs.Driver(); drv != store.DriverSQLite {
		fmt.Fprintf(stderr, "Error: joe db backup supports the SQLite store only; the configured driver is %q.\n", drv)
		fmt.Fprintln(stderr, "Back a non-SQLite database up with that engine's own tooling.")
		return 1
	}

	// Bind the destination rather than interpolating it: VACUUM INTO takes an
	// expression, so a path that came from an operator's shell needs no string
	// surgery to get there.
	if _, err := bs.ExecContext(ctx, "VACUUM INTO ?", dest); err != nil {
		fmt.Fprintf(stderr, "Error: backup failed: %v\n", err)
		fmt.Fprintf(stderr, "The source database is unchanged. Check that %s has room and is writable by this account, remove any partial file left at %s, then re-run.\n", parent, dest)
		return 1
	}

	written, err := os.Stat(dest)
	if err != nil {
		fmt.Fprintf(stderr, "Error: the backup reported success but %s cannot be read back: %v\n", dest, err)
		return 1
	}

	fmt.Fprintf(stdout, "Backup written to %s (%d bytes).\n", dest, written.Size())
	// Component config is the one thing encrypted at rest, and it is the part
	// that makes a restored Joe useful: without the key it decrypts to nothing
	// and the restored server reaches none of its components. Say that here,
	// where the operator is holding a file they may be about to file away alone.
	fmt.Fprintln(stdout, "Component configuration in this copy stays encrypted. Restoring it needs the matching encryption.key")
	fmt.Fprintln(stdout, "from Joe's .joe directory, under the home directory of the account running joe — back that key up")
	fmt.Fprintln(stdout, "alongside this file. Without it a restored Joe starts, but reaches none of its components.")
	return 0
}

// runDBRestore implements `joe db restore <src>`: it puts a backup back at the
// configured database path, with pre-flight checks that turn the restore
// procedure's documented silent failures into refusals before Joe ever boots.
//
// The command exists because the manual procedure has two traps that produce no
// error at the moment they are sprung. A stale -wal beside the target silently
// replays over the restored file and resurrects the previous database wholesale.
// A restore without the matching encryption.key boots cleanly and reaches no
// components. Both are caught here instead of at boot, or never.
func runDBRestore(ctx context.Context, args []string, stdout, stderr io.Writer, deps runDeps) int {
	fs := flag.NewFlagSet("joe db restore", flag.ContinueOnError)
	fs.SetOutput(stderr)
	force := fs.Bool("force", false, "replace the existing database at the configured path")
	allowMissingKey := fs.Bool("allow-missing-key", false,
		"restore encrypted component configs even though no encryption key is present")
	// --config governs TWO things here — the database this command replaces and
	// the encryption key it checks for beside it — and it must govern them
	// together. See the single load below.
	configPath := fs.String("config", "", "path to the config file (default ~/.joe/config.yaml)")
	if err := fs.Parse(reorderFlagsFirst(args, map[string]bool{"config": true})); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "Error: restore requires exactly one <src> path.")
		fmt.Fprintln(stderr, "Usage: joe db restore <src> [--force] [--allow-missing-key] [--config <path>]")
		return 2
	}
	src := fs.Arg(0)

	// ONE load, threaded to both config-governed uses. The key and the database
	// are one unit of durable state (see config.DatabaseConfig.EncryptionKeyPath):
	// a flag that redirected the database but resolved the key from the default
	// config would check for a key belonging to a different install, pass the
	// missing-key gate on the strength of it, and hand back a database nothing
	// can decrypt. Passing one config object to both makes that unrepresentable
	// rather than merely avoided.
	cfgPath, ok := resolveConfigFlag(*configPath, stderr)
	if !ok {
		return 1
	}
	cfg, err := deps.loadConfig(cfgPath)
	if err != nil {
		fmt.Fprintf(stderr, "Error: failed to load config: %v\n", err)
		return 1
	}

	// (a) SRC exists and is a regular file.
	srcInfo, err := os.Stat(src)
	switch {
	case errors.Is(err, os.ErrNotExist):
		fmt.Fprintf(stderr, "Error: %s does not exist.\n", src)
		fmt.Fprintln(stderr, "Name the backup file to restore from — the one `joe db backup` wrote.")
		return 1
	case err != nil:
		fmt.Fprintf(stderr, "Error: cannot inspect %s: %v\n", src, err)
		return 1
	case srcInfo.IsDir():
		fmt.Fprintf(stderr, "Error: %s is a directory; restore takes a backup FILE.\n", src)
		fmt.Fprintln(stderr, "If you backed up the whole .joe directory, name the joe.db inside it.")
		return 1
	case !srcInfo.Mode().IsRegular():
		fmt.Fprintf(stderr, "Error: %s is not a regular file.\n", src)
		return 1
	}

	dbCfg, err := deps.resolveDatabaseConfig(cfg)
	if err != nil {
		fmt.Fprintf(stderr, "Error: cannot resolve the database to restore to: %v\n", err)
		fmt.Fprintln(stderr, "Check that the account running this command can read Joe's config.")
		return 1
	}
	if dbCfg.Driver != store.DriverSQLite {
		fmt.Fprintf(stderr, "Error: joe db restore supports the SQLite store only; the configured driver is %q.\n", dbCfg.Driver)
		fmt.Fprintln(stderr, "Restore a non-SQLite database with that engine's own tooling.")
		return 1
	}
	target := dbCfg.DSN

	// Restoring a database onto itself would be a data-loss bug rather than a
	// no-op: the sidecar deletion below would remove the -wal that holds the very
	// rows the copy is about to read. Refuse before anything is touched.
	if tgtInfo, statErr := os.Stat(target); statErr == nil && os.SameFile(srcInfo, tgtInfo) {
		fmt.Fprintf(stderr, "Error: %s is Joe's configured database; restoring it onto itself would destroy it.\n", src)
		fmt.Fprintln(stderr, "Name a backup file taken with `joe db backup`.")
		return 1
	}

	// (b) Open SRC read-only and check it is a sound database before trusting it.
	sdb, closeSrc, err := deps.openSourceDB(src)
	if err != nil {
		fmt.Fprintf(stderr, "Error: cannot open %s as a database: %v\n", src, err)
		fmt.Fprintln(stderr, "Restore takes a SQLite backup file; check the path names the backup and not an archive of it.")
		return 1
	}
	defer func() { _ = closeSrc() }()

	integrity, err := sdb.IntegrityCheck(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "Error: cannot verify %s: %v\n", src, err)
		fmt.Fprintln(stderr, "Nothing has been changed. Restore from a different backup.")
		return 1
	}
	if integrity != "ok" {
		fmt.Fprintf(stderr, "Error: %s fails its integrity check: %s\n", src, integrity)
		fmt.Fprintln(stderr, "This backup is damaged and will not be restored. Nothing has been changed.")
		fmt.Fprintln(stderr, "Restore from an earlier backup instead.")
		return 1
	}

	// (c) Confirm SRC is a Joe database at all, and learn whether its component
	// configs are encrypted — without decrypting anything.
	insp, err := sdb.InspectComponents(ctx)
	if errors.Is(err, errNoComponentsTable) {
		fmt.Fprintf(stderr, "Error: %s is a valid SQLite database but does not look like Joe's.\n", src)
		fmt.Fprintln(stderr, "Checked for the components table and found none. Nothing has been changed.")
		return 1
	}
	if err != nil {
		fmt.Fprintf(stderr, "Error: cannot inspect %s: %v\n", src, err)
		return 1
	}

	// (d) Encrypted configs with no key. Since D-0120 boot ALSO refuses this, so
	// this gate is no longer the only thing standing between the operator and a
	// broken install — it is the earlier and better-diagnosed of the two. It is
	// kept, and kept first, because refusing here means nothing was overwritten:
	// catching it at boot means the restore already happened.
	if insp.encrypted > 0 {
		// The SAME cfg that named the target above — see the load in this
		// function's preamble.
		keyPath, keyErr := deps.encryptionKeyPath(cfg)
		if keyErr != nil {
			fmt.Fprintf(stderr, "Error: cannot resolve the encryption key path: %v\n", keyErr)
			return 1
		}
		if _, statErr := os.Stat(keyPath); errors.Is(statErr, os.ErrNotExist) {
			if !*allowMissingKey {
				fmt.Fprintf(stderr, "Error: this backup has encrypted component configuration, but no encryption key is present at\n  %s\n", keyPath)
				fmt.Fprintln(stderr, "Restoring without the key that was current when the backup was taken produces a Joe that")
				fmt.Fprintln(stderr, "reaches none of its components: the existing component configuration would stay unreadable,")
				fmt.Fprintln(stderr, "and there is no way to repair it afterwards. Joe refuses to boot in that state rather than")
				fmt.Fprintln(stderr, "starting up broken, so the restore would leave you with a database you cannot run.")
				fmt.Fprintln(stderr, "Put the matching encryption.key back first, or pass --allow-missing-key to accept that outcome.")
				return 1
			}
			fmt.Fprintf(stdout, "Warning: no encryption key at %s; --allow-missing-key was given.\n", keyPath)
			fmt.Fprintln(stdout, "The restored Joe reaches none of its components and will refuse to boot until the matching")
			fmt.Fprintln(stdout, "encryption.key is put back. This cannot be undone.")
		}
	}

	targetExists := true
	if _, statErr := os.Stat(target); errors.Is(statErr, os.ErrNotExist) {
		targetExists = false
	} else if statErr != nil {
		fmt.Fprintf(stderr, "Error: cannot inspect %s: %v\n", target, statErr)
		return 1
	}

	// (e) Occupancy. Sidecars alone cannot tell a running daemon from an unclean
	// stop — both leave them — so the lock probe is what separates the two. Probe
	// only an existing target: opening a path that does not exist would create it.
	if targetExists {
		busy, probeErr := deps.probeTargetOccupied(target)
		if probeErr != nil {
			fmt.Fprintf(stderr, "Error: cannot determine whether %s is in use: %v\n", target, probeErr)
			fmt.Fprintln(stderr, "Nothing has been changed.")
			return 1
		}
		if busy {
			// No override. A restore under a live daemon races writes it cannot
			// see, and the daemon would still hold the pre-restore database in
			// memory afterwards.
			fmt.Fprintf(stderr, "Error: a process holds %s open — Joe appears to be running.\n", target)
			fmt.Fprintln(stderr, "Stop Joe, then re-run. Nothing has been changed.")
			return 1
		}
	}

	// (f) An existing database is replaced only on an explicit --force.
	if targetExists && !*force {
		fmt.Fprintf(stderr, "Error: %s already exists; refusing to replace Joe's database.\n", target)
		fmt.Fprintln(stderr, "Back it up first with `joe db backup`, then pass --force to replace it.")
		return 1
	}

	// Clear any sidecars before writing. Read the reason precisely, because it is
	// narrower than it looks: with the copy done by VACUUM INTO, a stale -wal is
	// already defused — the engine finds no destination file, treats it as a new
	// database, and resets the WAL rather than recovering it. The deletion is
	// therefore defence in depth, not the thing standing between the operator and
	// data loss today.
	//
	// It is kept for two reasons. The engine's WAL reset on a fresh destination is
	// observed behaviour, not a documented contract, and the whole guarantee should
	// not rest on it. And the hazard is real and measured for the copy shape one
	// change away: laying the file down by byte copy — over an existing main file
	// OR over a unlinked one — lets the stale -wal replay on the next open and
	// resurrect the previous database wholesale, integrity_check reporting ok
	// because the result is a coherent old database rather than a corrupt one, and
	// then checkpoints itself into the main file and deletes its own evidence.
	// Removing the sidecars makes the outcome independent of which copy mechanism
	// is in force.
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.Remove(target + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(stderr, "Error: cannot remove the stale %s file beside %s: %v\n", suffix, target, err)
			fmt.Fprintln(stderr, "Remove it by hand, then re-run. Nothing has been changed.")
			return 1
		}
	}

	if *force && targetExists {
		if err := os.Remove(target); err != nil {
			fmt.Fprintf(stderr, "Error: cannot replace %s: %v\n", target, err)
			return 1
		}
	}

	if err := sdb.CopyTo(ctx, target); err != nil {
		fmt.Fprintf(stderr, "Error: restore failed: %v\n", err)
		fmt.Fprintf(stderr, "The database at %s is now incomplete and MUST NOT be booted.\n", target)
		fmt.Fprintln(stderr, "Re-run `joe db restore` once the cause is fixed.")
		return 1
	}

	// Verify what was actually written, from a read-only handle: the copy is a
	// VACUUM INTO output and so carries no sidecars, and this open adds none.
	vdb, closeV, err := deps.openSourceDB(target)
	if err != nil {
		fmt.Fprintf(stderr, "Error: the restored database at %s cannot be re-opened: %v\n", target, err)
		fmt.Fprintln(stderr, "It is incomplete and MUST NOT be booted. Re-run `joe db restore`.")
		return 1
	}
	vIntegrity, vErr := vdb.IntegrityCheck(ctx)
	_ = closeV()
	if vErr != nil || vIntegrity != "ok" {
		fmt.Fprintf(stderr, "Error: the restored database at %s does not verify: %v %s\n", target, vErr, vIntegrity)
		fmt.Fprintln(stderr, "It is incomplete and MUST NOT be booted. Re-run `joe db restore`.")
		return 1
	}

	written, err := os.Stat(target)
	if err != nil {
		fmt.Fprintf(stderr, "Error: the restore reported success but %s cannot be read back: %v\n", target, err)
		return 1
	}
	fmt.Fprintf(stdout, "Restored %s to %s (%d bytes).\n", src, target, written.Size())
	fmt.Fprintln(stdout, "Component configuration in this database stays encrypted. Joe needs the matching encryption.key")
	fmt.Fprintln(stdout, "from its .joe directory, under the home directory of the account running joe — restore that key")
	fmt.Fprintln(stdout, "alongside this file. Without it Joe starts, but reaches none of its components.")
	return 0
}
