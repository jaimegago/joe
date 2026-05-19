//go:build seam_enabled

package seams_test

import (
	"testing"

	"github.com/jaimegago/joe/internal/seams"
)

// TestSeams_AllFlippedUnderBuildTag is a sanity check that runs only
// with `go test -tags=seam_enabled ./...`. It confirms that the
// seam_enabled build constraint actually selects seams_enabled.go over
// seams.go (i.e., the build-tag toggle works at all). If the paired
// seam-enabled tests across the repository ever start asserting "not
// 403" while the constants are still false, this test fails first and
// points at the toggle itself rather than the consumer paths.
func TestSeams_AllFlippedUnderBuildTag(t *testing.T) {
	cases := []struct {
		name string
		got  bool
	}{
		{"JoeAutonomousDeclareEnabled", seams.JoeAutonomousDeclareEnabled},
		{"JoeAutonomousResolveEnabled", seams.JoeAutonomousResolveEnabled},
		{"JoeConfirmCloseDispositionEnabled", seams.JoeConfirmCloseDispositionEnabled},
		{"JoeCaptainTypeEnabled", seams.JoeCaptainTypeEnabled},
	}
	for _, c := range cases {
		if !c.got {
			t.Errorf("%s = false under -tags=seam_enabled — the build-tag toggle "+
				"is not selecting seams_enabled.go. Check the //go:build "+
				"constraints in internal/seams/seams.go and seams_enabled.go.",
				c.name)
		}
	}
}
