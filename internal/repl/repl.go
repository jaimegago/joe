package repl

import (
	"context"
	"fmt"

	"github.com/jaimegago/joe/internal/joe"
)

// REPL implements the Read-Eval-Print-Loop for interactive mode
type REPL struct {
	joe *joe.Joe
}

// New creates a new REPL
func New(j *joe.Joe) *REPL {
	return &REPL{
		joe: j,
	}
}

// Run starts the REPL loop
func (r *REPL) Run(ctx context.Context) error {
	fmt.Println("Joe is ready.")
	fmt.Println()

	// TODO: Implement actual REPL loop with readline
	// For now, this is just a placeholder

	return nil
}
