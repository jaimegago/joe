package safety

import (
	"errors"
	"fmt"
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
