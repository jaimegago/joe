package askuser

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"

	"github.com/jaimegago/joe/internal/llm"
)

// Tool implements a tool that asks the user for input
// This allows the agentic loop to pause and get user input when needed
type Tool struct {
	reader io.Reader
	writer io.Writer
}

// NewTool creates a new ask_user tool with os.Stdin and os.Stdout
func NewTool() *Tool {
	return &Tool{
		reader: os.Stdin,
		writer: os.Stdout,
	}
}

// NewToolWithIO creates a new ask_user tool with custom reader and writer
// This is useful for testing with bytes.Buffer
func NewToolWithIO(reader io.Reader, writer io.Writer) *Tool {
	return &Tool{
		reader: reader,
		writer: writer,
	}
}

// Name returns the tool's name
func (t *Tool) Name() string {
	return "ask_user"
}

// Description returns a description for the LLM
func (t *Tool) Description() string {
	return "Ask the user a question and wait for their response. Use this when you need additional information from the user."
}

// Parameters returns the parameter schema
func (t *Tool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"question": {
				Type:        "string",
				Description: "The question to ask the user",
			},
		},
		Required: []string{"question"},
	}
}

// Execute runs the ask_user tool
func (t *Tool) Execute(ctx context.Context, args map[string]any) (any, error) {
	question, ok := args["question"].(string)
	if !ok || question == "" {
		return nil, fmt.Errorf("missing or invalid 'question' parameter")
	}

	// Print the question
	fmt.Fprintf(t.writer, "%s ", question)

	// Read the answer
	scanner := bufio.NewScanner(t.reader)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("failed to read user input: %w", err)
		}
		// EOF without error
		return map[string]string{"answer": ""}, nil
	}

	answer := scanner.Text()

	return map[string]string{
		"answer": answer,
	}, nil
}
