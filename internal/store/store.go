package store

import (
	"database/sql"
	"embed"
	"fmt"
	"log/slog"
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
	Sources        SourceRepository
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

// New opens a database connection and returns a fully wired Store.
// SQLite-specific PRAGMAs are applied via Exec after the connection is opened,
// so the DSN never needs to carry driver-specific query parameters.
func New(cfg DatabaseConfig, metrics *observability.Metrics) (*Store, error) {
	if cfg.Driver == "" {
		cfg.Driver = DriverSQLite
	}

	db, err := sql.Open(cfg.Driver, cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	if cfg.Driver == DriverSQLite {
		// Apply SQLite-specific settings that were previously embedded in the
		// DSN query string. Postgres has built-in MVCC; these are not needed.
		for _, pragma := range []string{
			"PRAGMA foreign_keys = ON",
			"PRAGMA journal_mode = WAL",
			"PRAGMA busy_timeout = 5000",
		} {
			if _, err := db.Exec(pragma); err != nil {
				db.Close()
				return nil, fmt.Errorf("apply sqlite pragma %q: %w", pragma, err)
			}
		}
		// WAL mode caps concurrent writers; match the previous behaviour.
		db.SetMaxOpenConns(10)
		db.SetMaxIdleConns(2)
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
		Sources:        &sqlSourceRepository{db: db, driver: cfg.Driver, metrics: metrics},
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
