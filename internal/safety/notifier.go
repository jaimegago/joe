package safety

import (
	"context"
	"fmt"
)

// ActionNotifier receives notifications before and after T2/T3 tool executions.
// Implementations control how the human is informed (REPL output, logging, etc.).
//
// The notification contract from security-in-layers.md:
//
//	T3 (Act):    BEFORE + AFTER (BEFORE is blocking — human can cancel)
//	T2 (Record): AFTER only (post-execution log)
//	T1 (Observe): no notification
type ActionNotifier interface {
	// NotifyBefore is called before a T3 action executes. For interactive
	// sessions (REPL), this should display a countdown and block for a
	// cancellation window. The implementation should respect ctx cancellation.
	// Returns an error if the action should be aborted (e.g., user cancelled).
	NotifyBefore(ctx context.Context, info ActionInfo) error

	// NotifyAfter is called after a T2 or T3 action executes, regardless of
	// whether it succeeded or failed. This is informational — the return
	// value is ignored by the executor.
	NotifyAfter(ctx context.Context, info ActionInfo, result any, execErr error)
}

// ActionInfo describes a tool execution for notification purposes.
type ActionInfo struct {
	ToolName    string
	Tier        ActionTier
	Description string         // human-readable, from ToolClassification
	Args        map[string]any // the arguments passed to the tool
}

// FormatBefore returns the pre-execution notification message.
func (a ActionInfo) FormatBefore() string {
	return fmt.Sprintf("[Joe] I'm about to %s: %s", a.Description, a.ToolName)
}

// FormatAfter returns the post-execution notification message.
func (a ActionInfo) FormatAfter(execErr error) string {
	if execErr != nil {
		return fmt.Sprintf("[Joe] Failed: %s (%v)", a.ToolName, execErr)
	}
	return fmt.Sprintf("[Joe] Done: %s", a.ToolName)
}

// NoopNotifier does nothing. Used in tests and non-interactive contexts.
type NoopNotifier struct{}

func (n *NoopNotifier) NotifyBefore(_ context.Context, _ ActionInfo) error { return nil }
func (n *NoopNotifier) NotifyAfter(_ context.Context, _ ActionInfo, _ any, _ error) {
}

// LogNotifier logs T2/T3 actions to a callback function. Useful for the Core
// Agent (joecored) where there's no REPL but we still want audit logging.
type LogNotifier struct {
	LogFunc func(msg string)
}

func (n *LogNotifier) NotifyBefore(_ context.Context, info ActionInfo) error {
	if n.LogFunc != nil {
		n.LogFunc(info.FormatBefore())
	}
	return nil
}

func (n *LogNotifier) NotifyAfter(_ context.Context, info ActionInfo, _ any, execErr error) {
	if n.LogFunc != nil {
		n.LogFunc(info.FormatAfter(execErr))
	}
}
