package repl

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/jaimegago/joe/internal/client"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/tools"
	"github.com/jaimegago/joe/internal/uid"
)

var ErrExit = errors.New("exit requested")

var runModelSelector = RunModelSelector

// REPL implements the Read-Eval-Print-Loop for interactive mode. After the
// Phase 2 runtime collapse it is a thin client: it sends user input to
// joe-core, streams the single agentic loop's output back, renders it, and
// services local-tool callbacks against the operator's own machine. It runs no
// LLM and no agentic loop of its own.
type REPL struct {
	client      *client.Client
	config      *config.Config
	executor    *tools.Executor // local executor for delegated tool callbacks
	registry    *tools.Registry // local tools, advertised to joe-core
	clientTools []client.ClientToolDef
	sessionID   string
}

// New creates a thin REPL bound to a joe-core client. The executor/registry
// hold the local-machine tools the CLI executes when joe-core delegates a
// tool call back over the stream.
func New(c *client.Client, cfg *config.Config, executor *tools.Executor, registry *tools.Registry) *REPL {
	return &REPL{
		client:      c,
		config:      cfg,
		executor:    executor,
		registry:    registry,
		clientTools: toClientTools(registry),
		sessionID:   uid.New(),
	}
}

// toClientTools converts a local tool registry into the client-tool
// advertisements joe-core registers as delegating stubs.
func toClientTools(registry *tools.Registry) []client.ClientToolDef {
	if registry == nil {
		return nil
	}
	defs := registry.ToDefinitions()
	out := make([]client.ClientToolDef, 0, len(defs))
	for _, d := range defs {
		out = append(out, client.ClientToolDef{
			Name:        d.Name,
			Description: d.Description,
			Parameters:  d.Parameters,
		})
	}
	return out
}

// Run starts the REPL loop. It reads input, sends each turn to joe-core, and
// renders the streamed response. Exits on "exit", "quit", or Ctrl+D (EOF).
func (r *REPL) Run(ctx context.Context) error {
	fmt.Println("Joe is ready.")
	fmt.Println()

	scanner := bufio.NewScanner(os.Stdin)

	for {
		fmt.Print("> ")

		if !scanner.Scan() {
			break // EOF (Ctrl+D) or error
		}

		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}

		if strings.HasPrefix(input, "/") {
			if err := r.handleCommand(ctx, input); err != nil {
				if errors.Is(err, ErrExit) {
					fmt.Println("Goodbye.")
					break
				}
				fmt.Printf("Error: %v\n", err)
			}
			fmt.Println()
			continue
		}

		if err := r.streamTurn(ctx, input); err != nil {
			fmt.Printf("Error: %v\n", err)
		}
		fmt.Println()
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading input: %w", err)
	}
	return nil
}

// streamTurn sends one user message to joe-core's single agentic loop and
// renders the streamed events.
func (r *REPL) streamTurn(ctx context.Context, message string) error {
	return r.client.StreamTask(ctx, client.TaskStreamRequest{
		Message:     message,
		SessionID:   r.sessionID,
		ClientTools: r.clientTools,
	}, func(e client.TaskEvent) error {
		return r.onEvent(ctx, e)
	})
}

// onEvent dispatches a single streamed event: execute delegated local tools,
// render the final answer, and otherwise ignore (step events are progress only).
func (r *REPL) onEvent(ctx context.Context, e client.TaskEvent) error {
	switch e.Type {
	case client.TaskEventLocalToolCall:
		var call client.LocalToolCall
		if err := json.Unmarshal(e.Data, &call); err != nil {
			return fmt.Errorf("decode local tool call: %w", err)
		}
		return r.runLocalTool(ctx, call)
	case client.TaskEventFinal:
		var res client.TaskResult
		if err := json.Unmarshal(e.Data, &res); err != nil {
			return fmt.Errorf("decode final response: %w", err)
		}
		r.renderFinal(res)
	}
	return nil
}

// runLocalTool executes a delegated tool on the operator's machine (through the
// local executor, so the local safety policy applies) and posts the result
// back. A tool failure is reported to joe-core as an error result so the loop
// can continue, not surfaced as a stream-aborting error here.
func (r *REPL) runLocalTool(ctx context.Context, call client.LocalToolCall) error {
	fmt.Printf("  · %s\n", call.Name)

	result, execErr := r.executor.Execute(ctx, call.Name, call.Args)
	errMsg := ""
	if execErr != nil {
		errMsg = execErr.Error()
	}
	if err := r.client.SubmitToolResult(ctx, call.TaskID, call.CallID, result, errMsg); err != nil {
		return fmt.Errorf("submit tool result: %w", err)
	}
	return nil
}

// renderFinal prints the final answer, or the error for a non-completed turn.
func (r *REPL) renderFinal(res client.TaskResult) {
	if res.Status != "" && res.Status != "completed" && res.Error != "" {
		fmt.Printf("Error: %s\n", res.Error)
		return
	}
	if res.FinalAnswer != "" {
		fmt.Println(res.FinalAnswer)
	}
}

// handleCommand processes REPL commands starting with /
func (r *REPL) handleCommand(ctx context.Context, input string) error {
	cmd := strings.TrimPrefix(input, "/")
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return nil
	}

	switch parts[0] {
	case "model":
		return r.handleModelCommand(ctx)
	case "help":
		return r.handleHelpCommand()
	case "panic":
		return r.handlePanicCommand(ctx)
	case "exit", "quit":
		return ErrExit
	default:
		return fmt.Errorf("unknown command: /%s. Type /help for available commands", parts[0])
	}
}

// handleModelCommand shows an interactive model selector and switches the
// model on joe-core (the single runtime), not a CLI-local adapter.
func (r *REPL) handleModelCommand(ctx context.Context) error {
	models, err := r.client.ListModels(ctx)
	if err != nil {
		return fmt.Errorf("failed to list models: %w", err)
	}

	if len(models.Available) == 0 {
		fmt.Println("No models configured on joe-core")
		return nil
	}
	if len(models.Available) == 1 {
		fmt.Printf("Only one model configured: %s\n", models.Current)
		return nil
	}

	selected, err := runModelSelector(models.Available, models.Current)
	if err != nil {
		return fmt.Errorf("failed to run selector: %w", err)
	}
	if selected == "" {
		fmt.Println("Cancelled")
		return nil
	}
	if selected == models.Current {
		fmt.Printf("Already using %s\n", models.Current)
		return nil
	}

	result, err := r.client.SetModel(ctx, selected)
	if err != nil {
		return fmt.Errorf("failed to switch model: %w", err)
	}

	// Keep local config in sync for any display purposes.
	r.config.LLM.Current = result.Current

	fmt.Printf("\nSwitched to %s (%s/%s)\n", result.Current, result.Provider, result.Model)
	return nil
}

// handlePanicCommand triggers an emergency shutdown of joecored.
// It prompts for confirmation before sending the request.
func (r *REPL) handlePanicCommand(ctx context.Context) error {
	fmt.Println("⚠  EMERGENCY SHUTDOWN")
	fmt.Println()
	fmt.Println("This will immediately:")
	fmt.Println("  • Stop all in-flight operations")
	fmt.Println("  • Shut down joe-core")
	fmt.Println("  • Restart joe-core in safe mode (T1/read-only)")
	fmt.Println()
	fmt.Print("Type 'yes' to confirm: ")

	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return fmt.Errorf("cancelled")
	}
	if strings.TrimSpace(scanner.Text()) != "yes" {
		fmt.Println("Cancelled.")
		return nil
	}

	scheme := "http"
	if r.config.Server.TLSEnabled {
		scheme = "https"
	}
	panicURL := scheme + "://" + r.config.Server.Address + "/api/v1/panic"

	body, _ := json.Marshal(map[string]string{"reason": "operator triggered via REPL"})
	req, err := http.NewRequestWithContext(ctx, "POST", panicURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create panic request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if key := r.config.Server.LoopbackKey(); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to reach joe-core: %w", err)
	}
	defer resp.Body.Close()

	fmt.Println()
	fmt.Println("🛑 Panic triggered. joe-core shutting down...")
	fmt.Printf("   State saved to ~/.joe/panic.state\n")
	fmt.Println()
	fmt.Println("Reconnect after restart. Joe will be in safe mode (read-only).")
	fmt.Println("Use 'joe unlock --reason \"...\"' to resume normal operation.")
	return nil
}

// handleHelpCommand displays available commands
func (r *REPL) handleHelpCommand() error {
	help := `Available commands:
  /model    - Switch LLM model
  /panic    - Emergency shutdown (kills joe-core, restarts in safe mode)
  /help     - Show this help
  /exit     - Exit Joe (or use Ctrl+D)
`
	fmt.Print(help)
	return nil
}
