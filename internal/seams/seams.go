//go:build !seam_enabled

// Package seams hosts the compile-time autonomy-seam flags from
// the session-model design (Phase 0) §"incremental-autonomy seam pattern".
//
// Every constant in this package gates a defined-but-inert entry point
// that Phase 1 builds as a *seam*, never as *behavior*. The constants
// are compile-time `const false` — NOT config-driven, NOT runtime
// settings, NOT environment variables. Flipping a seam is a one-line
// constant change followed by a rebuild; that mechanical step is the
// gate, not a settings change.
//
// The const-not-var named structural guard in seams_guard_test.go
// enforces this property and also asserts the broader repository
// contains no `var Joe*Enabled` declarations and no `os.Getenv` calls
// or config struct fields referencing the seam names.
//
// Per Change 12: the four seams build as `false` for the default
// build. A parallel file seams_enabled.go (with `//go:build seam_enabled`)
// provides the same identifiers as `true`, enabling paired tests to
// assert each autonomous path becomes reachable when the seam is
// flipped — without requiring a code change at the call site.
package seams

// JoeAutonomousDeclareEnabled gates Joe-autonomous incident declaration
// (the session-model design (Phase 0) R2 / "incremental-autonomy seam pattern").
// When false (Phase 1), a declare request that asks for
// declared_kind=joe is refused with 403 BEFORE any sessionmodel
// mutation. When true (future enablement), the declare handler routes
// to the same DeclareIncidentRegime path, but with declared_kind=joe
// and the autonomous-authority binding still bounded by §F.
const JoeAutonomousDeclareEnabled = false

// JoeAutonomousResolveEnabled gates Joe-autonomous incident resolve
// (the session-model design (Phase 0) R4 / "incremental-autonomy seam pattern").
// When false (Phase 1), a resolve request that signals as_joe=true is
// refused with 403 BEFORE any call to
// sessionmodel.Repository.ResolveIncidentRegime — preserving Change 5's
// AST grep guard (Invariant 4) which pins the SOLE production caller of
// ResolveIncidentRegime to the human-resolve handler.
const JoeAutonomousResolveEnabled = false

// JoeConfirmCloseDispositionEnabled gates Joe self-disposition of a
// confirm_close solicitation (the session-model design (Phase 0) §D taxonomy /
// "incremental-autonomy seam pattern"). When false (Phase 1), a resolve
// request against a confirm_close solicitation that signals
// disposed_by=joe is refused with 403. Human disposition (the default
// disposed_by value) is unaffected and proceeds through the normal
// resolve path.
const JoeConfirmCloseDispositionEnabled = false

// JoeCaptainTypeEnabled gates attaching a captain of type joe
// (the session-model design (Phase 0) R-CAP4 / "incremental-autonomy seam pattern").
// When false (Phase 1), CaptainService.Attach returns
// ErrOnlyHumansInPhase1 for captain_type=joe. When true (future
// enablement), Attach proceeds with the joe-type insertion.
//
// §B R-OVR LIMITATION — DO NOT ENABLE without first landing scoped
// per-agent unlock.
//
// The existing override substrate (panic → safe-mode → unlock --reason)
// is a SINGLE GLOBAL BOOLEAN. There is no per-agent or per-session
// unlock and no approval workflow. Therefore, when Joe-captaincy is
// enabled, "human overrides autonomous Joe" via the panic path is a
// GLOBAL EMERGENCY STOP (entire system drops to T1 safe mode), not a
// session-surgical takeback.
//
// Phase 1 explicitly DOES NOT BUILD scoped per-agent unlock — it is
// tracked as "Open / deferred" in the session-model design (Phase 0). The
// R-OVR force-yield in CaptainService.BeginTransfer (compiled in, not
// behind this seam) IS in place, but its semantic guarantee is only
// complete once a session-surgical override mechanism exists.
//
// A future contributor enabling this seam without first landing scoped
// unlock is the residual-risk failure mode recorded in the Phase 1
// decomposition plan §6.
const JoeCaptainTypeEnabled = false
