package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaimegago/joe/internal/safety"
)

// getPosture drives the handler with a given floor and returns the decoded
// response.
func getPosture(t *testing.T, floor safety.WriteFloor) writePostureResponse {
	t.Helper()
	h := &postureHandler{floor: floor}
	req := httptest.NewRequest("GET", "/api/v1/posture", nil)
	w := httptest.NewRecorder()
	h.handlePosture(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp writePostureResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp
}

// TestHandlePosture_Normal — floor down (no panic, no observation) reports the
// "normal" posture and writable=true.
func TestHandlePosture_Normal(t *testing.T) {
	resp := getPosture(t, safety.ResolveWriteFloor(false, false))
	if resp.Posture != postureNormal {
		t.Errorf("posture: got %q, want %q", resp.Posture, postureNormal)
	}
	if !resp.Writable {
		t.Error("expected writable=true when floor is down")
	}
}

// TestHandlePosture_Observation — the calm observation floor reports the
// "observation" posture and writable=false, distinct from safe_mode.
func TestHandlePosture_Observation(t *testing.T) {
	resp := getPosture(t, safety.ResolveWriteFloor(false, true /*observation*/))
	if resp.Posture != postureObservation {
		t.Errorf("posture: got %q, want %q", resp.Posture, postureObservation)
	}
	if resp.Writable {
		t.Error("expected writable=false in observation mode")
	}
}

// TestHandlePosture_SafeMode — the sticky-panic floor reports the "safe_mode"
// posture and writable=false. Panic wins over observation (panicStatePresent).
func TestHandlePosture_SafeMode(t *testing.T) {
	resp := getPosture(t, safety.ResolveWriteFloor(true /*panic*/, true /*observation*/))
	if resp.Posture != postureSafeMode {
		t.Errorf("posture: got %q, want %q", resp.Posture, postureSafeMode)
	}
	if resp.Writable {
		t.Error("expected writable=false in safe mode")
	}
}

// TestRegisterPostureRoutes confirms the read-only posture route is registered
// and that it is GET-only (no write surface).
func TestRegisterPostureRoutes(t *testing.T) {
	mux := http.NewServeMux()
	s := &Server{}
	s.registerPostureRoutes(mux, "/api/v1")

	// GET is registered.
	req := httptest.NewRequest("OPTIONS", "/api/v1/posture", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code == http.StatusNotFound {
		t.Error("route /api/v1/posture not registered (got 404)")
	}

	// POST must not be routed — the posture endpoint is read-only.
	req = httptest.NewRequest("POST", "/api/v1/posture", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /api/v1/posture should be 405 (read-only); got %d", w.Code)
	}
}
