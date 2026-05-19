//go:build !seam_enabled

package sessionmodel_test

import (
	"testing"

	"github.com/jaimegago/joe/internal/sessionmodel"
)

// TestSeam_JoeCaptainType_Disabled is the default-build assertion half
// of the Change 12 paired test for seams.JoeCaptainTypeEnabled. With
// the seam at its production value (false), CaptainService.Attach
// must refuse a captain_type=joe attach with ErrOnlyHumansInPhase1
// BEFORE writing any captain row.
//
// The paired enabled-build assertion lives in
// seam_captain_enabled_test.go with `//go:build seam_enabled`.
func TestSeam_JoeCaptainType_Disabled(t *testing.T) {
	e := newCaptainEnv(t, 60)
	sessionID := e.declareWithCaptain(t, "alice")

	_, err := e.svc.Attach(e.ctx, sessionID, "joe-agent", sessionmodel.CaptainTypeJoe)
	if err == nil {
		t.Fatal("attach with captain_type=joe should be refused in the default " +
			"build (seams.JoeCaptainTypeEnabled = false)")
	}
	if err != sessionmodel.ErrOnlyHumansInPhase1 {
		t.Errorf("err = %v, want ErrOnlyHumansInPhase1 (the seam-gated sentinel)", err)
	}
}
