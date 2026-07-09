package safety

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestActionInfo_FormatBefore(t *testing.T) {
	info := ActionInfo{
		ToolName:    "github_comment",
		Class:       ActionMutate,
		Description: "Post a review comment on a GitHub pull request",
	}
	msg := info.FormatBefore()
	for _, want := range []string{"[Joe]", "github_comment", "Post a review"} {
		if !strings.Contains(msg, want) {
			t.Errorf("FormatBefore() = %q, want to contain %q", msg, want)
		}
	}
}

func TestActionInfo_FormatAfter(t *testing.T) {
	tests := []struct {
		name         string
		err          error
		wantContains string
	}{
		{
			name:         "success",
			err:          nil,
			wantContains: "[Joe] Done",
		},
		{
			name:         "error",
			err:          errors.New("permission denied"),
			wantContains: "[Joe] Failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := ActionInfo{ToolName: "publish_doc_update_git", Class: ActionMutate, Description: "Commit and push doc proposal to Git repo"}
			msg := info.FormatAfter(tt.err)
			if !strings.Contains(msg, tt.wantContains) {
				t.Errorf("FormatAfter() = %q, want to contain %q", msg, tt.wantContains)
			}
			if !strings.Contains(msg, "publish_doc_update_git") {
				t.Errorf("FormatAfter() = %q, want to contain tool name", msg)
			}
			if tt.err != nil && !strings.Contains(msg, tt.err.Error()) {
				t.Errorf("FormatAfter() = %q, want to contain error %q", msg, tt.err.Error())
			}
		})
	}
}

func TestNoopNotifier(t *testing.T) {
	n := &NoopNotifier{}
	info := ActionInfo{ToolName: "test", Class: ActionMutate}

	if err := n.NotifyBefore(context.Background(), info); err != nil {
		t.Errorf("NotifyBefore() = %v, want nil", err)
	}
	// Should not panic with either nil or non-nil error.
	n.NotifyAfter(context.Background(), info, "result", nil)
	n.NotifyAfter(context.Background(), info, nil, errors.New("fail"))
}

func TestLogNotifier(t *testing.T) {
	info := ActionInfo{
		ToolName:    "graph_add_node",
		Class:       ActionMutate,
		Description: "Add node to knowledge graph",
	}

	tests := []struct {
		name       string
		nilFunc    bool
		wantMsgs   int
		wantBefore string
		wantAfter  string
	}{
		{
			name:       "logs before and after on success",
			wantMsgs:   2,
			wantBefore: "[Joe]",
			wantAfter:  "[Joe] Done",
		},
		{
			name:     "nil log func does not panic",
			nilFunc:  true,
			wantMsgs: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var messages []string
			var logFunc func(string)
			if !tt.nilFunc {
				logFunc = func(msg string) { messages = append(messages, msg) }
			}

			n := &LogNotifier{LogFunc: logFunc}
			if err := n.NotifyBefore(context.Background(), info); err != nil {
				t.Errorf("NotifyBefore() = %v, want nil", err)
			}
			n.NotifyAfter(context.Background(), info, "ok", nil)

			if len(messages) != tt.wantMsgs {
				t.Fatalf("logged %d messages, want %d", len(messages), tt.wantMsgs)
			}
			if tt.wantBefore != "" && !strings.Contains(messages[0], tt.wantBefore) {
				t.Errorf("before message = %q, want to contain %q", messages[0], tt.wantBefore)
			}
			if tt.wantAfter != "" && !strings.Contains(messages[1], tt.wantAfter) {
				t.Errorf("after message = %q, want to contain %q", messages[1], tt.wantAfter)
			}
		})
	}
}
