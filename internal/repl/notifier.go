package repl

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/jaimegago/joe/internal/safety"
)

// DefaultT3Delay is the countdown before a T3 action executes. During this
// window the user can press Ctrl+C (cancel context) to abort.
const DefaultT3Delay = 3 * time.Second

// Notifier implements safety.ActionNotifier for interactive REPL sessions.
// T3 actions get a blocking countdown with cancellation window.
// T2 actions get a post-execution log line.
type Notifier struct {
	Out   io.Writer     // output writer (default: os.Stdout)
	Delay time.Duration // T3 pre-execution delay (default: 3s)
}

// NewNotifier creates a REPL notifier that writes to stdout.
func NewNotifier() *Notifier {
	return &Notifier{
		Out:   os.Stdout,
		Delay: DefaultT3Delay,
	}
}

// NotifyBefore displays a pre-execution warning for T3 actions and blocks for
// the cancellation window. Returns an error if the context is cancelled during
// the wait (user pressed Ctrl+C).
func (n *Notifier) NotifyBefore(ctx context.Context, info safety.ActionInfo) error {
	if info.Tier != safety.TierAct {
		return nil // only T3 gets pre-execution notification
	}

	out := n.out()
	delay := n.delay()

	fmt.Fprintf(out, "\n%s. Proceeding in %s... (Ctrl+C to cancel)\n", info.FormatBefore(), delay)

	select {
	case <-time.After(delay):
		return nil // countdown finished, proceed
	case <-ctx.Done():
		fmt.Fprintf(out, "[Joe] Cancelled.\n")
		return ctx.Err()
	}
}

// NotifyAfter displays a post-execution summary for T2 and T3 actions.
func (n *Notifier) NotifyAfter(_ context.Context, info safety.ActionInfo, _ any, execErr error) {
	out := n.out()
	fmt.Fprintf(out, "%s\n", info.FormatAfter(execErr))
}

func (n *Notifier) out() io.Writer {
	if n.Out != nil {
		return n.Out
	}
	return os.Stdout
}

func (n *Notifier) delay() time.Duration {
	if n.Delay > 0 {
		return n.Delay
	}
	return DefaultT3Delay
}
