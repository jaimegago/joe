package store

import "github.com/jaimegago/joe/internal/sqlutil"

const (
	// DriverSQLite is the driver name for modernc.org/sqlite (database/sql).
	DriverSQLite = sqlutil.DriverSQLite
	// DriverPostgres is the driver name for github.com/jackc/pgx/v5/stdlib (database/sql).
	DriverPostgres = sqlutil.DriverPostgres
)

// Rebind rewrites a query that uses ? positional placeholders to use $1, $2, ...
// style for PostgreSQL. For DriverSQLite and any other driver it returns the
// query unchanged.
func Rebind(driver, query string) string {
	return sqlutil.Rebind(driver, query)
}
