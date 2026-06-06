package llmsettings_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/jaimegago/joe/internal/agentloop"
	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/llmsettings"
	"github.com/jaimegago/joe/internal/llmusage"
	"github.com/jaimegago/joe/internal/store"
)

// TestCostGate_ReadsLiveFromStorage proves the load-bearing G4
// integration property for the cost-window gate: it now reads its
// thresholds LIVE from storage on every Chat call, NOT from a
// constant baked at construction. The test:
//
//  1. Pre-seeds enough usage so the hourly window has accumulated 100
//     nano-units.
//  2. Writes a stored hourly threshold of 50 through the mutation
//     service — below the accumulated total. The gate must refuse.
//  3. Raises the stored hourly threshold to 1_000_000_000 through the
//     mutation service. The same gate, unchanged, must now allow.
//
// If the threshold were captured at construction time the third step
// would still refuse. If the gate read the hardcoded backstop directly
// the first step would not refuse (the backstop is far above 100).
// Both regressions are guarded.
func TestCostGate_ReadsLiveFromStorage(t *testing.T) {
	s := freshStore(t)
	llmUsage := llmusage.NewRepository(s.DB(), store.DriverSQLite)
	settingsRepo := llmsettings.NewRepository(s.DB(), store.DriverSQLite)
	auditRepo := audit.NewRepository(s.DB(), store.DriverSQLite)
	svc := llmsettings.NewMutationService(settingsRepo, auditRepo)
	provider := llmsettings.NewCostLimitsProvider(settingsRepo, llmusage.NewStaticCostLimits(), slog.Default())

	ctx := context.Background()

	// Compute the current hour window ONCE, up front, and reuse the same
	// bounds for both the seed and the SumCostNano assertion below so the
	// two can never disagree. Seed the row at hStart + a 1-minute margin
	// so it is guaranteed inside [hStart, hEnd) regardless of where the
	// wall clock sits in the hour.
	//
	// The earlier time.Now()-5m placement flaked: in the first 5 minutes
	// of a clock hour, now-5m falls in the PREVIOUS hour — outside
	// HourWindow(now) — so SumCostNano returned 0 and the "gate would
	// refuse" precondition (sum >= threshold) failed. Anchoring to hStart
	// removes the dependency on the wall-clock minute entirely.
	hStart, hEnd := llmusage.HourWindow(time.Now().UTC())

	// Pre-seed a usage row inside the current hour summing to 100
	// nano-units.
	if err := llmUsage.Insert(ctx, llmusage.Row{
		Timestamp:         hStart.Add(time.Minute),
		Model:             "claude-sonnet-4-20250514",
		Currency:          "USD",
		EstimatedCostNano: 100,
	}); err != nil {
		t.Fatalf("seed llm_usage: %v", err)
	}

	// Step 1 — threshold below accumulated sum: gate would refuse.
	// We reach for the gate's primitive (SumCostNano) plus the
	// provider's returned threshold to assert the relationship the
	// gate computes; the gate site itself is exercised end-to-end in
	// internal/llmusage's gate_test.go.
	if err := svc.SetCostLimit(ctx, llmsettings.WindowHourly, 50); err != nil {
		t.Fatalf("SetCostLimit(50): %v", err)
	}
	if got := provider.HourlyLimitNano(); got != 50 {
		t.Fatalf("provider hourly = %d, want 50 (stored value must be read live)", got)
	}
	sum, err := llmUsage.SumCostNano(ctx, hStart, hEnd, "USD")
	if err != nil {
		t.Fatalf("SumCostNano: %v", err)
	}
	if sum < provider.HourlyLimitNano() {
		t.Fatalf("pre-seed observed sum %d < threshold %d; the gate would not refuse — test setup is wrong", sum, provider.HourlyLimitNano())
	}

	// Step 2 — raise threshold high. The provider must reflect the
	// change on the NEXT call without any reconstruction.
	if err := svc.SetCostLimit(ctx, llmsettings.WindowHourly, 1_000_000_000); err != nil {
		t.Fatalf("SetCostLimit(1e9): %v", err)
	}
	if got := provider.HourlyLimitNano(); got != 1_000_000_000 {
		t.Fatalf("provider hourly after raise = %d, want 1_000_000_000 (live read should reflect the change)", got)
	}
	if sum >= provider.HourlyLimitNano() {
		t.Fatalf("after raise, observed sum %d still >= threshold %d; the gate would still refuse — provider did not pick up the change", sum, provider.HourlyLimitNano())
	}
}

// TestRunawayCeiling_ReadsLiveFromStorage is the SessionLimits
// equivalent of the test above: setting a low ceiling through the
// service makes the storage-backed provider report that ceiling on
// the next read; raising it through the service makes the next read
// report the new value.
func TestRunawayCeiling_ReadsLiveFromStorage(t *testing.T) {
	s := freshStore(t)
	settingsRepo := llmsettings.NewRepository(s.DB(), store.DriverSQLite)
	auditRepo := audit.NewRepository(s.DB(), store.DriverSQLite)
	svc := llmsettings.NewMutationService(settingsRepo, auditRepo)
	provider := llmsettings.NewSessionLimitsProvider(settingsRepo, agentloop.NewStaticSessionLimits(), slog.Default())

	ctx := context.Background()

	if err := svc.SetRunawayCeiling(ctx, 1_000); err != nil {
		t.Fatalf("SetRunawayCeiling(1000): %v", err)
	}
	if got := provider.SessionTokenCeiling(); got != 1_000 {
		t.Fatalf("ceiling after set = %d, want 1_000 (stored value must be read live)", got)
	}

	if err := svc.SetRunawayCeiling(ctx, 5_000_000); err != nil {
		t.Fatalf("SetRunawayCeiling(5_000_000): %v", err)
	}
	if got := provider.SessionTokenCeiling(); got != 5_000_000 {
		t.Fatalf("ceiling after raise = %d, want 5_000_000 (live read should reflect change)", got)
	}
}
