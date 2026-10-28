// Package autorun is the Auto Joe run: joe analysing a fixed scope of the
// cluster without a human turn, as the read-only autonomous identity it already
// has (joe-pm queue/autojoe-ro.md and its Run branch queue/autojoe-ro-run.md,
// settled 2026-10-27; the build order docs/backlog/autojoe-ro.md).
//
// The package owns the run side of that build: the non-HTTP entry that starts
// the agent loop (Service.Trigger, reached by an operator act and by an
// installed schedule, and by nothing else), the loop carrying agent:core for
// the run's duration, the per-run budget seam with its reserved report
// headroom, and the run record. It does not own findings: what a run finds,
// how it is typed, ranked and rendered, is the report side's
// (joe-pm threads/autojoe-ro-report-build.md), and FindingsPipeline is the one
// seam where that side plugs in.
//
// Three invariants the design fixed and this package holds by construction:
//
//   - A run executes as svc:agent:core whoever switched it on. The activator is
//     recorded on the run and is never the principal; the session a run writes
//     is created by svc:agent:core, and that creator is what tells a run's
//     session from a person's.
//   - A run never widens its own authority or its own aim. Nothing here grants,
//     promotes, registers, or writes the graph; the scope is resolved once at
//     start, fixed on the record, and enforced on the executor.
//   - A run that hits a ceiling still reports. The pass is bounded by the
//     ceilings; the report is funded from a reserve outside them, and the
//     record names what was and was not looked at, with the ceiling as the
//     stop reason. Degrading to fit is not an option the code has.
package autorun

import (
	"sort"
	"strings"
	"time"
)

// Selection is the operator's scope: a graph selection — components, zones,
// and (when the field exists) labels on components — that a run resolves at
// start to a fixed component set. There is no prose in it, deliberately: the
// operator aims a run only structurally.
type Selection struct {
	Components []string `json:"components,omitempty"`
	Zones      []string `json:"zones,omitempty"`
}

// Normalize returns the selection with each list sorted and de-duplicated and
// blank entries dropped, so two selections naming the same things in a
// different order are one scope.
func (s Selection) Normalize() Selection {
	return Selection{
		Components: normalizeList(s.Components),
		Zones:      normalizeList(s.Zones),
	}
}

// Key is the canonical identity of the scope a selection names. It is what
// "at most one live run per scope" keys on: the partial unique index on
// auto_runs.scope_key holds one running row per key.
func (s Selection) Key() string {
	n := s.Normalize()
	return "components=" + strings.Join(n.Components, ",") + ";zones=" + strings.Join(n.Zones, ",")
}

// Empty reports a selection that names nothing.
func (s Selection) Empty() bool {
	n := s.Normalize()
	return len(n.Components) == 0 && len(n.Zones) == 0
}

func normalizeList(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, dup := seen[v]; dup {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// Budget is the per-run budget: the two ceilings that bound the pass and the
// reserve, held outside them, that funds the report when a ceiling fires. It
// is set from deployment defaults when a trigger fires or a schedule is
// installed, and recorded on the run so the record says what the run was
// allowed.
type Budget struct {
	TokenCeiling        int
	WallClock           time.Duration
	ReportReserveTokens int
	ReportReserve       time.Duration
}

// Run state vocabulary. A run that stopped on a ceiling is completed — it
// delivered the partial report it owes — with the ceiling as its StopReason;
// failed is reserved for a run that could not report at all.
const (
	StateRunning   = "running"
	StateCompleted = "completed"
	StateFailed    = "failed"
)

// Stop reasons beyond the loop's own (agentloop.StopReasonTokenCeiling,
// StopReasonWallClock, StopReasonMaxIterations, StopReasonCancelled).
const (
	// StopReasonError marks a run whose loop returned an error that is not one
	// of the ceilings; the record's Error carries it.
	StopReasonError = "error"
	// StopReasonInterrupted marks a run found still 'running' when the service
	// started — the process ended under it, so it reported nothing. The boot
	// sweep closes such rows so the scope is not held live forever.
	StopReasonInterrupted = "interrupted"
)

// Run is one row of auto_runs: the run record the design named — activator,
// resolved component set, both ceilings, coverage, stop reason, and the
// session the run wrote (docs/backlog/autojoe-ro.md § The run record).
// Delivery reads coverage and the stop reason from here, never from prose.
type Run struct {
	ID string `json:"id"`
	// Activator is who switched the run on: the operator principal, or
	// "schedule:<id>" for an installed schedule. Never the principal the run
	// executes as.
	Activator string `json:"activator"`
	// Principal is the identity the run executed as — always svc:agent:core.
	Principal string    `json:"principal"`
	Selection Selection `json:"selection"`
	ScopeKey  string    `json:"scope_key"`
	// ResolvedComponents is the fixed component set the selection resolved to
	// at run start. Two runs over one selection at different times compare on
	// this, not on the selection.
	ResolvedComponents  []string `json:"resolved_components"`
	TokenCeiling        int      `json:"token_ceiling"`
	WallClockSeconds    int      `json:"wall_clock_seconds"`
	ReportReserveTokens int      `json:"report_reserve_tokens"`
	State               string   `json:"state"`
	StopReason          string   `json:"stop_reason,omitempty"`
	// Covered and NotCovered partition ResolvedComponents by whether the pass
	// read the component. Derived by code from the tool calls the run made,
	// never asserted by the model.
	Covered    []string `json:"covered"`
	NotCovered []string `json:"not_covered"`
	// TokensUsed is the pass's spend against TokenCeiling; ReportTokens is what
	// the report call spent from the reserve, on top.
	TokensUsed   int `json:"tokens_used"`
	ReportTokens int `json:"report_tokens"`
	// SessionID is the session the run wrote, created by svc:agent:core.
	SessionID string     `json:"session_id,omitempty"`
	Error     string     `json:"error,omitempty"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
}

// Live reports whether the run still holds its scope.
func (r *Run) Live() bool { return r.State == StateRunning }

// Schedule is one row of auto_schedules: a standing trigger the operator
// installed over a scope. Its ceilings were copied from the deployment
// defaults at install time, so what it will cost per run is on the row.
type Schedule struct {
	ID                  string     `json:"id"`
	Selection           Selection  `json:"selection"`
	ScopeKey            string     `json:"scope_key"`
	IntervalSeconds     int        `json:"interval_seconds"`
	TokenCeiling        int        `json:"token_ceiling"`
	WallClockSeconds    int        `json:"wall_clock_seconds"`
	ReportReserveTokens int        `json:"report_reserve_tokens"`
	InstalledBy         string     `json:"installed_by"`
	InstalledAt         time.Time  `json:"installed_at"`
	NextRunAt           time.Time  `json:"next_run_at"`
	LastRunAt           *time.Time `json:"last_run_at,omitempty"`
	// LastOutcome is where a refusal is visible to a schedule as its
	// activator: "started <run id>", or "refused: ..." when a run on the scope
	// was still live at the tick.
	LastOutcome string `json:"last_outcome,omitempty"`
}

// Budget returns the schedule's recorded budget as a Budget.
func (s *Schedule) Budget() Budget {
	return Budget{
		TokenCeiling:        s.TokenCeiling,
		WallClock:           time.Duration(s.WallClockSeconds) * time.Second,
		ReportReserveTokens: s.ReportReserveTokens,
	}
}
