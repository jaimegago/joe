package repl

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/jaimegago/joe/internal/agent"
)

// REPL implements the Read-Eval-Print-Loop for interactive mode
type REPL struct {
	agent   *agent.Agent
	session *agent.Session
}

// New creates a new REPL with the given agent
func New(a *agent.Agent) *REPL {
	return &REPL{
		agent:   a,
		session: agent.NewSession(),
	}
}

// Run starts the REPL loop
// Prints welcome message, then loops reading input and calling the agent
// Exits on "exit", "quit", or Ctrl+D (EOF)
func (r *REPL) Run(ctx context.Context) error {
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

		// Handle exit commands
		if input == "exit" || input == "quit" {
			fmt.Println("Goodbye.")
			break
		}

		// Skip empty input
		if input == "" {
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
