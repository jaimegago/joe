package safety

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestActionInfo_FormatBefore(t *testing.T) {
	info := ActionInfo{
		ToolName:    "write_file",
		Tier:        TierAct,
		Description: "Write file to local filesystem",
	}

	msg := info.FormatBefore()
	if !strings.Contains(msg, "[Joe]") {
		t.Errorf("FormatBefore() = %q, want [Joe] prefix", msg)
	}
	if !strings.Contains(msg, "write_file") {
		t.Errorf("FormatBefore() = %q, want tool name", msg)
	}
	if !strings.Contains(msg, "Write file") {
		t.Errorf("FormatBefore() = %q, want description", msg)
	}
}

func TestActionInfo_FormatAfter_Success(t *testing.T) {
	info := ActionInfo{
		ToolName:    "run_command",
		Tier:        TierAct,
		Description: "Run shell command",
	}

	msg := info.FormatAfter(nil)
	if !strings.Contains(msg, "[Joe] Done") {
		t.Errorf("FormatAfter(nil) = %q, want '[Joe] Done'", msg)
	}
	if !strings.Contains(msg, "run_command") {
		t.Errorf("FormatAfter(nil) = %q, want tool name", msg)
	}
}

func TestActionInfo_FormatAfter_Error(t *testing.T) {
	info := ActionInfo{
		ToolName:    "write_file",
		Tier:        TierAct,
		Description: "Write file to local filesystem",
	}

	msg := info.FormatAfter(errors.New("permission denied"))
	if !strings.Contains(msg, "[Joe] Failed") {
		t.Errorf("FormatAfter(err) = %q, want '[Joe] Failed'", msg)
	}
	if !strings.Contains(msg, "permission denied") {
		t.Errorf("FormatAfter(err) = %q, want error message", msg)
	}
}

func TestNoopNotifier(t *testing.T) {
	n := &NoopNotifier{}
	info := ActionInfo{ToolName: "test", Tier: TierAct}

	// NotifyBefore should return nil
	if err := n.NotifyBefore(context.Background(), info); err != nil {
		t.Errorf("NoopNotifier.NotifyBefore() = %v, want nil", err)
	}

	// NotifyAfter should not panic
	n.NotifyAfter(context.Background(), info, "result", nil)
	n.NotifyAfter(context.Background(), info, nil, errors.New("fail"))
}

func TestLogNotifier(t *testing.T) {
	var messages []string
	n := &LogNotifier{
		LogFunc: func(msg string) {
			messages = append(messages, msg)
		},
	}

	info := ActionInfo{
		ToolName:    "graph_add_node",
		Tier:        TierRecord,
		Description: "Add node to knowledge graph",
	}

	// NotifyBefore
	if err := n.NotifyBefore(context.Background(), info); err != nil {
		t.Errorf("LogNotifier.NotifyBefore() = %v, want nil", err)
	}

	// NotifyAfter
	n.NotifyAfter(context.Background(), info, "ok", nil)

	if len(messages) != 2 {
		t.Fatalf("LogNotifier logged %d messages, want 2", len(messages))
	}

	if !strings.Contains(messages[0], "[Joe]") {
		t.Errorf("Before message = %q, want [Joe] prefix", messages[0])
	}
	if !strings.Contains(messages[1], "[Joe] Done") {
		t.Errorf("After message = %q, want [Joe] Done", messages[1])
	}
}

func TestLogNotifier_NilFunc(t *testing.T) {
	n := &LogNotifier{LogFunc: nil}
	info := ActionInfo{ToolName: "test", Tier: TierAct}

	// Should not panic with nil LogFunc
	if err := n.NotifyBefore(context.Background(), info); err != nil {
		t.Errorf("LogNotifier.NotifyBefore() with nil func = %v, want nil", err)
	}
	n.NotifyAfter(context.Background(), info, nil, nil)
}
