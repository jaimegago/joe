package api

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jaimegago/joe/internal/access"
	"github.com/jaimegago/joe/internal/captaingate"
	"github.com/jaimegago/joe/internal/safety"
	"github.com/jaimegago/joe/internal/tools"
)

// TestClassifyWriteFailure covers each typed tool-failure branch the chat UI
// dispatches on (Item 8). The classifier must distinguish a captain-gate
// incident-mode refusal from an executor scope refusal from an RBAC zone
// denial, and must NOT mislabel an ordinary tool error as a denial.
//
// The scope cases are the load-bearing ones for the harness: both executor
// scope checks must resolve to scope_denial and NOT to zone_denial, because
// downstream the two mean different things — no grant versus a session scoped
// to exclude the target — and a harness that reads the code cannot recover the
// distinction once it is collapsed.
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
			name: "executor zone scope violation → scope_denial",
			err: &tools.ZoneViolationError{
				ToolName:       "k8s_get",
				ComponentID:    "prod-cluster",
				ZoneNames:      "staging",
				TargetZoneName: "production",
			},
			want: errorCodeScopeDenial,
		},
		{
			name: "executor zone scope violation wrapped → scope_denial",
			err: fmt.Errorf("tool failed: %w", &tools.ZoneViolationError{
				ToolName:    "k8s_get",
				ComponentID: "prod-cluster",
			}),
			want: errorCodeScopeDenial,
		},
		{
			name: "executor namespace scope violation → scope_denial",
			err: &tools.NamespaceViolationError{
				ToolName:          "k8s_get",
				Namespace:         "kube-system",
				AllowedNamespaces: []string{"frontend"},
				ZoneNames:         "staging",
				TargetZoneName:    "production",
			},
			want: errorCodeScopeDenial,
		},
		{
			name: "executor namespace scope violation wrapped → scope_denial",
			err: fmt.Errorf("tool failed: %w", &tools.NamespaceViolationError{
				ToolName:          "k8s_get",
				Namespace:         "kube-system",
				AllowedNamespaces: []string{"frontend"},
			}),
			want: errorCodeScopeDenial,
		},
		{
			name: "RBAC permission denied → zone_denial",
			err:  access.ErrPermissionDenied,
			want: errorCodeZoneDenial,
		},
		{
			name: "RBAC permission denied wrapped (inproc mapAccessError shape) → zone_denial",
			err:  fmt.Errorf("access denied for component %q: %w", "prod-db", access.ErrPermissionDenied),
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

// TestFirstWriteFailureCode_ScopeDenialWinsOverALaterFloorDenial pins the
// consequence of adding scope_denial, which is not visible from
// classifyWriteFailure alone.
//
// firstWriteFailureCode is first-non-empty, and scope_denial fires on a READ —
// the executor's scope check runs ahead of the action class, and the captain
// gate short-circuits reads straight to it (zone_denial reaches reads too; the
// floor codes do not). A read is typically the first thing a turn does, so in a
// zone-scoped session with the write floor up — the read-only posture — a
// scope-refused read now takes the turn-level slot that a floor-denied write
// used to hold, and every consumer of error_code sees scope_denial where it
// used to see observation.
//
// That is intended: first-non-empty is the contract and the earlier refusal is
// the one the user hit first. What it costs is that a consumer with no branch
// for scope_denial renders NOTHING where it used to render the observation
// notice. ui/src/hooks/writeFailureMessage.test.ts pins the other half — that
// every code reaching this field has a message.
func TestFirstWriteFailureCode_ScopeDenialWinsOverALaterFloorDenial(t *testing.T) {
	steps := []taskStep{
		{ToolResults: []taskToolResult{
			{Name: "k8s_get", Error: "ZONE BOUNDARY VIOLATION", ErrorCode: errorCodeScopeDenial},
		}},
		{ToolResults: []taskToolResult{
			{Name: "k8s_scale", Error: "refused", ErrorCode: errorCodeObservation},
		}},
	}
	if got := firstWriteFailureCode(steps); got != errorCodeScopeDenial {
		t.Errorf("firstWriteFailureCode = %q; want the earlier %q", got, errorCodeScopeDenial)
	}
}
