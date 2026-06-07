package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jaimegago/joe/internal/safety"
)

// stubPanicHandler returns a panicHandler whose joeDirFn points at t.TempDir()
// and whose floor is down (no panic, no observation).
func stubPanicHandler(t *testing.T) *panicHandler {
	t.Helper()
	dir := t.TempDir()
	return &panicHandler{joeDirFn: func() (string, error) { return dir, nil }}
}

func TestHandlePanicStatus_NotInPanic(t *testing.T) {
	h := stubPanicHandler(t) // floor down
	req := httptest.NewRequest("GET", "/api/v1/panic/status", nil)
	w := httptest.NewRecorder()
	h.handlePanicStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp panicStatusResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.SafeMode {
		t.Error("expected safe_mode=false")
	}
}

func TestHandlePanicStatus_InPanic(t *testing.T) {
	dir := t.TempDir()
	// The boot-resolved floor is up with reason safe_mode (sticky panic).
	h := &panicHandler{
		joeDirFn: func() (string, error) { return dir, nil },
		floor:    safety.ResolveWriteFloor(true, false),
	}

	state := safety.PanicState{
		TriggeredAt:   time.Date(2025, 3, 1, 12, 0, 0, 0, time.UTC),
		TriggerSource: safety.PanicSourceAPI,
		TriggerReason: "test panic",
	}
	if err := safety.WritePanicState(dir, state); err != nil {
		t.Fatalf("write panic state: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/v1/panic/status", nil)
	w := httptest.NewRecorder()
	h.handlePanicStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp panicStatusResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.SafeMode {
		t.Error("expected safe_mode=true")
	}
	if resp.TriggerReason != "test panic" {
		t.Errorf("TriggerReason: got %q", resp.TriggerReason)
	}
}

// TestHandlePanicStatus_Observation confirms the calm observation posture is NOT
// reported as safe mode — only the safe_mode floor reason is "panic recovery".
func TestHandlePanicStatus_Observation(t *testing.T) {
	dir := t.TempDir()
	h := &panicHandler{
		joeDirFn: func() (string, error) { return dir, nil },
		floor:    safety.ResolveWriteFloor(false, true /*observation*/),
	}

	req := httptest.NewRequest("GET", "/api/v1/panic/status", nil)
	w := httptest.NewRecorder()
	h.handlePanicStatus(w, req)

	var resp panicStatusResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.SafeMode {
		t.Error("observation mode must NOT report safe_mode=true")
	}
}

func TestHandlePanicStatus_JoeDirError(t *testing.T) {
	h := &panicHandler{
		joeDirFn: func() (string, error) { return "", fmt.Errorf("no home") },
		floor:    safety.ResolveWriteFloor(true, false),
	}
	req := httptest.NewRequest("GET", "/api/v1/panic/status", nil)
	w := httptest.NewRecorder()
	h.handlePanicStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 even with dir error, got %d", w.Code)
	}
	var resp panicStatusResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.SafeMode {
		t.Error("expected safe_mode=true")
	}
}

// TestRegisterPanicRoutes confirms the panic + status routes are registered and,
// critically, that the unlock route is GONE — the floor has no API surface to be
// lowered (D-0018 point 4).
func TestRegisterPanicRoutes(t *testing.T) {
	mux := http.NewServeMux()
	s := &Server{}
	s.registerPanicRoutes(mux, "/api/v1")

	// Use wrong-method (OPTIONS) requests to confirm routes are registered (405)
	// without invoking handlers. POST /panic in particular schedules os.Exit,
	// so we must never call it with its real method in tests.
	registered := []string{
		"/api/v1/panic",
		"/api/v1/panic/status",
	}
	for _, p := range registered {
		req := httptest.NewRequest("OPTIONS", p, nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code == http.StatusNotFound {
			t.Errorf("route %s not registered (got 404)", p)
		}
	}

	// The unlock endpoint must NOT exist — there is no API to lower the floor.
	req := httptest.NewRequest("POST", "/api/v1/unlock", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("unlock endpoint must be gone; got status %d for POST /api/v1/unlock", w.Code)
	}
}
