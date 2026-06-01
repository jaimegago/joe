package llmsettings_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/jaimegago/joe/internal/agentloop"
	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/llmsettings"
	"github.com/jaimegago/joe/internal/llmusage"
	"github.com/jaimegago/joe/internal/store"
)

// TestCostLimitsProvider_StoredValueWins exercises the read path: when
// the operator has written a non-zero threshold via the mutation
// service, the storage-backed provider returns it verbatim and does
// NOT fall back to the hardcoded backstop.
func TestCostLimitsProvider_StoredValueWins(t *testing.T) {
	s := freshStore(t)
	repo := llmsettings.NewRepository(s.DB(), store.DriverSQLite)
	auditRepo := audit.NewRepository(s.DB(), store.DriverSQLite)
	svc := llmsettings.NewMutationService(repo, auditRepo)

	const stored = int64(7_000_000_000)
	if err := svc.SetCostLimit(context.Background(), llmsettings.WindowHourly, stored); err != nil {
		t.Fatalf("SetCostLimit: %v", err)
	}

	p := llmsettings.NewCostLimitsProvider(repo, llmusage.NewStaticCostLimits(), slog.Default())
	if got := p.HourlyLimitNano(); got != stored {
		t.Errorf("HourlyLimitNano = %d, want %d (stored value must win when non-zero)", got, stored)
	}
	// Daily/monthly remain at zero (unset), so they fall back to the
	// static backstop. This pins the documented "unset falls back to
	// hardcoded backstop, NOT unlimited" policy.
	if got, want := p.DailyLimitNano(), llmusage.NewStaticCostLimits().DailyLimitNano(); got != want {
		t.Errorf("DailyLimitNano = %d, want %d (unset must fall back to backstop, not 0 = unlimited)", got, want)
	}
	if got, want := p.MonthlyLimitNano(), llmusage.NewStaticCostLimits().MonthlyLimitNano(); got != want {
		t.Errorf("MonthlyLimitNano = %d, want %d (unset must fall back to backstop)", got, want)
	}
}

// TestCostLimitsProvider_UnsetFallsBackToBackstop is the focused
// regression guard for the launch policy: a stored ZERO (the migration
// seed, an explicit operator clear) MUST NOT be reinterpreted as
// "no limit". It falls back to the hardcoded backstop instead, so a
// freshly migrated system remains protected.
func TestCostLimitsProvider_UnsetFallsBackToBackstop(t *testing.T) {
	s := freshStore(t)
	repo := llmsettings.NewRepository(s.DB(), store.DriverSQLite)
	backstop := llmusage.NewStaticCostLimits()
	p := llmsettings.NewCostLimitsProvider(repo, backstop, slog.Default())

	if got, want := p.HourlyLimitNano(), backstop.HourlyLimitNano(); got != want {
		t.Errorf("HourlyLimitNano on fresh DB = %d, want backstop %d (unset must fall back, not mean unlimited)", got, want)
	}
	if got, want := p.DailyLimitNano(), backstop.DailyLimitNano(); got != want {
		t.Errorf("DailyLimitNano on fresh DB = %d, want backstop %d", got, want)
	}
	if got, want := p.MonthlyLimitNano(), backstop.MonthlyLimitNano(); got != want {
		t.Errorf("MonthlyLimitNano on fresh DB = %d, want backstop %d", got, want)
	}
}

// TestSessionLimitsProvider_StoredValueWins exercises the storage
// path: a stored non-zero ceiling overrides the hardcoded backstop.
func TestSessionLimitsProvider_StoredValueWins(t *testing.T) {
	s := freshStore(t)
	repo := llmsettings.NewRepository(s.DB(), store.DriverSQLite)
	auditRepo := audit.NewRepository(s.DB(), store.DriverSQLite)
	svc := llmsettings.NewMutationService(repo, auditRepo)

	const stored = 250_000
	if err := svc.SetRunawayCeiling(context.Background(), stored); err != nil {
		t.Fatalf("SetRunawayCeiling: %v", err)
	}

	p := llmsettings.NewSessionLimitsProvider(repo, agentloop.NewStaticSessionLimits(), slog.Default())
	if got := p.SessionTokenCeiling(); got != stored {
		t.Errorf("SessionTokenCeiling = %d, want %d", got, stored)
	}
}

// TestSessionLimitsProvider_UnsetFallsBackToBackstop pins the same
// launch policy for the runaway ceiling: a stored zero falls back to
// the hardcoded backstop rather than meaning "no limit".
func TestSessionLimitsProvider_UnsetFallsBackToBackstop(t *testing.T) {
	s := freshStore(t)
	repo := llmsettings.NewRepository(s.DB(), store.DriverSQLite)
	backstop := agentloop.NewStaticSessionLimits()
	p := llmsettings.NewSessionLimitsProvider(repo, backstop, slog.Default())

	if got, want := p.SessionTokenCeiling(), backstop.SessionTokenCeiling(); got != want {
		t.Errorf("SessionTokenCeiling on fresh DB = %d, want backstop %d (unset must fall back, not mean unlimited)", got, want)
	}
}
