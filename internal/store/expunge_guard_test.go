package store

import (
	"io/fs"
	"strings"
	"testing"
)

// TestInvariant_ExpungeNoSoftDeleteIdentifiers is the named structural
// guard for PHASE-0-SESSION-MODEL.md §5b-5 — incident deletion is
// TRUE EXPUNGE, not a tombstone. Phase 1 carries no soft-delete
// column anywhere in the schema; the only retention seam is the
// retention_class TEXT on agent_sessions (a label, not a tombstone
// flag).
//
// This test walks every file under migrations/*.sql (via the same
// embed.FS the migrator uses, so it can't drift out of sync) and
// asserts ZERO occurrences of the identifiers `deleted_at`,
// `archived_at`, or `tombstone`. The match is case-insensitive
// substring so column casing variations are caught.
//
// The guard is ABSOLUTE — there is no allowlist. Any future
// appearance of those identifiers fails the build with an
// explanatory message. To add one legitimately:
//  1. Update PHASE-0-SESSION-MODEL.md §5b-5 explicitly to permit
//     soft-delete and document the new lifecycle.
//  2. Update this test to relax the constraint with a documented
//     justification.
//
// Both edits must happen in the same diff so the design intent
// survives review.
//
// Lives in package `store` (not store_test) so it can access the
// unexported migrationsFS variable directly.
func TestInvariant_ExpungeNoSoftDeleteIdentifiers(t *testing.T) {
	forbidden := []string{"deleted_at", "archived_at", "tombstone"}

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
					t.Errorf("§5b-5 violation: migration %s line %d contains forbidden "+
						"identifier %q.\n  line: %s\n\n"+
						"Phase 1 deletion is TRUE EXPUNGE (incident + linked investigations "+
						"cascade away). No soft-delete column may appear. The only retention "+
						"seam is retention_class TEXT on agent_sessions. To legitimately add "+
						"a soft-delete mechanism, update PHASE-0-SESSION-MODEL.md §5b-5 and "+
						"relax this guard in the same diff.",
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
