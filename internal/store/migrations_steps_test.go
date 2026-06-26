package store

import (
	"strconv"
	"strings"
	"testing"
)

// headVersion returns the highest migration version embedded in migrationsFS
// (the same FS the migrator runs against). The per-migration round-trip tests
// step DOWN from HEAD to a fixed anchor version and back up; deriving HEAD here
// — rather than hardcoding a distance-from-top step literal — means adding a new
// migration on top shifts every step automatically and requires ZERO edits to
// these tests. See D-0044 (extends the D-0032 "volatile growth-driven counts are
// structural, never fixed literals" principle into test code).
func headVersion(t *testing.T) uint {
	t.Helper()
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		t.Fatalf("ReadDir migrations: %v", err)
	}
	var max uint
	for _, e := range entries {
		name := e.Name()
		i := strings.IndexByte(name, '_')
		if i <= 0 {
			continue
		}
		v, err := strconv.ParseUint(name[:i], 10, 64)
		if err != nil {
			continue
		}
		if uint(v) > max {
			max = uint(v)
		}
	}
	if max == 0 {
		t.Fatal("no migrations found in migrationsFS")
	}
	return max
}

// stepsDownTo returns the negative step count that reverts every migration
// strictly ABOVE the given anchor version, leaving the schema AT anchor. The
// anchor is a fixed, known version that does not shift as later migrations are
// added, so the returned magnitude grows automatically with HEAD. Pass the
// result directly to migrate.Migrate.Steps.
func stepsDownTo(t *testing.T, anchor uint) int {
	t.Helper()
	head := headVersion(t)
	if anchor > head {
		t.Fatalf("anchor version %d is above HEAD %d", anchor, head)
	}
	return -int(head - anchor)
}
