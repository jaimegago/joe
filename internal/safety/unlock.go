package safety

import (
	"fmt"
	"log/slog"
	"time"
)

// Unlock exits safe mode by clearing the panic state file and lifting the
// T1-only restriction. The reason field is mandatory — it is written to the
// audit log so operators can reconstruct what happened.
func Unlock(joeDir, reason string) error {
	if reason == "" {
		return fmt.Errorf("unlock requires a non-empty reason for the audit log")
	}

	if err := ClearPanicState(joeDir); err != nil {
		return fmt.Errorf("clear panic state: %w", err)
	}

	DeactivateSafeMode()
	Reset()

	slog.Info("safe mode lifted",
		"reason", reason,
		"timestamp", time.Now().UTC().Format(time.RFC3339),
	)

	return nil
}
