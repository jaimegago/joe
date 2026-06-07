package api

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jaimegago/joe/internal/access"
	"github.com/jaimegago/joe/internal/captaingate"
	"github.com/jaimegago/joe/internal/safety"
)

// TestClassifyWriteFailure covers each typed write-failure branch the chat UI
// dispatches on (Item 8). The classifier must distinguish a captain-gate
// incident-mode refusal from an RBAC zone denial, and must NOT mislabel an
// ordinary tool error as a denial.
func TestClassifyWriteFailure(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "captain gate refusal → incident_mode",
			err:  &captaingate.GateRefusalError{SessionID: "s1", Tool: "k8s_scale", CaptainSessionID: ""},
			want: errorCodeIncidentMode,
		},
		{
			name: "captain gate refusal wrapped → incident_mode",
			err:  fmt.Errorf("tool failed: %w", &captaingate.GateRefusalError{SessionID: "s1", Tool: "k8s_scale"}),
			want: errorCodeIncidentMode,
		},
		{
			name: "RBAC permission denied → zone_denial",
			err:  access.ErrPermissionDenied,
			want: errorCodeZoneDenial,
		},
		{
			name: "RBAC permission denied wrapped (inproc mapAccessError shape) → zone_denial",
			err:  fmt.Errorf("access denied for source %q: %w", "prod-db", access.ErrPermissionDenied),
			want: errorCodeZoneDenial,
		},
		{
			name: "write floor safe_mode → safe_mode",
			err:  &safety.WriteFloorError{Reason: safety.FloorReasonSafeMode},
			want: errorCodeSafeMode,
		},
		{
			name: "write floor safe_mode wrapped → safe_mode",
			err:  fmt.Errorf("tool failed: %w", &safety.WriteFloorError{Reason: safety.FloorReasonSafeMode}),
			want: errorCodeSafeMode,
		},
		{
			name: "write floor observation → observation",
			err:  &safety.WriteFloorError{Reason: safety.FloorReasonObservation},
			want: errorCodeObservation,
		},
		{
			name: "write floor observation wrapped → observation",
			err:  fmt.Errorf("tool failed: %w", &safety.WriteFloorError{Reason: safety.FloorReasonObservation}),
			want: errorCodeObservation,
		},
		{
			name: "ordinary tool error → no code",
			err:  errors.New("connection refused"),
			want: "",
		},
		{
			name: "nil → no code",
			err:  nil,
			want: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyWriteFailure(tc.err); got != tc.want {
				t.Errorf("classifyWriteFailure(%v) = %q; want %q", tc.err, got, tc.want)
			}
		})
	}
}

// TestFirstWriteFailureCode confirms the turn-level summary returns the first
// per-tool denial code across steps (a denied write does not terminate the
// loop, so the code rides on the otherwise-completed turn) and "" when no
// write was denied.
func TestFirstWriteFailureCode(t *testing.T) {
	none := []taskStep{
		{ToolResults: []taskToolResult{{Name: "k8s_get", Error: "transient"}}},
	}
	if got := firstWriteFailureCode(none); got != "" {
		t.Errorf("firstWriteFailureCode(no denial) = %q; want empty", got)
	}

	denied := []taskStep{
		{ToolResults: []taskToolResult{{Name: "k8s_get"}}},
		{ToolResults: []taskToolResult{
			{Name: "k8s_scale", Error: "refused", ErrorCode: errorCodeIncidentMode},
			{Name: "k8s_delete", Error: "refused", ErrorCode: errorCodeZoneDenial},
		}},
	}
	if got := firstWriteFailureCode(denied); got != errorCodeIncidentMode {
		t.Errorf("firstWriteFailureCode = %q; want first code %q", got, errorCodeIncidentMode)
	}
}
