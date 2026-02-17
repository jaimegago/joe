package repl

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/llm"
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
