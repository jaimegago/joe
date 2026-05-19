//go:build seam_enabled

package sessionmodel_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/jaimegago/joe/internal/sessionmodel"
)

// TestSeam_JoeCaptainType_Enabled is the build-tag-isolated half of the
// Change 12 paired test for seams.JoeCaptainTypeEnabled. Compiled only
// with `go test -tags=seam_enabled ./...`.
//
// With the seam enabled, CaptainService.Attach must NOT refuse a
// captain_type=joe request. The attach proceeds through the normal
// pending_captain → first-captain path (R-CAP2 semantics extended to
// joe-type), inserting a joe captain row.
//
// Proves the call-site wiring at sessionmodel.Attach drops the
// seam-gated refusal when the constant is true.
func TestSeam_JoeCaptainType_Enabled(t *testing.T) {
	e := newCaptainEnv(t, 60)

	// Pending-captain incident: create directly so Attach has a
	// captain-less session to bind to.
	state := sessionmodel.IncidentStateDeclared
	sess := sessionmodel.AgentSession{
		ID:               uuid.NewString(),
		Type:             sessionmodel.SessionTypeIncident,
		IncidentState:    &state,
		CreatorPrincipal: "system",
	}
	if _, err := e.sess.CreateSession(e.ctx, sess); err != nil {
		t.Fatalf("create incident: %v", err)
	}

	res, err := e.svc.Attach(e.ctx, sess.ID, "joe-agent", sessionmodel.CaptainTypeJoe)
	if err != nil {
		t.Fatalf("Attach(joe) under seam_enabled returned err = %v — the call site "+
			"must NOT refuse when seams.JoeCaptainTypeEnabled = true", err)
	}
	if !res.BecameCaptain {
		t.Error("Attach(joe) under seam_enabled should bind the pending_captain " +
			"session (R-CAP2 path extended to joe-type)")
	}

	// Sanity: the persisted row carries captain_type='joe'.
	cap, err := e.sess.GetActiveCaptain(e.ctx, sess.ID)
	if err != nil || cap == nil {
		t.Fatalf("GetActiveCaptain: %v (cap=%v)", err, cap)
	}
	if cap.CaptainType != sessionmodel.CaptainTypeJoe {
		t.Errorf("active captain type = %q, want joe", cap.CaptainType)
	}
	if cap.Principal != "joe-agent" {
		t.Errorf("captain principal = %q, want joe-agent", cap.Principal)
	}
}
