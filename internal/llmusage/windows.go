package llmusage

import "time"

// Stream G phase G3b — cost-window boundary computation.
//
// Three UTC time windows partition the recent past for the cost gate:
// hourly, daily, monthly. Each window is half-open [start, end) so
// consecutive windows partition cleanly (the upper bound of one window
// equals the lower bound of the next, and the SumCostNano range filter
// uses created_at >= start AND created_at < end). All bounds are
// computed in UTC because llm_usage.created_at is written in UTC by the
// repository.
//
// The functions here are pure and side-effect-free. They take the
// reference time `now` as an argument so tests can drive them against
// fixed instants without touching time.Now; the recorder passes
// time.Now().UTC() at the call site.

// HourWindow returns [start, end) for the UTC hour containing `now`.
// start is `now` truncated to the hour; end is one hour later. The
// upper bound is exclusive — a row stamped exactly at `end` belongs to
// the NEXT window, never this one.
func HourWindow(now time.Time) (start, end time.Time) {
	u := now.UTC()
	start = time.Date(u.Year(), u.Month(), u.Day(), u.Hour(), 0, 0, 0, time.UTC)
	end = start.Add(time.Hour)
	return start, end
}

// DayWindow returns [start, end) for the UTC day containing `now`.
// start is midnight UTC on `now`'s date; end is midnight UTC the
// following day. The exclusive upper bound means a row stamped exactly
// at the next day's midnight is excluded — it belongs to the next day's
// window.
func DayWindow(now time.Time) (start, end time.Time) {
	u := now.UTC()
	start = time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
	end = start.AddDate(0, 0, 1)
	return start, end
}

// MonthWindow returns [start, end) for the UTC calendar month
// containing `now`. start is the first day of that month at midnight
// UTC; end is the first day of the NEXT month at midnight UTC.
//
// The next-month computation uses time.Date with month+1 and day 1, and
// relies on the standard library's documented normalization: when month
// is 13, time.Date rolls it to January of the following year (and day 32
// rolls to the next month, etc.). This is what makes the December→January
// rollover correct without a special case here — the test
// TestMonthWindow_DecemberRollsToJanuary pins the invariant.
func MonthWindow(now time.Time) (start, end time.Time) {
	u := now.UTC()
	start = time.Date(u.Year(), u.Month(), 1, 0, 0, 0, 0, time.UTC)
	end = time.Date(u.Year(), u.Month()+1, 1, 0, 0, 0, 0, time.UTC)
	return start, end
}
