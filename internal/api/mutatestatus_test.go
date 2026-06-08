package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/safety"
)

// getMutateStatus drives the handler with a given floor and returns both the
// decoded response and the raw JSON body (so tests can assert the exact wire
// bytes, not just the decoded values).
func getMutateStatus(t *testing.T, floor safety.WriteFloor) (mutateStatusResponse, string) {
	t.Helper()
	h := &mutateStatusHandler{floor: floor}
	req := httptest.NewRequest("GET", "/api/v1/mutate-status", nil)
	w := httptest.NewRecorder()
	h.handleMutateStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	raw := w.Body.String()
	var resp mutateStatusResponse
	if err := json.NewDecoder(strings.NewReader(raw)).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp, raw
}

// TestHandleMutateStatus_Full — floor down (no panic, no observation) reports
// can_mutate=true and reason="full". The empty FloorReasonNone must serialize
// as the explicit "full" and NEVER as the empty string (footgun guard).
func TestHandleMutateStatus_Full(t *testing.T) {
	resp, raw := getMutateStatus(t, safety.ResolveWriteFloor(false, false))
	if !resp.CanMutate {
		t.Error("expected can_mutate=true when floor is down")
	}
	if resp.Reason != "full" {
		t.Errorf("reason: got %q, want %q", resp.Reason, "full")
	}
	// Footgun guard: prove the floor-none case is "full" on the wire, not "".
	if !strings.Contains(raw, `"reason":"full"`) {
		t.Errorf("wire JSON must contain \"reason\":\"full\"; got %s", raw)
	}
	if strings.Contains(raw, `"reason":""`) {
		t.Errorf("wire JSON must NEVER emit an empty reason; got %s", raw)
	}
}

// TestHandleMutateStatus_Observation — the calm observation floor reports
// can_mutate=false and reason="observation", distinct from safe_mode.
func TestHandleMutateStatus_Observation(t *testing.T) {
	resp, _ := getMutateStatus(t, safety.ResolveWriteFloor(false, true /*observation*/))
	if resp.CanMutate {
		t.Error("expected can_mutate=false in observation mode")
	}
	if resp.Reason != "observation" {
		t.Errorf("reason: got %q, want %q", resp.Reason, "observation")
	}
}

// TestHandleMutateStatus_SafeMode — the sticky-panic floor reports
// can_mutate=false and reason="safe_mode". Panic wins over observation
// (panicStatePresent in ResolveWriteFloor).
func TestHandleMutateStatus_SafeMode(t *testing.T) {
	resp, _ := getMutateStatus(t, safety.ResolveWriteFloor(true /*panic*/, true /*observation*/))
	if resp.CanMutate {
		t.Error("expected can_mutate=false in safe mode")
	}
	if resp.Reason != "safe_mode" {
		t.Errorf("reason: got %q, want %q", resp.Reason, "safe_mode")
	}
}

// TestHandleMutateStatus_ExactKeys confirms the wire object carries EXACTLY the
// two snake_case keys can_mutate and reason — no leftover "posture"/"writable"
// fields, no extras.
func TestHandleMutateStatus_ExactKeys(t *testing.T) {
	_, raw := getMutateStatus(t, safety.ResolveWriteFloor(false, false))
	var keys map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &keys); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected exactly 2 keys, got %d: %v", len(keys), keys)
	}
	if _, ok := keys["can_mutate"]; !ok {
		t.Error("missing key can_mutate")
	}
	if _, ok := keys["reason"]; !ok {
		t.Error("missing key reason")
	}
	for k := range keys {
		if k != "can_mutate" && k != "reason" {
			t.Errorf("unexpected key %q on the wire", k)
		}
	}
}

// TestRegisterMutateStatusRoutes confirms the read-only mutate-status route is
// registered at the new path and is GET-only (no write surface). It is
// registered plainly on the mux, so it inherits the global edge-auth middleware
// chain exactly like panic/status and regime — no per-route gating.
func TestRegisterMutateStatusRoutes(t *testing.T) {
	mux := http.NewServeMux()
	s := &Server{}
	s.registerMutateStatusRoutes(mux, "/api/v1")

	// GET is registered (OPTIONS probes routing without invoking the handler).
	req := httptest.NewRequest("OPTIONS", "/api/v1/mutate-status", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code == http.StatusNotFound {
		t.Error("route /api/v1/mutate-status not registered (got 404)")
	}

	// POST must not be routed — the mutate-status endpoint is read-only.
	req = httptest.NewRequest("POST", "/api/v1/mutate-status", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /api/v1/mutate-status should be 405 (read-only); got %d", w.Code)
	}

	// The old /posture path must be gone.
	req = httptest.NewRequest("GET", "/api/v1/posture", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("old /api/v1/posture path must be gone; got %d", w.Code)
	}
}
