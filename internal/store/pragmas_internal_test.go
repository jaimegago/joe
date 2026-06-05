package store

import (
	"strings"
	"testing"
)

// TestWithSQLitePragmas documents the DSN-augmentation edge cases: a plain path
// gets a ?-prefixed query, a DSN that already has a query string gets &-joined
// params, and a pragma the caller already set is not duplicated (their value
// wins).
func TestWithSQLitePragmas(t *testing.T) {
	cases := []struct {
		name     string
		dsn      string
		contains []string
		absent   []string
	}{
		{
			name: "plain path",
			dsn:  "/var/joe/joe.db",
			contains: []string{
				"/var/joe/joe.db?_pragma=busy_timeout(5000)",
				"_pragma=foreign_keys(1)",
				"_pragma=journal_mode(WAL)",
			},
		},
		{
			name:     "existing query string uses ampersand",
			dsn:      "file:joe.db?cache=shared",
			contains: []string{"file:joe.db?cache=shared&_pragma=busy_timeout(5000)"},
		},
		{
			name:     "caller override of busy_timeout is preserved",
			dsn:      "joe.db?_pragma=busy_timeout(10000)",
			contains: []string{"busy_timeout(10000)", "_pragma=foreign_keys(1)"},
			absent:   []string{"busy_timeout(5000)"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := withSQLitePragmas(tc.dsn)
			for _, want := range tc.contains {
				if !strings.Contains(got, want) {
					t.Errorf("withSQLitePragmas(%q) = %q, missing %q", tc.dsn, got, want)
				}
			}
			for _, no := range tc.absent {
				if strings.Contains(got, no) {
					t.Errorf("withSQLitePragmas(%q) = %q, should not contain %q", tc.dsn, got, no)
				}
			}
		})
	}
}
