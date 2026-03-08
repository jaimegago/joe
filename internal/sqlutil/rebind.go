// Package sqlutil provides database driver utilities shared across packages
// that cannot import internal/store without creating import cycles.
package sqlutil

import "strconv"

const (
	// DriverSQLite is the driver name for modernc.org/sqlite (database/sql).
	DriverSQLite = "sqlite"
	// DriverPostgres is the driver name for github.com/jackc/pgx/v5/stdlib (database/sql).
	DriverPostgres = "pgx"
)

// Rebind rewrites a query that uses ? positional placeholders to use $1, $2, ...
// style for PostgreSQL. For DriverSQLite and any other driver it returns the
// query unchanged.
func Rebind(driver, query string) string {
	if driver != DriverPostgres {
		return query
	}

	out := make([]byte, 0, len(query)+len(query)/4)
	n := 1
	for i := 0; i < len(query); i++ {
		if query[i] == '?' {
			out = append(out, '$')
			out = append(out, strconv.Itoa(n)...)
			n++
		} else {
			out = append(out, query[i])
		}
	}
	return string(out)
}
