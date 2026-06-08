package store

import (
	"database/sql"
	"embed"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"
	migrateDatabase "github.com/golang-migrate/migrate/v4/database"
	migratePostgres "github.com/golang-migrate/migrate/v4/database/postgres"
	migrateSQLite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/jackc/pgx/v5/stdlib" // registers "pgx" database/sql driver
	_ "modernc.org/sqlite"

	"github.com/jaimegago/joe/internal/knowledge"
	"github.com/jaimegago/joe/internal/observability"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Store provides access to all repositories.
type Store struct {
	db             *sql.DB
	driver         string
	Components     ComponentRepository
	Sessions       SessionRepository
	Clarifications ClarificationRepository
	Cache          CacheRepository
	Facts          FactRepository
	Knowledge      knowledge.Repository
	Metrics        *observability.Metrics
}

// DatabaseConfig carries the driver name and connection DSN for Store.New.
// Re-exported here from config so callers only need to import the store package.
type DatabaseConfig struct {
	// Driver is DriverSQLite (default) or DriverPostgres.
	Driver string
	// DSN is the file path for SQLite or a libpq connection string for PostgreSQL.
	DSN string
}

// sqlitePragmas are the per-connection PRAGMAs every SQLite connection in the
// pool must carry, in modernc.org/sqlite's _pragma(value) form. busy_timeout
// makes a connection wait (rather than fail instantly with SQLITE_BUSY) when
// another holds the write lock; foreign_keys enables FK enforcement (off by
// default in SQLite); journal_mode=WAL is file-persistent but included so a
// brand-new database is in WAL from its first connection.
var sqlitePragmas = []string{
	"busy_timeout(5000)",
	"foreign_keys(1)",
	"journal_mode(WAL)",
}

// withSQLitePragmas appends sqlitePragmas to a SQLite DSN as modernc _pragma
// query parameters, choosing ? or & based on whether the DSN already carries a
// query string. A pragma the caller already set in the DSN (e.g. an operator
// override of busy_timeout) is left untouched so the override wins.
func withSQLitePragmas(dsn string) string {
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	var b strings.Builder
	b.WriteString(dsn)
	for _, p := range sqlitePragmas {
		name := p[:strings.IndexByte(p, '(')]
		if strings.Contains(dsn, "_pragma="+name+"(") {
			continue // caller already set this pragma in the DSN; respect it
		}
		b.WriteString(sep)
		b.WriteString("_pragma=")
		b.WriteString(p)
		sep = "&"
	}
	return b.String()
}

// isUnsharedMemoryDSN reports whether dsn names an in-memory SQLite database
// that is private to a single connection. The two in-memory forms are the bare
// ":memory:" and a "file:...mode=memory" URI; either is connection-private
// unless it also opts into "cache=shared", which makes one named in-memory
// database visible across connections. Such an unshared DB must be pinned to a
// one-connection pool, since a second connection would open a fresh, empty
// database. The check runs on the caller-supplied DSN (before pragmas are
// appended).
func isUnsharedMemoryDSN(dsn string) bool {
	isMemory := dsn == ":memory:" ||
		strings.Contains(dsn, ":memory:") ||
		strings.Contains(dsn, "mode=memory")
	if !isMemory {
		return false
	}
	return !strings.Contains(dsn, "cache=shared")
}

// New opens a database connection and returns a fully wired Store.
// SQLite-specific PRAGMAs are encoded in the DSN (see withSQLitePragmas) so
// they apply to EVERY connection the pool opens — not just one.
func New(cfg DatabaseConfig, metrics *observability.Metrics) (*Store, error) {
	if cfg.Driver == "" {
		cfg.Driver = DriverSQLite
	}

	dsn := cfg.DSN
	if cfg.Driver == DriverSQLite {
		// busy_timeout and foreign_keys are PER-CONNECTION settings. Applying
		// them with a one-off db.Exec after Open lands on a single pooled
		// connection and leaves the pool's other connections at SQLite's
		// defaults — busy_timeout=0 (instant SQLITE_BUSY under write
		// contention) and foreign_keys=OFF (no FK enforcement). Encoding them
		// in the DSN makes modernc.org/sqlite run them on every connection it
		// opens. Postgres has built-in MVCC and FK enforcement; not needed.
		dsn = withSQLitePragmas(dsn)
	}

	db, err := sql.Open(cfg.Driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	if cfg.Driver == DriverSQLite {
		if isUnsharedMemoryDSN(cfg.DSN) {
			// An unshared in-memory SQLite database is private to a single
			// connection: every additional pooled connection opens its own
			// empty database with none of the migrated tables. A pool larger
			// than one therefore intermittently serves "no such table" once
			// any concurrent access (e.g. a background title write) forces a
			// second connection. Pin in-memory DBs to a single connection so
			// all access shares the one migrated database.
			db.SetMaxOpenConns(1)
			db.SetMaxIdleConns(1)
		} else {
			// WAL mode caps concurrent writers; match the previous behaviour.
			db.SetMaxOpenConns(10)
			db.SetMaxIdleConns(2)
		}
	} else {
		// PostgreSQL handles concurrency natively; allow a larger pool.
		db.SetMaxOpenConns(25)
		db.SetMaxIdleConns(5)
	}
	db.SetConnMaxLifetime(time.Hour)

	metrics = observability.EnsureMetrics(metrics)

	store := &Store{
		db:             db,
		driver:         cfg.Driver,
		Components:     &sqlComponentRepository{db: db, driver: cfg.Driver, metrics: metrics},
		Sessions:       &sqlSessionRepository{db: db, driver: cfg.Driver, metrics: metrics},
		Clarifications: &sqlClarificationRepository{db: db, driver: cfg.Driver, metrics: metrics},
		Cache:          &sqlCacheRepository{db: db, driver: cfg.Driver, metrics: metrics},
		Facts:          &sqlFactRepository{db: db, driver: cfg.Driver, metrics: metrics},
		Knowledge:      knowledge.NewRepository(db, cfg.Driver, metrics),
		Metrics:        metrics,
	}

	return store, nil
}

// Migrate runs all pending migrations against the configured database driver.
func (s *Store) Migrate() error {
	source, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("create migration source: %w", err)
	}

	var driver migrateDatabase.Driver
	switch s.driver {
	case DriverPostgres:
		driver, err = migratePostgres.WithInstance(s.db, &migratePostgres.Config{DatabaseName: "joe"})
	default: // DriverSQLite
		driver, err = migrateSQLite.WithInstance(s.db, &migrateSQLite.Config{})
	}
	if err != nil {
		return fmt.Errorf("create migration driver: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", source, s.driver, driver)
	if err != nil {
		return fmt.Errorf("create migrator: %w", err)
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("run migrations: %w", err)
	}

	return nil
}

// Driver returns the database driver name (DriverSQLite or DriverPostgres).
func (s *Store) Driver() string {
	return s.driver
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// DB returns the underlying *sql.DB (for packages that need raw access).
func (s *Store) DB() *sql.DB {
	return s.db
}

// PanicStore returns a ClusterPanicStore backed by this store's database.
func (s *Store) PanicStore() *sqlPanicStore {
	return NewPanicStore(s.db, s.driver)
}

// parseTimeOrWarn parses a time string in RFC3339 format and logs a warning if
// parsing fails. Returns a pointer to the parsed time, or nil on failure.
func parseTimeOrWarn(value, field string) *time.Time {
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		slog.Warn("failed to parse time from database", "field", field, "value", value, "error", err)
		return nil
	}
	return &t
}
