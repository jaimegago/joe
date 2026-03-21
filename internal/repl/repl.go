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

	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/useragent"
)

var ErrExit = errors.New("exit requested")

var runModelSelector = RunModelSelector

// REPL implements the Read-Eval-Print-Loop for interactive mode
type REPL struct {
	agent   *useragent.Agent
	config  *config.Config
	session *useragent.Session
}

// New creates a new REPL with the given agent and config
// The session is created with default settings (no message limit)
func New(a *useragent.Agent, cfg *config.Config) *REPL {
	return &REPL{
		agent:   a,
		config:  cfg,
		session: useragent.NewSession(nil),
	}
}

// NewWithSession creates a new REPL with the given agent, config, and session
// This allows callers to configure session settings (like MaxMessages) before starting the REPL
func NewWithSession(a *useragent.Agent, cfg *config.Config, session *useragent.Session) *REPL {
	return &REPL{
		agent:   a,
		config:  cfg,
		session: session,
	}
}

// Run starts the REPL loop
// Prints welcome message, then loops reading input and calling the agent
// Exits on "exit", "quit", or Ctrl+D (EOF)
func (r *REPL) Run(ctx context.Context) error {
	defer r.session.Close()

	fmt.Println("Joe is ready.")
	fmt.Println()

	scanner := bufio.NewScanner(os.Stdin)

	for {
		// Print prompt
		fmt.Print("> ")

		// Read input
		if !scanner.Scan() {
			// EOF (Ctrl+D) or error
			break
		}

		input := strings.TrimSpace(scanner.Text())

		// Skip empty input
		if input == "" {
			continue
		}

		// Handle commands (start with /)
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

		// Run the agent
		response, err := r.agent.Run(ctx, r.session, input)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			fmt.Println()
			continue
		}

		// Print response
		fmt.Println(response)
		fmt.Println()
	}

	// Check for scanner errors
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading input: %w", err)
	}

	return nil
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

// handleModelCommand shows an interactive model selector and switches models
func (r *REPL) handleModelCommand(ctx context.Context) error {
	models := r.config.LLM.ModelNames()
	current := r.config.LLM.Current

	if len(models) == 0 {
		fmt.Println("No models configured in config.yaml")
		return nil
	}

	if len(models) == 1 {
		fmt.Printf("Only one model configured: %s\n", current)
		return nil
	}

	selected, err := runModelSelector(models, current)
	if err != nil {
		return fmt.Errorf("failed to run selector: %w", err)
	}

	if selected == "" {
		// User cancelled
		fmt.Println("Cancelled")
		return nil
	}

	if selected == current {
		fmt.Printf("Already using %s\n", current)
		return nil
	}

	// Get the model config
	modelCfg, ok := r.config.LLM.Available[selected]
	if !ok {
		return fmt.Errorf("model %s not found in config", selected)
	}

	// Switch the model
	if err := r.agent.SwitchModel(ctx, modelCfg.Provider, modelCfg.Model, selected); err != nil {
		return fmt.Errorf("failed to switch model: %w", err)
	}

	// Update config current
	r.config.LLM.Current = selected

	fmt.Printf("\nSwitched to %s (%s/%s)\n", selected, modelCfg.Provider, modelCfg.Model)
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
	if r.config.Server.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+r.config.Server.APIKey)
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
