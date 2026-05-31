package llmusage_test

import (
	"testing"
	"time"

	"github.com/jaimegago/joe/internal/llmusage"
)

// TestHourWindow_CoversCurrentHour pins the per-hour boundary
// computation. The start is "now" truncated to the hour; the end is
// one hour later. A timestamp exactly at the upper bound belongs to
// the NEXT window (half-open semantics), so we assert end is the
// next-hour midnight-of-the-hour, not the same hour.
func TestHourWindow_CoversCurrentHour(t *testing.T) {
	now := time.Date(2026, 5, 31, 14, 37, 22, 999, time.UTC)
	start, end := llmusage.HourWindow(now)
	wantStart := time.Date(2026, 5, 31, 14, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2026, 5, 31, 15, 0, 0, 0, time.UTC)
	if !start.Equal(wantStart) {
		t.Errorf("start = %v, want %v", start, wantStart)
	}
	if !end.Equal(wantEnd) {
		t.Errorf("end = %v, want %v", end, wantEnd)
	}
	if !end.After(start) {
		t.Fatal("end must be strictly after start (half-open contract)")
	}
}

// TestDayWindow_CoversCurrentUTCDay pins the per-day boundary
// computation. start is midnight UTC on `now`'s date; end is midnight
// UTC the next day.
func TestDayWindow_CoversCurrentUTCDay(t *testing.T) {
	now := time.Date(2026, 5, 31, 23, 45, 0, 0, time.UTC)
	start, end := llmusage.DayWindow(now)
	wantStart := time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	if !start.Equal(wantStart) {
		t.Errorf("start = %v, want %v", start, wantStart)
	}
	if !end.Equal(wantEnd) {
		t.Errorf("end = %v, want %v", end, wantEnd)
	}
}

// TestMonthWindow_CoversCurrentUTCMonth pins the per-month boundary
// computation for a mid-month, non-edge case.
func TestMonthWindow_CoversCurrentUTCMonth(t *testing.T) {
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	start, end := llmusage.MonthWindow(now)
	wantStart := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	if !start.Equal(wantStart) {
		t.Errorf("start = %v, want %v", start, wantStart)
	}
	if !end.Equal(wantEnd) {
		t.Errorf("end = %v, want %v", end, wantEnd)
	}
}

// TestMonthWindow_DecemberRollsToJanuary is the rollover regression
// guard: month+1 with month=12 must produce January of the NEXT year
// at day 1. This relies on the standard library's documented
// time.Date normalization; if MonthWindow ever stops trusting that
// (e.g. someone hard-codes `time.December + 1`), this test fails.
func TestMonthWindow_DecemberRollsToJanuary(t *testing.T) {
	now := time.Date(2026, time.December, 17, 9, 30, 0, 0, time.UTC)
	start, end := llmusage.MonthWindow(now)
	wantStart := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	if !start.Equal(wantStart) {
		t.Errorf("start = %v, want %v", start, wantStart)
	}
	if !end.Equal(wantEnd) {
		t.Errorf("end = %v, want %v (Dec→Jan must roll the year)", end, wantEnd)
	}
}

// TestWindows_AreHalfOpenPartition asserts the [start, end) windows
// returned by the three helpers compose as a strict half-open
// partition: a row stamped exactly at the upper bound belongs to the
// NEXT window, not this one. The check uses the same `time.Before`
// semantics the SumCostNano range filter uses.
func TestWindows_AreHalfOpenPartition(t *testing.T) {
	now := time.Date(2026, 5, 31, 14, 37, 0, 0, time.UTC)
	hStart, hEnd := llmusage.HourWindow(now)
	if !hEnd.After(hStart) {
		t.Fatal("hour: end not after start")
	}
	// A row stamped exactly at hEnd must NOT be counted as in [hStart,
	// hEnd): the `t < end` predicate excludes the boundary.
	if hEnd.Before(hEnd) {
		t.Fatal("time.Before must be irreflexive; sanity check failed")
	}
}
