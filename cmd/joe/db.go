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

	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/paths"
	"github.com/jaimegago/joe/internal/store"
)

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
// the same config the daemon uses (cfg.Database overrides, else the .joe
// directory's joe.db), and returns it with a closer.
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
func defaultOpenBackupStore() (backupStore, func() error, error) {
	cfg, err := config.Load(paths.DefaultConfigPath())
	if err != nil {
		return nil, nil, fmt.Errorf("load config: %w", err)
	}
	dbPath, err := paths.DatabasePath()
	if err != nil {
		return nil, nil, fmt.Errorf("resolve database path: %w", err)
	}
	dbCfg := store.DatabaseConfig{Driver: store.DriverSQLite, DSN: dbPath}
	if cfg.Database.Driver != "" {
		dbCfg.Driver = cfg.Database.Driver
	}
	if cfg.Database.DSN != "" {
		dbCfg.DSN = cfg.Database.DSN
	}
	s, err := store.New(dbCfg, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("open database: %w", err)
	}
	return storeBackupHandle{s}, s.Close, nil
}

// runDBCommand implements `joe db <backup>` — operator utilities that act on
// Joe's database file directly rather than through the running daemon. The
// namespace is deliberately open-ended; backup is its first member.
func runDBCommand(ctx context.Context, args []string, stdout, stderr io.Writer, deps runDeps) int {
	usage := func() {
		fmt.Fprintln(stderr, "Usage: joe db <backup> [flags]")
		fmt.Fprintln(stderr, "  backup <dest> [--force]     Write a consistent copy of Joe's database to <dest>.")
		fmt.Fprintln(stderr, "                              Safe to run against a live Joe. Refuses an existing")
		fmt.Fprintln(stderr, "                              <dest> unless --force is given.")
	}
	if len(args) == 0 {
		usage()
		return 2
	}

	switch args[0] {
	case "backup":
		return runDBBackup(ctx, args[1:], stdout, stderr, deps)
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
	if err := fs.Parse(reorderFlagsFirst(args, map[string]bool{})); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "Error: backup requires exactly one <dest> path.")
		fmt.Fprintln(stderr, "Usage: joe db backup <dest> [--force]")
		return 2
	}
	dest := fs.Arg(0)

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

	bs, closeStore, err := deps.openBackupStore()
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
