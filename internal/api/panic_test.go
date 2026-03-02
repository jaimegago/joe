package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jaimegago/joe/internal/safety"
)

// stubPanicHandler returns a panicHandler whose joeDirFn points at t.TempDir().
func stubPanicHandler(t *testing.T) *panicHandler {
	t.Helper()
	dir := t.TempDir()
	return &panicHandler{joeDirFn: func() (string, error) { return dir, nil }}
}

func TestHandlePanicStatus_NotInPanic(t *testing.T) {
	safety.DeactivateSafeMode()
	safety.Reset()
	t.Cleanup(func() {
		safety.DeactivateSafeMode()
		safety.Reset()
	})

	h := stubPanicHandler(t)
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
	safety.DeactivateSafeMode()
	safety.Reset()
	t.Cleanup(func() {
		safety.DeactivateSafeMode()
		safety.Reset()
	})

	dir := t.TempDir()
	h := &panicHandler{joeDirFn: func() (string, error) { return dir, nil }}

	state := safety.PanicState{
		TriggeredAt:   time.Date(2025, 3, 1, 12, 0, 0, 0, time.UTC),
		TriggerSource: safety.PanicSourceAPI,
		TriggerReason: "test panic",
	}
	if err := safety.WritePanicState(dir, state); err != nil {
		t.Fatalf("write panic state: %v", err)
	}
	safety.ActivateSafeMode()

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

func TestHandleUnlock_MissingReason(t *testing.T) {
	h := stubPanicHandler(t)
	body := bytes.NewBufferString(`{"reason":""}`)
	req := httptest.NewRequest("POST", "/api/v1/unlock", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.handleUnlock(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleUnlock_Success(t *testing.T) {
	// Activate safe mode directly without calling Trigger. handleUnlock does not
	// require IsPanicked()==true — it calls safety.Unlock unconditionally — so
	// calling Trigger here is unnecessary and emits a spurious
	// "EMERGENCY SHUTDOWN TRIGGERED" slog.Error in test output.
	safety.DeactivateSafeMode()
	safety.Reset()
	safety.ActivateSafeMode()
	t.Cleanup(func() {
		safety.DeactivateSafeMode()
		safety.Reset()
	})

	dir := t.TempDir()
	state := safety.PanicState{
		TriggeredAt:   time.Now().UTC(),
		TriggerSource: safety.PanicSourceAPI,
	}
	if err := safety.WritePanicState(dir, state); err != nil {
		t.Fatalf("write panic state: %v", err)
	}
	h := &panicHandler{joeDirFn: func() (string, error) { return dir, nil }}

	body := bytes.NewBufferString(`{"reason":"false alarm, investigated and resolved"}`)
	req := httptest.NewRequest("POST", "/api/v1/unlock", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.handleUnlock(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}
	if safety.IsSafeModeActive() {
		t.Error("expected safe mode inactive after unlock")
	}
}

func TestHandleUnlock_BadJSON(t *testing.T) {
	h := stubPanicHandler(t)
	body := bytes.NewBufferString(`not-json`)
	req := httptest.NewRequest("POST", "/api/v1/unlock", body)
	w := httptest.NewRecorder()
	h.handleUnlock(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandlePanicStatus_JoeDirError(t *testing.T) {
	safety.ActivateSafeMode()
	t.Cleanup(func() { safety.DeactivateSafeMode() })

	h := &panicHandler{joeDirFn: func() (string, error) { return "", fmt.Errorf("no home") }}
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

func TestRegisterPanicRoutes(t *testing.T) {
	mux := http.NewServeMux()
	s := &Server{}
	s.registerPanicRoutes(mux, "/api/v1")

	// Use wrong-method (OPTIONS) requests to confirm routes are registered (405)
	// without invoking handlers. POST /panic in particular schedules os.Exit,
	// so we must never call it with its real method in tests.
	paths := []string{
		"/api/v1/panic",
		"/api/v1/panic/status",
		"/api/v1/unlock",
	}

	for _, p := range paths {
		req := httptest.NewRequest("OPTIONS", p, nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code == http.StatusNotFound {
			t.Errorf("route %s not registered (got 404)", p)
		}
	}
}
