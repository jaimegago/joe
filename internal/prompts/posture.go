package prompts

import "github.com/jaimegago/joe/internal/safety"

// observationPosture is the system-prompt section injected when the boot-resolved
// write floor is up for FloorReasonObservation (D-0019). Observation is Joe's
// intended read-only resting posture, so the text frames it as the intended
// default — it carries NO recovery or "unlock" guidance, because there is nothing
// for the user to fix.
//
// The wording draws the read/mutate boundary explicitly (Session:
// posture-prompt-conflation). The prior text told the model to "offer the
// read-only investigation ... instead" when declining a change; a weak model read
// that literally on a live cluster and deferred log, event, and spec READS to the
// operator, citing observation mode. The floor gates Mutates only
// (internal/tools/executor.go), so that deferral was pure prompt conflation. The
// text must therefore say, unambiguously, that reads are unaffected — while
// keeping the mutation refusal at least as strong as before.
const observationPosture = `CURRENT POSTURE — OBSERVATION MODE (READ-ONLY):
You are running in observation mode, Joe's intended read-only posture. This posture restricts mutations only. All read tools — logs, events, resource specs, status, queries, inspection of any kind — remain fully available and are unaffected by it. When investigating, carry the investigation to completion yourself using read tools; do not cite observation mode as a reason to stop reading, to skip a read, or to hand read steps to the operator. Only a change to a managed system (apply, scale, delete, restart, edit, publish, or any other mutation) is out of bounds: do not attempt it — state your diagnosis and propose the specific change for the operator to make. This is the intended resting state, not a fault to be cleared.`

// safeModePosture is the system-prompt section injected when the boot-resolved
// write floor is up for FloorReasonSafeMode (D-0019) — a sticky panic state was
// present at boot. The wording differs from observation: it frames restoration as
// an operator action and deliberately instructs the model NOT to direct the user
// to clear the state or run any command. Recovery guidance (operator-side, restart)
// already lives in the reactive denial UI message; the system prompt must not
// duplicate or contradict it.
const safeModePosture = `CURRENT POSTURE — SAFE MODE (EMERGENCY HALT):
You are currently in safe mode following an emergency halt. You will not make any changes to managed systems until normal operation is restored by an operator. You can still read, inspect, query, and explain. When a request would change a managed system, do not attempt the change: explain that you are in safe mode and will not make it. Restoring normal operation is an operator action — do not direct the user to clear this state or to run any command to lift it.`

// PostureSection returns the conditional posture section appended to the task
// system prompt, selected by the boot-resolved write floor's reason (D-0019). It
// tells the model its current posture so it can decline managed-system writes
// proactively with articulation, rather than only reacting after the floor denies
// a tool call at execution. It changes neither the advertised tool surface nor
// enforcement — the floor still denies every Mutate regardless of what the model
// does. Full mode (FloorReasonNone) returns the empty string: full-mode write
// behaviour is governed by RBAC, not a prompt line.
func PostureSection(reason safety.FloorReason) string {
	switch reason {
	case safety.FloorReasonObservation:
		return observationPosture
	case safety.FloorReasonSafeMode:
		return safeModePosture
	default:
		return ""
	}
}
