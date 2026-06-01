package llmsettings

import (
	"context"
	"log/slog"
)

// ResolveActiveModelOnStartup applies the Stream G phase G4 active-
// model startup precedence:
//
//  1. Read the stored active model from llm_settings (singleton row,
//     pre-seeded with empty string at migration 017 time).
//  2. If the stored value is NON-EMPTY and present in the supplied
//     availableKeys set, use it — log info naming the source.
//  3. If the stored value is non-empty but NOT in availableKeys, fall
//     back to configuredCurrent and log warn naming both the stale
//     stored value and the fall-back. Do NOT fail startup.
//  4. If the stored value is empty (the migration seed), fall back to
//     configuredCurrent silently — this is the expected fresh-install
//     state, not a warning condition.
//  5. If the repository read itself errors, fall back to
//     configuredCurrent and log warn — a settings-table outage must
//     not block startup.
//
// The returned key is suitable both for looking up cfg.LLM.Available
// to construct the inner adapter AND for seeding the
// SwappableAdapter's current key, so the live system's reported
// current and the table's record agree at boot.
//
// availableKeys is taken as a membership set rather than the full
// LLMConfig because this function does not need provider or model
// strings — only the "is this key configured?" question.
//
// This function is a pure resolver. It does not write the
// configuredCurrent back into the table when the stored value is
// empty, because doing so would conflate "first boot" with "operator
// preference". The first explicit model change through the mutation
// service is what writes the table; until then, configuration wins
// by default and an empty stored value remains the marker of "never
// set".
func ResolveActiveModelOnStartup(
	ctx context.Context,
	repo Repository,
	configuredCurrent string,
	availableKeys map[string]bool,
	logger *slog.Logger,
) string {
	if logger == nil {
		logger = slog.Default()
	}
	stored, err := repo.ReadActiveModel(ctx)
	if err != nil {
		logger.Warn("LLM settings: failed to read stored active model on startup; falling back to configured current model",
			"error", err,
			"configured_current", configuredCurrent,
		)
		return configuredCurrent
	}
	if stored == "" {
		return configuredCurrent
	}
	if availableKeys[stored] {
		logger.Info("LLM settings: using stored active model", "model", stored)
		return stored
	}
	logger.Warn("LLM settings: stored active model not present in configured available models; falling back to configured current model",
		"stored", stored,
		"configured_current", configuredCurrent,
	)
	return configuredCurrent
}
