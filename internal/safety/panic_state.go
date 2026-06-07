package safety

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

const panicStateFile = "panic.state"

// PanicState is persisted to ~/.joe/panic.state when an emergency shutdown is
// triggered. On the next startup joecored reads this file and boots in safe mode.
type PanicState struct {
	TriggeredAt   time.Time   `yaml:"triggered_at"`
	TriggerSource PanicSource `yaml:"trigger_source"`
	TriggerReason string      `yaml:"trigger_reason,omitempty"`
}

// WritePanicState persists the panic state to <joeDir>/panic.state (mode 0600).
func WritePanicState(joeDir string, state PanicState) error {
	data, err := yaml.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal panic state: %w", err)
	}
	path := filepath.Join(joeDir, panicStateFile)
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write panic state: %w", err)
	}
	return nil
}

// ReadPanicState reads the panic state file from <joeDir>/panic.state.
// Returns (nil, nil) when no panic.state file exists (normal startup).
func ReadPanicState(joeDir string) (*PanicState, error) {
	path := filepath.Join(joeDir, panicStateFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read panic state: %w", err)
	}
	var state PanicState
	if err := yaml.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse panic state: %w", err)
	}
	return &state, nil
}

// ClearPanicState removes the panic.state file. Returns nil if the file did not exist.
func ClearPanicState(joeDir string) error {
	path := filepath.Join(joeDir, panicStateFile)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("clear panic state: %w", err)
	}
	return nil
}

// AcknowledgePanic clears the persisted panic.state file as a deliberate
// operator acknowledgment (D-0018 point 4). It is a LOCAL-FILE-ONLY host
// operation: it does NOT contact or signal any running process, does NOT touch
// any live write floor, and does NOT reference the floor value at all — it edits
// the persisted state file only. The cleared floor takes effect on the next
// restart; until then a running Joe stays read-only because the floor was sealed
// at boot and is never re-derived from disk mid-process. The reason is mandatory
// and recorded to the audit log as the acknowledgment trail.
func AcknowledgePanic(joeDir, reason string) error {
	if reason == "" {
		return fmt.Errorf("acknowledgment requires a non-empty reason for the audit log")
	}
	if err := ClearPanicState(joeDir); err != nil {
		return err
	}
	slog.Info("panic state acknowledged and cleared",
		"reason", reason,
		"timestamp", time.Now().UTC().Format(time.RFC3339),
		"note", "takes effect on restart; Joe stays read-only until restarted",
	)
	return nil
}
