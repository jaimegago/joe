package audit_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/store"
)

// TestVerifyAppendOnly_PassesOnAMigratedStoreAndLeavesNothing: the boot probe
// accepts the store the migrations build, and its probe row is never
// committed — the table holds exactly what it held before.
func TestVerifyAppendOnly_PassesOnAMigratedStoreAndLeavesNothing(t *testing.T) {
	s := freshStore(t)
	repo := audit.NewRepository(s.DB(), store.DriverSQLite)
	insertAct(t, repo, "s-a", "k8s-prod", audit.DecisionAllow, audit.Operation{Verb: "list", Resource: "pods"}, "")
	before := countRows(t, s.DB())

	if err := audit.VerifyAppendOnly(context.Background(), s.DB(), store.DriverSQLite); err != nil {
		t.Fatalf("VerifyAppendOnly on a migrated store: %v", err)
	}
	if after := countRows(t, s.DB()); after != before {
		t.Errorf("the probe left rows behind: %d before, %d after", before, after)
	}
	// And it can run again: nothing it did changed what the next run sees.
	if err := audit.VerifyAppendOnly(context.Background(), s.DB(), store.DriverSQLite); err != nil {
		t.Fatalf("second VerifyAppendOnly: %v", err)
	}
}

// TestVerifyAppendOnly_RefusesAStoreThatAcceptsEither: with either trigger
// gone the probe fails, and it fails for the statement that trigger guarded.
// Each case removes one enforcement and leaves the other, so the probe is
// shown to check UPDATE and DELETE separately rather than one of them twice.
func TestVerifyAppendOnly_RefusesAStoreThatAcceptsEither(t *testing.T) {
	for _, tc := range []struct {
		trigger string
		accepts string
	}{
		{"audit_log_no_update", "UPDATE"},
		{"audit_log_no_delete", "DELETE"},
	} {
		t.Run(tc.accepts, func(t *testing.T) {
			s := freshStore(t)
			ctx := context.Background()
			if _, err := s.DB().ExecContext(ctx, `DROP TRIGGER `+tc.trigger); err != nil {
				t.Fatalf("drop %s: %v", tc.trigger, err)
			}
			before := countRows(t, s.DB())

			err := audit.VerifyAppendOnly(ctx, s.DB(), store.DriverSQLite)
			if !errors.Is(err, audit.ErrNotAppendOnly) {
				t.Fatalf("VerifyAppendOnly = %v; want ErrNotAppendOnly on a store that accepts %s", err, tc.accepts)
			}
			if want := "accepted " + tc.accepts; !strings.Contains(err.Error(), want) {
				t.Errorf("error = %q; want it to name the statement the store accepted (%q)", err, want)
			}
			// A failed probe leaves nothing behind either: it rolled back.
			if after := countRows(t, s.DB()); after != before {
				t.Errorf("the failed probe left rows behind: %d before, %d after", before, after)
			}
		})
	}
}

// TestVerifyAppendOnly_RefusesAStoreItCannotProbe: a store where the probe
// cannot run at all is not passed. Here the table is absent; the probe's own
// insert fails and the result is a refusal, not a pass.
func TestVerifyAppendOnly_RefusesAStoreItCannotProbe(t *testing.T) {
	s, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	// No Migrate: audit_log does not exist.
	if err := audit.VerifyAppendOnly(context.Background(), s.DB(), store.DriverSQLite); !errors.Is(err, audit.ErrNotAppendOnly) {
		t.Fatalf("VerifyAppendOnly = %v; want ErrNotAppendOnly when the probe cannot run", err)
	}
}
