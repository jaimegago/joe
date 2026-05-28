package repl

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/client"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/safety"
)

// --- fake joe-core helpers ---

func writeSSE(w http.ResponseWriter, event string, data any) {
	b, _ := json.Marshal(data)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
}

// streamServer emits a step then a final event with the given answer.
func streamServer(t *testing.T, answer string) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tasks/stream" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		writeSSE(w, "step", map[string]any{"step_number": 1})
		writeSSE(w, "final", map[string]any{"final_answer": answer, "status": "completed"})
	}))
	t.Cleanup(ts.Close)
	return ts
}

// modelServer serves GET /models and POST /models/current.
func modelServer(t *testing.T, available []string, current string, setOK bool) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/models", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"available": available, "current": current})
	})
	mux.HandleFunc("POST /api/v1/models/current", func(w http.ResponseWriter, r *http.Request) {
		if !setOK {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error":"invalid_request","message":"cannot switch"}`)
			return
		}
		var body struct {
			Name string `json:"name"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(map[string]string{"current": body.Name, "provider": "p", "model": "m"})
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

func testREPLConfig() *config.Config {
	return &config.Config{LLM: config.LLMConfig{
		Current: "current",
		Available: map[string]config.ModelConfig{
			"current": {Provider: "test", Model: "m1"},
			"other":   {Provider: "test", Model: "m2"},
		},
	}}
}

// --- command routing ---

func TestHandleCommand_Basic(t *testing.T) {
	r := newTestREPL(t, client.New("http://unused"), testREPLConfig())

	if err := r.handleCommand(context.Background(), "/exit"); !errors.Is(err, ErrExit) {
		t.Fatalf("expected ErrExit, got %v", err)
	}
	if err := r.handleCommand(context.Background(), "/quit"); !errors.Is(err, ErrExit) {
		t.Fatalf("expected ErrExit, got %v", err)
	}
	if err := r.handleCommand(context.Background(), "/unknown"); err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("expected unknown command error, got %v", err)
	}

	out := captureStdout(t, func() {
		if err := r.handleCommand(context.Background(), "/help"); err != nil {
			t.Fatalf("/help error: %v", err)
		}
	})
	if !strings.Contains(out, "Available commands") {
		t.Fatalf("expected help output, got %q", out)
	}
}

func TestHandleCommand_EmptyCommand(t *testing.T) {
	r := newTestREPL(t, client.New("http://unused"), testREPLConfig())
	if err := r.handleCommand(context.Background(), "/  "); err != nil {
		t.Fatalf("handleCommand('/  ') = %v, want nil", err)
	}
}

// --- /model ---

func TestHandleModelCommand_Branches(t *testing.T) {
	ctx := context.Background()
	original := runModelSelector
	defer func() { runModelSelector = original }()

	t.Run("no models", func(t *testing.T) {
		ts := modelServer(t, nil, "", true)
		r := newTestREPL(t, client.New(ts.URL), testREPLConfig())
		out := captureStdout(t, func() {
			if err := r.handleModelCommand(ctx); err != nil {
				t.Fatalf("error: %v", err)
			}
		})
		if !strings.Contains(out, "No models configured") {
			t.Fatalf("unexpected output: %q", out)
		}
	})

	t.Run("single model", func(t *testing.T) {
		ts := modelServer(t, []string{"only"}, "only", true)
		r := newTestREPL(t, client.New(ts.URL), testREPLConfig())
		out := captureStdout(t, func() {
			if err := r.handleModelCommand(ctx); err != nil {
				t.Fatalf("error: %v", err)
			}
		})
		if !strings.Contains(out, "Only one model configured") {
			t.Fatalf("unexpected output: %q", out)
		}
	})

	t.Run("list error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer ts.Close()
		r := newTestREPL(t, client.New(ts.URL), testREPLConfig())
		err := r.handleModelCommand(ctx)
		if err == nil || !strings.Contains(err.Error(), "failed to list models") {
			t.Fatalf("expected list error, got %v", err)
		}
	})

	t.Run("selector error", func(t *testing.T) {
		runModelSelector = func(models []string, current string) (string, error) { return "", errors.New("selector failed") }
		ts := modelServer(t, []string{"current", "other"}, "current", true)
		r := newTestREPL(t, client.New(ts.URL), testREPLConfig())
		err := r.handleModelCommand(ctx)
		if err == nil || !strings.Contains(err.Error(), "failed to run selector") {
			t.Fatalf("expected wrapped selector error, got %v", err)
		}
	})

	t.Run("cancelled", func(t *testing.T) {
		runModelSelector = func(models []string, current string) (string, error) { return "", nil }
		ts := modelServer(t, []string{"current", "other"}, "current", true)
		r := newTestREPL(t, client.New(ts.URL), testREPLConfig())
		out := captureStdout(t, func() {
			if err := r.handleModelCommand(ctx); err != nil {
				t.Fatalf("error: %v", err)
			}
		})
		if !strings.Contains(out, "Cancelled") {
			t.Fatalf("unexpected output: %q", out)
		}
	})

	t.Run("already current", func(t *testing.T) {
		runModelSelector = func(models []string, current string) (string, error) { return current, nil }
		ts := modelServer(t, []string{"current", "other"}, "current", true)
		r := newTestREPL(t, client.New(ts.URL), testREPLConfig())
		out := captureStdout(t, func() {
			if err := r.handleModelCommand(ctx); err != nil {
				t.Fatalf("error: %v", err)
			}
		})
		if !strings.Contains(out, "Already using") {
			t.Fatalf("unexpected output: %q", out)
		}
	})

	t.Run("switch fails", func(t *testing.T) {
		runModelSelector = func(models []string, current string) (string, error) { return "other", nil }
		ts := modelServer(t, []string{"current", "other"}, "current", false)
		r := newTestREPL(t, client.New(ts.URL), testREPLConfig())
		err := r.handleModelCommand(ctx)
		if err == nil || !strings.Contains(err.Error(), "failed to switch model") {
			t.Fatalf("expected switch failure, got %v", err)
		}
	})

	t.Run("switch success", func(t *testing.T) {
		runModelSelector = func(models []string, current string) (string, error) { return "other", nil }
		ts := modelServer(t, []string{"current", "other"}, "current", true)
		cfg := testREPLConfig()
		r := newTestREPL(t, client.New(ts.URL), cfg)
		out := captureStdout(t, func() {
			if err := r.handleModelCommand(ctx); err != nil {
				t.Fatalf("error: %v", err)
			}
		})
		if cfg.LLM.Current != "other" {
			t.Fatalf("expected current model updated to other, got %q", cfg.LLM.Current)
		}
		if !strings.Contains(out, "Switched to other") {
			t.Fatalf("unexpected output: %q", out)
		}
	})
}

func TestHandleCommand_ModelAndPanicRouting(t *testing.T) {
	t.Run("routes /model", func(t *testing.T) {
		original := runModelSelector
		defer func() { runModelSelector = original }()
		runModelSelector = func(models []string, current string) (string, error) { return "", nil }

		ts := modelServer(t, []string{"current", "other"}, "current", true)
		r := newTestREPL(t, client.New(ts.URL), testREPLConfig())
		out := captureStdout(t, func() {
			if err := r.handleCommand(context.Background(), "/model"); err != nil {
				t.Fatalf("handleCommand(/model) = %v", err)
			}
		})
		if !strings.Contains(out, "Cancelled") {
			t.Fatalf("expected Cancelled from model selector, got %q", out)
		}
	})

	t.Run("routes /panic to cancelled (non-yes input)", func(t *testing.T) {
		r := newTestREPL(t, client.New("http://unused"), testREPLConfig())
		panicStdinHelper(t, "no\n", func() {
			if err := r.handleCommand(context.Background(), "/panic"); err != nil {
				t.Fatalf("handleCommand(/panic) = %v", err)
			}
		})
	})
}

// --- streaming turns ---

func TestRun_StreamsFinalAnswer(t *testing.T) {
	ts := streamServer(t, "hello-back")
	r := newTestREPL(t, client.New(ts.URL), testREPLConfig())

	out, err := runREPLWithInput(t, r, "hello\n/quit\n")
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if !strings.Contains(out, "hello-back") {
		t.Fatalf("expected streamed answer in output, got %q", out)
	}
	if !strings.Contains(out, "Goodbye.") {
		t.Fatalf("expected Goodbye, got %q", out)
	}
}

func TestRun_StreamErrorReported(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, `{"error":"service_unavailable","message":"LLM not available"}`)
	}))
	defer ts.Close()

	r := newTestREPL(t, client.New(ts.URL), testREPLConfig())
	out, err := runREPLWithInput(t, r, "hello\n/quit\n")
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if !strings.Contains(out, "Error:") {
		t.Fatalf("expected stream error reported, got %q", out)
	}
}

// --- local tool callback servicing ---

func TestREPL_ServicesLocalToolCall(t *testing.T) {
	var got struct {
		CallID string `json:"call_id"`
		Result any    `json:"result"`
		Error  string `json:"error"`
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/tool-results") {
			http.NotFound(w, r)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"status":"ok"}`)
	}))
	defer ts.Close()

	r := newTestREPL(t, client.New(ts.URL), testREPLConfig())

	fp := filepath.Join(t.TempDir(), "x.txt")
	if err := os.WriteFile(fp, []byte("FILEDATA"), 0600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	call := client.LocalToolCall{TaskID: "task1", CallID: "c1", Name: "read_file", Args: map[string]any{"path": fp}}
	data, _ := json.Marshal(call)

	captureStdout(t, func() {
		if err := r.onEvent(context.Background(), client.TaskEvent{Type: client.TaskEventLocalToolCall, Data: data}); err != nil {
			t.Fatalf("onEvent: %v", err)
		}
	})

	if got.CallID != "c1" {
		t.Errorf("submitted call_id = %q, want c1", got.CallID)
	}
	if got.Error != "" {
		t.Errorf("expected successful local tool, got error %q", got.Error)
	}
	if got.Result == nil {
		t.Error("expected a tool result to be submitted")
	}
}

func TestREPL_LocalToolFailureReportedNotAborted(t *testing.T) {
	var got struct {
		CallID string `json:"call_id"`
		Error  string `json:"error"`
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"status":"ok"}`)
	}))
	defer ts.Close()

	r := newTestREPL(t, client.New(ts.URL), testREPLConfig())

	call := client.LocalToolCall{TaskID: "task1", CallID: "c2", Name: "read_file", Args: map[string]any{"path": "/no/such/file/here"}}
	data, _ := json.Marshal(call)

	var err error
	captureStdout(t, func() {
		err = r.onEvent(context.Background(), client.TaskEvent{Type: client.TaskEventLocalToolCall, Data: data})
	})
	if err != nil {
		t.Fatalf("onEvent must not abort the stream on tool failure, got %v", err)
	}
	if got.Error == "" {
		t.Error("expected the tool failure to be reported to joe-core as an error result")
	}
}

// --- Run loop edges ---

func TestRun_EmptyInputAndEOF(t *testing.T) {
	t.Run("empty lines are skipped", func(t *testing.T) {
		r := newTestREPL(t, client.New("http://unused"), testREPLConfig())
		out, err := runREPLWithInput(t, r, "\n\n\n/quit\n")
		if err != nil {
			t.Fatalf("Run error: %v", err)
		}
		if !strings.Contains(out, "Goodbye.") {
			t.Fatalf("expected Goodbye, got %q", out)
		}
	})

	t.Run("EOF exits cleanly", func(t *testing.T) {
		r := newTestREPL(t, client.New("http://unused"), testREPLConfig())
		out, err := runREPLWithInput(t, r, "")
		if err != nil {
			t.Fatalf("Run error: %v", err)
		}
		if !strings.Contains(out, "Joe is ready.") {
			t.Fatalf("expected ready message, got %q", out)
		}
	})
}

// --- /panic ---

func TestHandlePanicCommand_Cancelled(t *testing.T) {
	r := newTestREPL(t, client.New("http://unused"), testREPLConfig())
	out := panicStdinHelper(t, "no\n", func() {
		if err := r.handlePanicCommand(context.Background()); err != nil {
			t.Fatalf("handlePanicCommand error: %v", err)
		}
	})
	if !strings.Contains(out, "Cancelled.") {
		t.Fatalf("expected Cancelled in output, got %q", out)
	}
}

func TestHandlePanicCommand_Confirmed(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	cfg := testREPLConfig()
	cfg.Server.Address = strings.TrimPrefix(ts.URL, "http://")

	r := newTestREPL(t, client.New("http://unused"), cfg)
	out := panicStdinHelper(t, "yes\n", func() {
		if err := r.handlePanicCommand(context.Background()); err != nil {
			t.Fatalf("handlePanicCommand error: %v", err)
		}
	})
	if !strings.Contains(out, "Panic triggered") {
		t.Fatalf("expected panic triggered in output, got %q", out)
	}
}

func TestHandlePanicCommand_EmptyInputCancels(t *testing.T) {
	r := newTestREPL(t, client.New("http://unused"), testREPLConfig())
	out := panicStdinHelper(t, "\n", func() {
		if err := r.handlePanicCommand(context.Background()); err != nil {
			t.Fatalf("handlePanicCommand error: %v", err)
		}
	})
	if !strings.Contains(out, "Cancelled.") {
		t.Fatalf("expected Cancelled in output, got %q", out)
	}
}

func TestHandlePanicCommand_EOFCancels(t *testing.T) {
	r := newTestREPL(t, client.New("http://unused"), testREPLConfig())
	_ = panicStdinHelper(t, "", func() {
		err := r.handlePanicCommand(context.Background())
		if err == nil || !strings.Contains(err.Error(), "cancelled") {
			t.Fatalf("expected cancelled error on EOF, got %v", err)
		}
	})
}

func TestHandlePanicCommand_WithAPIKey(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			t.Error("expected Authorization header")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	cfg := testREPLConfig()
	cfg.Server.Address = strings.TrimPrefix(ts.URL, "http://")
	cfg.Server.APIKey = "test-secret-key"

	r := newTestREPL(t, client.New("http://unused"), cfg)
	out := panicStdinHelper(t, "yes\n", func() {
		if err := r.handlePanicCommand(context.Background()); err != nil {
			t.Fatalf("handlePanicCommand error: %v", err)
		}
	})
	if !strings.Contains(out, "Panic triggered") {
		t.Fatalf("expected panic triggered, got %q", out)
	}
}

func TestHandlePanicCommand_UnreachableServer(t *testing.T) {
	cfg := testREPLConfig()
	cfg.Server.Address = "127.0.0.1:1" // nothing listening

	r := newTestREPL(t, client.New("http://unused"), cfg)
	_ = panicStdinHelper(t, "yes\n", func() {
		err := r.handlePanicCommand(context.Background())
		if err == nil || !strings.Contains(err.Error(), "failed to reach joe-core") {
			t.Fatalf("expected reach error, got %v", err)
		}
	})
}

func TestNotifier_NilOut_ZeroDelay(t *testing.T) {
	n := &Notifier{Out: nil, Delay: 0}
	info := safety.ActionInfo{
		ToolName:    "echo",
		Tier:        safety.TierAct,
		Description: "echo test",
	}
	out := captureStdout(t, func() {
		n.NotifyAfter(context.Background(), info, nil, nil)
	})
	_ = out

	n2 := &Notifier{Out: nil, Delay: 0}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	captureStdout(t, func() {
		_ = n2.NotifyBefore(ctx, info)
	})
}

// --- shared stdin/stdout helpers ---

func runREPLWithInput(t *testing.T, r *REPL, input string) (string, error) {
	t.Helper()
	origIn := os.Stdin
	origOut := os.Stdout
	defer func() {
		os.Stdin = origIn
		os.Stdout = origOut
	}()

	inR, inW, err := os.Pipe()
	if err != nil {
		return "", err
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		return "", err
	}

	if _, err := inW.WriteString(input); err != nil {
		return "", err
	}
	_ = inW.Close()

	os.Stdin = inR
	os.Stdout = outW

	runErr := r.Run(context.Background())
	_ = outW.Close()

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, outR)
	return buf.String(), runErr
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	origOut := os.Stdout
	defer func() { os.Stdout = origOut }()

	rp, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w

	fn()

	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, rp)
	return buf.String()
}

func panicStdinHelper(t *testing.T, stdinInput string, fn func()) string {
	t.Helper()
	origIn := os.Stdin
	origOut := os.Stdout
	defer func() {
		os.Stdin = origIn
		os.Stdout = origOut
	}()

	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	if _, err := inW.WriteString(stdinInput); err != nil {
		t.Fatalf("write stdin: %v", err)
	}
	_ = inW.Close()
	os.Stdin = inR

	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	os.Stdout = outW

	fn()

	_ = outW.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, outR)
	return buf.String()
}
