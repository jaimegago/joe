//go:build seam_enabled

// This file provides the seam_enabled variant of the autonomy-seam
// flags. It compiles ONLY when the build tag `seam_enabled` is set
// (e.g. `go test -tags=seam_enabled ./...`).
//
// Production binaries are built WITHOUT this tag — the default
// seams.go file (with `//go:build !seam_enabled`) is the canonical
// declaration and its const-false values are what ships.
//
// The paired-test pattern (a `_test.go` file asserting 403 in the
// default build, plus a `_seam_enabled_test.go` file with
// `//go:build seam_enabled` asserting non-403 when the seam flips)
// uses this file as the source of the flipped constants. Future
// enablement is therefore a one-line constant change in seams.go,
// not a wiring exercise — the call-site gating is already in place.
package seams

const JoeAutonomousDeclareEnabled = true
const JoeAutonomousResolveEnabled = true
const JoeConfirmCloseDispositionEnabled = true
const JoeCaptainTypeEnabled = true
