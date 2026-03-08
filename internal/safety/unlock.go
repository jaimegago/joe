package safety

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// Unlock exits safe mode by clearing the panic state file, clearing the
// cluster-wide DB panic state (if registered), and lifting the T1-only
// restriction. The reason field is mandatory — it is written to the audit log.
func Unlock(joeDir, reason string) error {
	if reason == "" {
		return fmt.Errorf("unlock requires a non-empty reason for the audit log")
	}

	if err := ClearPanicState(joeDir); err != nil {
		return fmt.Errorf("clear panic state: %w", err)
	}

	if clusterStore != nil {
		if err := clusterStore.ClearPanicked(context.Background()); err != nil {
			slog.Error("failed to clear cluster panic state", "error", err)
		}
	}

	DeactivateSafeMode()
	Reset()

	slog.Info("safe mode lifted",
		"reason", reason,
		"timestamp", time.Now().UTC().Format(time.RFC3339),
	)

	return nil
}
