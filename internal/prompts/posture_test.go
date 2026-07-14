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
	if !strings.Contains(got, "do not attempt it") {
		t.Fatalf("observation posture missing the do-not-attempt refusal stance for mutations:\n%s", got)
	}
	lower := strings.ToLower(got)
	for _, forbidden := range []string{"unlock", "joe unlock", "restore", "see docs", "to clear", "fix it"} {
		if strings.Contains(lower, forbidden) {
			t.Errorf("observation posture must not contain recovery/unlock language %q:\n%s", forbidden, got)
		}
	}
}

// TestPostureSection_ObservationReadsUnaffected pins the load-bearing property of
// the observation wording (Session: posture-prompt-conflation): the floor gates
// Mutates only, so the prompt must say reads are unaffected and must not give the
// model any licence to defer a read to the operator. The prior wording's
// "offer the read-only investigation ... instead" clause did exactly that on a live
// cluster, so its absence is asserted explicitly.
func TestPostureSection_ObservationReadsUnaffected(t *testing.T) {
	got := PostureSection(safety.FloorReasonObservation)

	// Reads remain available and are explicitly out of the posture's scope.
	for _, required := range []string{
		"restricts mutations only",
		"read tools",
		"remain fully available and are unaffected by it",
		"carry the investigation to completion yourself using read tools",
	} {
		if !strings.Contains(got, required) {
			t.Errorf("observation posture must state that reads remain available (missing %q):\n%s", required, got)
		}
	}

	// No deferring reads to the operator on posture grounds.
	for _, required := range []string{
		"do not cite observation mode as a reason to stop reading",
		"hand read steps to the operator",
	} {
		if !strings.Contains(got, required) {
			t.Errorf("observation posture must forbid deferring reads on posture grounds (missing %q):\n%s", required, got)
		}
	}

	// The conflating clause must never come back.
	if strings.Contains(strings.ToLower(got), "offer the read-only investigation") {
		t.Errorf("observation posture must not offer investigation as a substitute for performing it:\n%s", got)
	}

	// Mutation refusal and the not-a-fault framing are retained.
	if !strings.Contains(got, "is out of bounds: do not attempt it") {
		t.Errorf("observation posture must retain an explicit do-not-attempt instruction for mutations:\n%s", got)
	}
	if !strings.Contains(got, "propose the specific change for the operator to make") {
		t.Errorf("observation posture must direct the model to propose the change to the operator:\n%s", got)
	}
	if !strings.Contains(got, "not a fault to be cleared") {
		t.Errorf("observation posture must retain the not-a-fault framing:\n%s", got)
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
