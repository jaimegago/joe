package prompts

import (
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/safety"
)

// TestPostureSection_Observation asserts the observation floor reason yields the
// observation posture and that it carries NO recovery/unlock language (observation
// is the intended default — there is nothing for the user to fix).
func TestPostureSection_Observation(t *testing.T) {
	got := PostureSection(safety.FloorReasonObservation)
	if !strings.Contains(got, "observation mode") {
		t.Fatalf("observation posture missing 'observation mode' framing:\n%s", got)
	}
	if !strings.Contains(got, "will not make") {
		t.Fatalf("observation posture missing the refusal stance:\n%s", got)
	}
	lower := strings.ToLower(got)
	for _, forbidden := range []string{"unlock", "joe unlock", "restore", "see docs", "to clear", "fix it"} {
		if strings.Contains(lower, forbidden) {
			t.Errorf("observation posture must not contain recovery/unlock language %q:\n%s", forbidden, got)
		}
	}
}

// TestPostureSection_SafeMode asserts the safe_mode floor reason yields the safe
// mode posture and — the load-bearing guard — that it contains NO user-directed
// unlock instruction. Recovery is an operator action and already lives in the
// reactive denial UI message; the prompt must not duplicate or contradict it.
func TestPostureSection_SafeMode(t *testing.T) {
	got := PostureSection(safety.FloorReasonSafeMode)
	if !strings.Contains(got, "safe mode") {
		t.Fatalf("safe mode posture missing 'safe mode' framing:\n%s", got)
	}
	if !strings.Contains(got, "operator") {
		t.Fatalf("safe mode posture must frame restoration as an operator action:\n%s", got)
	}
	lower := strings.ToLower(got)
	for _, forbidden := range []string{"unlock", "joe unlock", "run joe unlock", "see docs", "docs to restore"} {
		if strings.Contains(lower, forbidden) {
			t.Errorf("safe mode posture must not contain user-directed unlock instruction %q:\n%s", forbidden, got)
		}
	}
}

// TestPostureSection_FullMode asserts full mode (FloorReasonNone) injects no
// posture line at all — full-mode write behaviour is governed by RBAC, not a
// prompt line.
func TestPostureSection_FullMode(t *testing.T) {
	if got := PostureSection(safety.FloorReasonNone); got != "" {
		t.Fatalf("full mode must inject no posture line, got:\n%s", got)
	}
}
