package store

import (
	"io/fs"
	"strings"
	"testing"
)

// TestInvariant_NoLegacyTombstoneIdentifiers is the surviving remnant of the
// former §5b-5 true-expunge guard. The §5b-5 "no soft-delete column anywhere"
// invariant is SUPERSEDED by the redesign (DESIGN-CHAT-SESSIONS.md §12.4/§12.5):
// session lifecycle is now timestamp-driven, with an explicit soft-delete (trash)
// and archive stage. Migration 025 introduces those columns deliberately
// (trashed_at / trashed_by / purge_after / archived_at / archived_by /
// archive_ref). The old guard forbade `archived_at`, so it was relaxed in the
// same diff that added the lifecycle (as the guard's own instructions required).
//
// What survives is a NAMING guard: the redesign names its soft-delete columns
// `trashed_at` and `archived_at`, never `deleted_at` or `tombstone`. This test
// keeps those two legacy identifiers out of the schema so the lifecycle naming
// stays consistent. The match is case-insensitive substring.
//
// Lives in package `store` (not store_test) so it can access the unexported
// migrationsFS variable directly.
func TestInvariant_NoLegacyTombstoneIdentifiers(t *testing.T) {
	forbidden := []string{"deleted_at", "tombstone"}

	err := fs.WalkDir(migrationsFS, "migrations", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".sql") {
			return nil
		}
		data, err := migrationsFS.ReadFile(path)
		if err != nil {
			t.Errorf("read %s: %v", path, err)
			return nil
		}
		lower := strings.ToLower(string(data))
		for _, needle := range forbidden {
			if !strings.Contains(lower, needle) {
				continue
			}
			// Identify the offending line(s) for a useful error.
			for i, line := range strings.Split(string(data), "\n") {
				if strings.Contains(strings.ToLower(line), needle) {
					t.Errorf("lifecycle-naming violation: migration %s line %d contains "+
						"forbidden identifier %q.\n  line: %s\n\n"+
						"The session lifecycle (DESIGN-CHAT-SESSIONS.md §12.4/§12.5) names its "+
						"soft-delete and archive columns `trashed_at` and `archived_at` — never "+
						"`deleted_at` or `tombstone`. Use the §12.4 column names for consistency.",
						path, i+1, needle, strings.TrimSpace(line))
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk migrations: %v", err)
	}
}
