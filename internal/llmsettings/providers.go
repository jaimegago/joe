package llmsettings

import (
	"context"
	"log/slog"

	"github.com/jaimegago/joe/internal/agentloop"
	"github.com/jaimegago/joe/internal/llmusage"
)

// CostLimitsProvider satisfies the existing llmusage.CostLimits
// interface verbatim — same three methods, same int64 return types,
// same "zero or below disables" convention — by reading the stored
// per-window thresholds from the settings repository.
//
// Backstop fall-back. A stored value of zero (the migration seed,
// meaning "unset") is REINTERPRETED here as the conservative hardcoded
// default from llmusage.StaticCostLimits, not as "no limit". The
// package doc states why: a freshly migrated system stays protected by
// the same backstop the prior phase installed, and an operator opts
// in to a different limit (including off-by-explicit-negative) through
// the mutation service. If the operator does explicitly clear a window
// by writing a negative value through the mutation service, that
// negative value flows through unchanged and the existing
// "zero-or-below disables" convention in
// RecorderAdapter.Chat / .gate trips the disable path.
//
// Read failures are logged at warn level and fall back to the
// hardcoded backstop too. This is consistent with the cost-window
// gate's own "fail-open, loud" posture on its aggregation read: a
// settings-read blip must not silently turn the limit off, and the
// gate site itself remains the authoritative point of decision.
type CostLimitsProvider struct {
	repo     Repository
	backstop llmusage.CostLimits
	logger   *slog.Logger
}

// NewCostLimitsProvider builds a storage-backed CostLimits provider.
// backstop is the hardcoded provider returned on a zero stored value
// or a read failure; the caller passes llmusage.NewStaticCostLimits()
// in production so the documented "unset falls back to the hardcoded
// backstop" policy holds at the wire site.
func NewCostLimitsProvider(repo Repository, backstop llmusage.CostLimits, logger *slog.Logger) *CostLimitsProvider {
	if logger == nil {
		logger = slog.Default()
	}
	if backstop == nil {
		backstop = llmusage.NewStaticCostLimits()
	}
	return &CostLimitsProvider{repo: repo, backstop: backstop, logger: logger}
}

func (p *CostLimitsProvider) readAll(ctx context.Context) CostLimitValues {
	v, err := p.repo.ReadCostLimits(ctx)
	if err != nil {
		// Fall back to the hardcoded backstop. Log loud so an
		// operator notices a persistent storage outage; do not let a
		// read error silently disable the cap.
		p.logger.Warn("llmsettings: cost-limits read failed; falling back to hardcoded backstop",
			"error", err,
		)
		return CostLimitValues{}
	}
	return v
}

// HourlyLimitNano returns the stored hourly threshold, or the
// hardcoded backstop's hourly value when the stored value is zero
// (unset) or the read failed.
func (p *CostLimitsProvider) HourlyLimitNano() int64 {
	v := p.readAll(context.Background())
	if v.HourlyNano == 0 {
		return p.backstop.HourlyLimitNano()
	}
	return v.HourlyNano
}

// DailyLimitNano — same semantics for the daily window.
func (p *CostLimitsProvider) DailyLimitNano() int64 {
	v := p.readAll(context.Background())
	if v.DailyNano == 0 {
		return p.backstop.DailyLimitNano()
	}
	return v.DailyNano
}

// MonthlyLimitNano — same semantics for the monthly window.
func (p *CostLimitsProvider) MonthlyLimitNano() int64 {
	v := p.readAll(context.Background())
	if v.MonthlyNano == 0 {
		return p.backstop.MonthlyLimitNano()
	}
	return v.MonthlyNano
}

// Compile-time check: CostLimitsProvider satisfies the existing
// llmusage.CostLimits interface. The recorder treats it as a drop-in
// replacement for StaticCostLimits — the check site in
// RecorderAdapter.Chat / .gate is unchanged.
var _ llmusage.CostLimits = (*CostLimitsProvider)(nil)

// SessionLimitsProvider satisfies the existing agentloop.SessionLimits
// interface verbatim by reading the stored session token ceiling from
// the settings repository.
//
// Backstop fall-back. Same policy as the cost-limits provider above:
// a stored zero (the migration seed, "unset") is reinterpreted as the
// hardcoded backstop value from agentloop.StaticSessionLimits. Read
// failures log warn and fall back to the backstop. A stored negative
// value (an explicit operator clear through the mutation service)
// flows through unchanged; the existing "zero-or-below disables"
// convention in agentloop.Agent.Run trips the disable path.
//
// The agentloop constructs SessionLimits per task in
// internal/api/tasks.go's buildTaskRun, so this provider must be safe
// to call from many concurrent tasks. The repository read is the only
// shared state, and database/sql is safe for concurrent use.
type SessionLimitsProvider struct {
	repo     Repository
	backstop agentloop.SessionLimits
	logger   *slog.Logger
}

// NewSessionLimitsProvider builds a storage-backed SessionLimits
// provider. backstop is the hardcoded provider returned on a zero
// stored value or a read failure.
func NewSessionLimitsProvider(repo Repository, backstop agentloop.SessionLimits, logger *slog.Logger) *SessionLimitsProvider {
	if logger == nil {
		logger = slog.Default()
	}
	if backstop == nil {
		backstop = agentloop.NewStaticSessionLimits()
	}
	return &SessionLimitsProvider{repo: repo, backstop: backstop, logger: logger}
}

// SessionTokenCeiling returns the stored ceiling, or the hardcoded
// backstop value when the stored value is zero (unset) or the read
// failed. The agentloop's check site treats non-positive returns as
// "disabled"; a negative stored value would flow through unchanged.
func (p *SessionLimitsProvider) SessionTokenCeiling() int {
	v, err := p.repo.ReadRunawayCeiling(context.Background())
	if err != nil {
		p.logger.Warn("llmsettings: runaway ceiling read failed; falling back to hardcoded backstop",
			"error", err,
		)
		return p.backstop.SessionTokenCeiling()
	}
	if v == 0 {
		return p.backstop.SessionTokenCeiling()
	}
	return v
}

// Compile-time check: SessionLimitsProvider satisfies the existing
// agentloop.SessionLimits interface.
var _ agentloop.SessionLimits = (*SessionLimitsProvider)(nil)
