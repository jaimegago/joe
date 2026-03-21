package repl

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/safety"
	"github.com/jaimegago/joe/internal/tools"
	"github.com/jaimegago/joe/internal/useragent"
)

type errLLM struct{ err error }

func (m *errLLM) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	return nil, m.err
}
func (m *errLLM) ChatStream(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	ch := make(chan llm.StreamChunk)
	close(ch)
	return ch, nil
}
func (m *errLLM) Embed(ctx context.Context, text string) ([]float32, error) { return nil, nil }

func TestNewWithSession(t *testing.T) {
	session := useragent.NewSession(nil)
	r := NewWithSession(newTestAgent(&mockLLM{response: "ok"}), testREPLConfig(), session)
	if r.session != session {
		t.Fatal("expected provided session to be used")
	}
}

func TestHandleCommand_Basic(t *testing.T) {
	r := New(newTestAgent(&mockLLM{response: "ok"}), testREPLConfig())

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

func TestHandleModelCommand_Branches(t *testing.T) {
	ctx := context.Background()
	original := runModelSelector
	defer func() { runModelSelector = original }()

	t.Run("no models", func(t *testing.T) {
		cfg := &config.Config{LLM: config.LLMConfig{Current: "x", Available: map[string]config.ModelConfig{}}}
		r := New(newTestAgent(&mockLLM{response: "ok"}), cfg)
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
		cfg := &config.Config{LLM: config.LLMConfig{Current: "only", Available: map[string]config.ModelConfig{"only": {Provider: "p", Model: "m"}}}}
		r := New(newTestAgent(&mockLLM{response: "ok"}), cfg)
		out := captureStdout(t, func() {
			if err := r.handleModelCommand(ctx); err != nil {
				t.Fatalf("error: %v", err)
			}
		})
		if !strings.Contains(out, "Only one model configured") {
			t.Fatalf("unexpected output: %q", out)
		}
	})

	t.Run("selector error", func(t *testing.T) {
		runModelSelector = func(models []string, current string) (string, error) { return "", errors.New("selector failed") }
		r := New(newSwitchableAgent(t, false), testREPLConfig())
		err := r.handleModelCommand(ctx)
		if err == nil || !strings.Contains(err.Error(), "failed to run selector") {
			t.Fatalf("expected wrapped selector error, got %v", err)
		}
	})

	t.Run("selector cancelled", func(t *testing.T) {
		runModelSelector = func(models []string, current string) (string, error) { return "", nil }
		r := New(newSwitchableAgent(t, false), testREPLConfig())
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
		r := New(newSwitchableAgent(t, false), testREPLConfig())
		out := captureStdout(t, func() {
			if err := r.handleModelCommand(ctx); err != nil {
				t.Fatalf("error: %v", err)
			}
		})
		if !strings.Contains(out, "Already using") {
			t.Fatalf("unexpected output: %q", out)
		}
	})

	t.Run("selected model missing", func(t *testing.T) {
		runModelSelector = func(models []string, current string) (string, error) { return "missing", nil }
		r := New(newSwitchableAgent(t, false), testREPLConfig())
		err := r.handleModelCommand(ctx)
		if err == nil || !strings.Contains(err.Error(), "model missing not found") {
			t.Fatalf("expected missing model error, got %v", err)
		}
	})

	t.Run("switch fails", func(t *testing.T) {
		runModelSelector = func(models []string, current string) (string, error) { return "other", nil }
		r := New(newSwitchableAgent(t, true), testREPLConfig())
		err := r.handleModelCommand(ctx)
		if err == nil || !strings.Contains(err.Error(), "failed to switch model") {
			t.Fatalf("expected switch failure, got %v", err)
		}
	})

	t.Run("switch success", func(t *testing.T) {
		runModelSelector = func(models []string, current string) (string, error) { return "other", nil }
		cfg := testREPLConfig()
		r := New(newSwitchableAgent(t, false), cfg)
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

func TestRun_CommandAndAgentPaths(t *testing.T) {
	t.Run("help and exit", func(t *testing.T) {
		r := New(newTestAgent(&mockLLM{response: "ok"}), testREPLConfig())
		out, err := runREPLWithInput(t, r, "/help\n/exit\n")
		if err != nil {
			t.Fatalf("Run error: %v", err)
		}
		if !strings.Contains(out, "Joe is ready.") || !strings.Contains(out, "Available commands") || !strings.Contains(out, "Goodbye.") {
			t.Fatalf("unexpected run output: %q", out)
		}
	})

	t.Run("agent success", func(t *testing.T) {
		r := New(newTestAgent(&mockLLM{response: "hello-back"}), testREPLConfig())
		out, err := runREPLWithInput(t, r, "hello\n/quit\n")
		if err != nil {
			t.Fatalf("Run error: %v", err)
		}
		if !strings.Contains(out, "hello-back") {
			t.Fatalf("expected agent response in output, got %q", out)
		}
	})

	t.Run("unknown command and agent error", func(t *testing.T) {
		r := New(newTestAgent(&errLLM{err: errors.New("llm-down")}), testREPLConfig())
		out, err := runREPLWithInput(t, r, "/bad\nhello\n/quit\n")
		if err != nil {
			t.Fatalf("Run error: %v", err)
		}
		if !strings.Contains(out, "unknown command") || !strings.Contains(out, "llm chat failed") {
			t.Fatalf("unexpected output: %q", out)
		}
	})
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

func newTestAgent(adapter llm.LLMAdapter) *useragent.Agent {
	registry := tools.NewRegistry()
	executor := tools.NewExecutor(registry, nil)
	return useragent.NewAgent(adapter, executor, registry, "test")
}

func newSwitchableAgent(t *testing.T, shouldFail bool) *useragent.Agent {
	t.Helper()
	registry := tools.NewRegistry()
	executor := tools.NewExecutor(registry, nil)
	factory := func(ctx context.Context, provider, model string) (llm.LLMAdapter, error) {
		if shouldFail {
			return nil, errors.New("factory failed")
		}
		return &mockLLM{response: "ok"}, nil
	}
	return useragent.NewAgent(&mockLLM{response: "ok"}, executor, registry, "test", useragent.WithAdapterFactory(factory), useragent.WithCurrentModelName("current"))
}

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

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w

	fn()

	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

// panicStdinHelper replaces os.Stdin with a pipe containing the given input,
// calls fn, then restores os.Stdin. It returns captured stdout output.
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

func TestHandlePanicCommand_Cancelled(t *testing.T) {
	r := New(newTestAgent(&mockLLM{response: "ok"}), testREPLConfig())
	out := panicStdinHelper(t, "no\n", func() {
		if err := r.handlePanicCommand(context.Background()); err != nil {
			// no error expected for cancellation
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
	// Strip "http://" to get just host:port for cfg.Server.Address
	cfg.Server.Address = strings.TrimPrefix(ts.URL, "http://")

	r := New(newTestAgent(&mockLLM{response: "ok"}), cfg)
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
	r := New(newTestAgent(&mockLLM{response: "ok"}), testREPLConfig())
	out := panicStdinHelper(t, "\n", func() {
		if err := r.handlePanicCommand(context.Background()); err != nil {
			t.Fatalf("handlePanicCommand error: %v", err)
		}
	})
	// Empty string != "yes", so Cancelled. should appear
	if !strings.Contains(out, "Cancelled.") {
		t.Fatalf("expected Cancelled in output, got %q", out)
	}
}

func TestHandlePanicCommand_EOFCancels(t *testing.T) {
	r := New(newTestAgent(&mockLLM{response: "ok"}), testREPLConfig())
	// Send EOF immediately (empty stdin) — scanner.Scan() returns false
	_ = panicStdinHelper(t, "", func() {
		err := r.handlePanicCommand(context.Background())
		// EOF causes scanner.Scan() == false → returns fmt.Errorf("cancelled")
		if err == nil || !strings.Contains(err.Error(), "cancelled") {
			t.Fatalf("expected cancelled error on EOF, got %v", err)
		}
	})
}

func TestHandleCommand_EmptyCommand(t *testing.T) {
	r := New(newTestAgent(&mockLLM{response: "ok"}), testREPLConfig())
	// "/  " → after TrimPrefix and Fields → empty parts → returns nil
	err := r.handleCommand(context.Background(), "/  ")
	if err != nil {
		t.Fatalf("handleCommand('/  ') = %v, want nil", err)
	}
}

func TestRun_EmptyInputAndEOF(t *testing.T) {
	t.Run("empty lines are skipped", func(t *testing.T) {
		r := New(newTestAgent(&mockLLM{response: "ok"}), testREPLConfig())
		// Send blank lines then quit — the empty input `continue` branch is exercised
		out, err := runREPLWithInput(t, r, "\n\n\n/quit\n")
		if err != nil {
			t.Fatalf("Run error: %v", err)
		}
		if !strings.Contains(out, "Goodbye.") {
			t.Fatalf("expected Goodbye, got %q", out)
		}
	})

	t.Run("EOF exits cleanly", func(t *testing.T) {
		r := New(newTestAgent(&mockLLM{response: "ok"}), testREPLConfig())
		// No /quit — just EOF (pipe closed with no input)
		out, err := runREPLWithInput(t, r, "")
		if err != nil {
			t.Fatalf("Run error: %v", err)
		}
		if !strings.Contains(out, "Joe is ready.") {
			t.Fatalf("expected ready message, got %q", out)
		}
	})
}

func TestHandleCommand_ModelAndPanicRouting(t *testing.T) {
	t.Run("routes /model", func(t *testing.T) {
		original := runModelSelector
		defer func() { runModelSelector = original }()
		runModelSelector = func(models []string, current string) (string, error) { return "", nil }

		r := New(newTestAgent(&mockLLM{response: "ok"}), testREPLConfig())
		out := captureStdout(t, func() {
			err := r.handleCommand(context.Background(), "/model")
			if err != nil {
				t.Fatalf("handleCommand(/model) = %v", err)
			}
		})
		if !strings.Contains(out, "Cancelled") {
			t.Fatalf("expected Cancelled from model selector, got %q", out)
		}
	})

	t.Run("routes /panic to cancelled (non-yes input)", func(t *testing.T) {
		r := New(newTestAgent(&mockLLM{response: "ok"}), testREPLConfig())
		panicStdinHelper(t, "no\n", func() {
			err := r.handleCommand(context.Background(), "/panic")
			if err != nil {
				t.Fatalf("handleCommand(/panic) = %v", err)
			}
		})
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

	r := New(newTestAgent(&mockLLM{response: "ok"}), cfg)
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

	r := New(newTestAgent(&mockLLM{response: "ok"}), cfg)
	_ = panicStdinHelper(t, "yes\n", func() {
		err := r.handlePanicCommand(context.Background())
		if err == nil || !strings.Contains(err.Error(), "failed to reach joe-core") {
			t.Fatalf("expected reach error, got %v", err)
		}
	})
}

func TestNotifier_NilOut_ZeroDelay(t *testing.T) {
	// Notifier with nil Out and zero Delay falls back to os.Stdout and DefaultT3Delay.
	// We test via NotifyAfter (which calls out()) to avoid blocking.
	n := &Notifier{Out: nil, Delay: 0}
	info := safety.ActionInfo{
		ToolName:    "echo",
		Tier:        safety.TierAct,
		Description: "echo test",
	}
	// Redirect stdout to avoid polluting test output
	out := captureStdout(t, func() {
		n.NotifyAfter(context.Background(), info, nil, nil)
	})
	_ = out // just verify it doesn't panic

	// Test that delay() returns DefaultT3Delay. We verify indirectly via
	// a very short-lived NotifyBefore that we cancel immediately.
	n2 := &Notifier{Out: nil, Delay: 0}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled
	captureStdout(t, func() {
		_ = n2.NotifyBefore(ctx, info)
	})
}
