package store

import (
	"database/sql"
	"embed"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite3"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/mattn/go-sqlite3"
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
}

// New creates a new Store with the given database path.
// The dbPath should include any SQLite flags (e.g., "joe.db?_foreign_keys=on").
// Use ":memory:" for in-memory database (testing).
func New(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	store := &Store{
		db:             db,
		Sources:        &sqlSourceRepository{db: db},
		Sessions:       &sqlSessionRepository{db: db},
		Clarifications: &sqlClarificationRepository{db: db},
		Cache:          &sqlCacheRepository{db: db},
		Facts:          &sqlFactRepository{db: db},
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
