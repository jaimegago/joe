package autorun

import (
	"testing"

	"github.com/jaimegago/joe/internal/audit"
)

// TestRunTrigger_NamesWhatOpenedTheRun: every act of a pass that declares no
// trigger of its own is recorded against what opened the run's session
// (D-0171) — the operator's request for a run an operator triggered, the
// standing schedule for a run a schedule fired. Both are from the audit
// trigger vocabulary and both carry a reference the run record can resolve.
func TestRunTrigger_NamesWhatOpenedTheRun(t *testing.T) {
	cases := []struct {
		name string
		run  Run
		want audit.Trigger
	}{
		{
			name: "operator-triggered run",
			run:  Run{ID: "run-1", Activator: "user:alice@example.com"},
			want: audit.Trigger{Type: audit.TriggerOperatorRequest, Ref: "run/run-1"},
		},
		{
			name: "service-account-triggered run",
			run:  Run{ID: "run-2", Activator: "svc:e2e"},
			want: audit.Trigger{Type: audit.TriggerOperatorRequest, Ref: "run/run-2"},
		},
		{
			name: "schedule-fired run",
			run:  Run{ID: "run-3", Activator: scheduleActivatorPrefix + "sched-9"},
			want: audit.Trigger{Type: audit.TriggerPolicy, Ref: "schedule/sched-9"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := runTrigger(tc.run)
			if got != tc.want {
				t.Errorf("runTrigger = %+v, want %+v", got, tc.want)
			}
			if !audit.IsTriggerType(got.Type) || got.Ref == "" {
				t.Errorf("trigger %+v is not a vocabulary type with a reference", got)
			}
		})
	}
}
