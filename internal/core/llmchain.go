package core

import (
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/llmusage"
)

// BuildLLMChain is the SINGLE construction site for the live LLM adapter
// chain. It wraps a freshly-built raw provider client in the
// usage-recording / cost-gating llmusage.RecorderAdapter, stamping the
// recorder's provider/model identity from mc and threading exactly the
// dependencies the boot assembly in cmd/joe/server.go threads. The server
// boot path AND both model-swap HTTP handlers (POST /api/v1/models/current
// and POST /api/v1/llm/settings/active-model) route through this one
// function, so a hot-swapped adapter carries identical recording and
// cost-gate enforcement to the boot adapter and the two cannot drift
// apart.
//
// Contract: inner MUST be the raw provider client returned by the LLM
// factory (llmfactory.NewAdapter via each caller's test seam) — never an
// already-wrapped adapter. The chain is built from the raw client up, so
// every caller passes factory output exactly once and the recorder is
// never wrapped around another recorder (no double counting, no
// double gating).
//
// Dependency sourcing mirrors the boot wire site exactly:
//
//   - Inner    — the raw client argument
//   - Repo     — s.LLMUsage (the llm_usage writer)
//   - Provider — mc.Provider   (NEW model's provider, post-swap identity)
//   - Model    — mc.Model      (NEW model's model id, post-swap identity)
//   - Currency — s.Config.LLM.Currency
//   - FXRate   — s.Config.LLM.USDToConfiguredRate
//   - Limits   — s.CostLimitsProvider (the SAME storage-backed instance the
//     gate enforces with; see Services.CostLimitsProvider)
//   - Audit    — s.Audit
//
// Costs and Logger are intentionally left unset so the recorder applies
// its built-in price table and slog.Default(), identical to boot (boot
// passes neither).
//
// Nil tolerance mirrors boot's degrade posture. Boot always has these
// dependencies present (they are unconditionally constructed before the
// adapter is built), so it wraps unconditionally. Where a dependency is
// genuinely absent — CostLimitsProvider nil in a partially-wired
// deployment — the nil interface lets the recorder fall back to its
// StaticCostLimits backstop, the same fail-safe posture documented on
// llmusage.NewRecorderAdapter. The CostLimitsProvider nil-check below
// guards against handing the recorder a non-nil interface wrapping a nil
// pointer, which would defeat that fallback.
func (s *Services) BuildLLMChain(inner llm.LLMAdapter, mc config.ModelConfig) llm.LLMAdapter {
	// Pass a true-nil interface (not a typed nil pointer) when the
	// provider is absent so NewRecorderAdapter's nil check substitutes
	// the StaticCostLimits backstop instead of calling methods on a nil
	// *llmsettings.CostLimitsProvider.
	var limits llmusage.CostLimits
	if s.CostLimitsProvider != nil {
		limits = s.CostLimitsProvider
	}

	cfg := llmusage.Config{
		Inner:    inner,
		Repo:     s.LLMUsage,
		Provider: mc.Provider,
		Model:    mc.Model,
		Limits:   limits,
		Audit:    s.Audit,
	}
	if s.Config != nil {
		cfg.Currency = s.Config.LLM.Currency
		cfg.FXRate = s.Config.LLM.USDToConfiguredRate
	}
	chain := llmusage.NewRecorderAdapter(cfg)

	// Wrap the recording / cost-gating chain in OpenTelemetry instrumentation
	// (LLM call/error/token/latency metrics + spans). This is the SINGLE chain
	// construction site shared by boot and both model-swap handlers, so every
	// live adapter — boot or hot-swapped — emits identical LLM observability.
	// Instrumentation is the OUTERMOST wrapper: it measures the whole chain end
	// to end and never sits between the recorder and the raw client, preserving
	// the recorder's raw-client contract. A nil logger applies slog.Default(),
	// mirroring the recorder's boot posture above.
	return llm.NewInstrumentedAdapter(chain, nil, mc.Provider, mc.Model)
}
