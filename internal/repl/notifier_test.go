package repl

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jaimegago/joe/internal/safety"
)

func TestNotifier_NotifyBefore_T3_Countdown(t *testing.T) {
	var buf bytes.Buffer
	n := &Notifier{
		Out:   &buf,
		Delay: 10 * time.Millisecond, // very short for tests
	}

	info := safety.ActionInfo{
		ToolName:    "write_file",
		Tier:        safety.TierAct,
		Description: "Write file to local filesystem",
	}

	start := time.Now()
	err := n.NotifyBefore(context.Background(), info)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("NotifyBefore() = %v, want nil", err)
	}

	// Should have waited at least the delay
	if elapsed < 10*time.Millisecond {
		t.Errorf("NotifyBefore() returned in %v, want >= 10ms", elapsed)
	}

	output := buf.String()
	if !strings.Contains(output, "[Joe]") {
		t.Errorf("output = %q, want [Joe] prefix", output)
	}
	if !strings.Contains(output, "write_file") {
		t.Errorf("output = %q, want tool name", output)
	}
	if !strings.Contains(output, "Ctrl+C") {
		t.Errorf("output = %q, want cancellation hint", output)
	}
}

func TestNotifier_NotifyBefore_T3_Cancelled(t *testing.T) {
	var buf bytes.Buffer
	n := &Notifier{
		Out:   &buf,
		Delay: 5 * time.Second, // long delay — will be cancelled
	}

	info := safety.ActionInfo{
		ToolName:    "run_command",
		Tier:        safety.TierAct,
		Description: "Run shell command",
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after a short delay
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := n.NotifyBefore(ctx, info)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("NotifyBefore() = nil, want context cancelled error")
	}

	// Should have returned quickly (not waited full 5s)
	if elapsed > 1*time.Second {
		t.Errorf("NotifyBefore() took %v, should have been cancelled quickly", elapsed)
	}

	output := buf.String()
	if !strings.Contains(output, "Cancelled") {
		t.Errorf("output = %q, want cancellation message", output)
	}
}

func TestNotifier_NotifyBefore_T2_NoDelay(t *testing.T) {
	var buf bytes.Buffer
	n := &Notifier{
		Out:   &buf,
		Delay: 5 * time.Second,
	}

	info := safety.ActionInfo{
		ToolName:    "graph_add_node",
		Tier:        safety.TierRecord,
		Description: "Add node to knowledge graph",
	}

	start := time.Now()
	err := n.NotifyBefore(context.Background(), info)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("NotifyBefore() = %v, want nil", err)
	}

	// T2 should not block
	if elapsed > 100*time.Millisecond {
		t.Errorf("NotifyBefore() for T2 took %v, should be instant", elapsed)
	}

	// T2 should not produce pre-execution output
	if buf.Len() > 0 {
		t.Errorf("T2 NotifyBefore() produced output: %q, want empty", buf.String())
	}
}

func TestNotifier_NotifyBefore_T1_NoDelay(t *testing.T) {
	var buf bytes.Buffer
	n := &Notifier{
		Out:   &buf,
		Delay: 5 * time.Second,
	}

	info := safety.ActionInfo{
		ToolName:    "read_file",
		Tier:        safety.TierObserve,
		Description: "Read file contents",
	}

	err := n.NotifyBefore(context.Background(), info)
	if err != nil {
		t.Fatalf("NotifyBefore() = %v, want nil", err)
	}
	if buf.Len() > 0 {
		t.Errorf("T1 NotifyBefore() produced output: %q, want empty", buf.String())
	}
}

func TestNotifier_NotifyAfter_Success(t *testing.T) {
	var buf bytes.Buffer
	n := &Notifier{Out: &buf, Delay: 1 * time.Millisecond}

	info := safety.ActionInfo{
		ToolName:    "write_file",
		Tier:        safety.TierAct,
		Description: "Write file to local filesystem",
	}

	n.NotifyAfter(context.Background(), info, "ok", nil)

	output := buf.String()
	if !strings.Contains(output, "[Joe] Done") {
		t.Errorf("output = %q, want '[Joe] Done'", output)
	}
}

func TestNotifier_NotifyAfter_Error(t *testing.T) {
	var buf bytes.Buffer
	n := &Notifier{Out: &buf, Delay: 1 * time.Millisecond}

	info := safety.ActionInfo{
		ToolName:    "run_command",
		Tier:        safety.TierAct,
		Description: "Run shell command",
	}

	n.NotifyAfter(context.Background(), info, nil, context.DeadlineExceeded)

	output := buf.String()
	if !strings.Contains(output, "[Joe] Failed") {
		t.Errorf("output = %q, want '[Joe] Failed'", output)
	}
}

func TestNewNotifier_Defaults(t *testing.T) {
	n := NewNotifier()
	if n.Out == nil {
		t.Error("NewNotifier().Out = nil, want os.Stdout")
	}
	if n.Delay != DefaultT3Delay {
		t.Errorf("NewNotifier().Delay = %v, want %v", n.Delay, DefaultT3Delay)
	}
}
