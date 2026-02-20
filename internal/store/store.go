package store

import (
	"database/sql"
	"embed"
	"fmt"
	"log/slog"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite3"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/mattn/go-sqlite3"

	"github.com/jaimegago/joe/internal/knowledge"
	"github.com/jaimegago/joe/internal/observability"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Store provides access to all repositories.
type Store struct {
	db             *sql.DB
	Sources        SourceRepository
	Sessions       SessionRepository
	Clarifications ClarificationRepository
	Cache          CacheRepository
	Facts          FactRepository
	Knowledge      knowledge.Repository
	Metrics        *observability.Metrics
}

// New creates a new Store with the given database path.
// The dbPath should include any SQLite flags (e.g., "joe.db?_foreign_keys=on").
// Use ":memory:" for in-memory database (testing).
func New(dbPath string, metrics *observability.Metrics) (*Store, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	metrics = observability.EnsureMetrics(metrics)

	store := &Store{
		db:             db,
		Sources:        &sqlSourceRepository{db: db, metrics: metrics},
		Sessions:       &sqlSessionRepository{db: db, metrics: metrics},
		Clarifications: &sqlClarificationRepository{db: db, metrics: metrics},
		Cache:          &sqlCacheRepository{db: db, metrics: metrics},
		Facts:          &sqlFactRepository{db: db, metrics: metrics},
		Knowledge:      knowledge.NewRepository(db, metrics),
		Metrics:        metrics,
	}

	return store, nil
}

// Migrate runs all pending migrations.
func (s *Store) Migrate() error {
	driver, err := sqlite3.WithInstance(s.db, &sqlite3.Config{})
	if err != nil {
		return fmt.Errorf("create migration driver: %w", err)
	}

	source, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("create migration source: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", source, "sqlite3", driver)
	if err != nil {
		return fmt.Errorf("create migrator: %w", err)
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("run migrations: %w", err)
	}

	return nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// DB returns the underlying database connection (for transactions).
func (s *Store) DB() *sql.DB {
	return s.db
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
