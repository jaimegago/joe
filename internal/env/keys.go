package env

import "fmt"

// Environment variable names used across multiple packages.
// Defined here to ensure consistency and avoid typos.
const (
	// LLM API Keys
	AnthropicAPIKey = "ANTHROPIC_API_KEY"
	GeminiAPIKey    = "GEMINI_API_KEY"
	GoogleAPIKey    = "GOOGLE_API_KEY"
	// OpenAIAPIKey is the key for the generic openai-compat provider. It is
	// OPTIONAL: keyless local endpoints (llama.cpp, Ollama) leave it unset,
	// and validation gates on BaseURL presence rather than this key.
	OpenAIAPIKey = "OPENAI_API_KEY"

	// System
	Home = "HOME"

	// Mode selects Joe's boot posture for the write floor (D-0018/D-0073). The
	// day-one default is observation: UNSET or ModeObservation boots Joe with the
	// write floor up (reason observation) — a hard read-only floor that is the
	// intended resting posture, distinct from panic/safe mode. ModeFull is
	// REFUSED at boot as not-yet-implemented (full mode boots write-capable only
	// once the full-mode-requires-auth fail-closed guarantee is built); any other
	// value is refused fail-closed. A sticky panic state forces safe mode
	// regardless. The value is mapped by ResolveBootMode and read once at boot in
	// cmd/joe/server.go, consistent with the other JOE_* boot env vars.
	Mode = "JOE_MODE"
)

// Values for the Mode env var.
const (
	// ModeObservation raises the write floor with reason observation at boot. It
	// is also the effective default when JOE_MODE is unset.
	ModeObservation = "observation"
	// ModeFull is the (not-yet-implemented) write-capable posture. Selecting it
	// is refused at boot until full mode lands — see ResolveBootMode.
	ModeFull = "full"
)

// ResolveBootMode maps a raw JOE_MODE value to the observation input for
// safety.ResolveWriteFloor. It is PURE and unit-testable — it reads no
// environment and stores nothing; the caller passes os.Getenv(Mode) in and
// exits non-zero on error. The observation posture is the day-one default
// (D-0073): an unconfigured Joe boots read-only.
//
//   - "" (unset) or ModeObservation → observation posture (observation=true, the
//     write floor comes up with reason observation unless panic wins first).
//   - ModeFull → error: full mode is not yet implemented; Joe currently runs in
//     observation mode only. Refused rather than silently downgraded.
//   - any other non-empty value → error naming the unrecognized value and the
//     accepted set (fail-closed).
//
// The full/unknown refusal is independent of panic state: the caller runs this
// decision BEFORE floor resolution, so a bad JOE_MODE aborts boot regardless of
// any sticky panic. safety.ResolveWriteFloor is unchanged — its writable
// resolution path (observation=false) is retained as the seam for full mode when
// it is implemented.
func ResolveBootMode(raw string) (observation bool, err error) {
	switch raw {
	case "", ModeObservation:
		return true, nil
	case ModeFull:
		return false, fmt.Errorf(
			"%s=%s is not yet implemented; Joe currently runs in observation mode only",
			Mode, ModeFull)
	default:
		return false, fmt.Errorf(
			"unrecognized %s value %q; accepted values are %q and %q",
			Mode, raw, ModeObservation, ModeFull)
	}
}
