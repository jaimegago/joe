package askuser

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestNewTool(t *testing.T) {
	tool := NewTool()
	if tool == nil {
		t.Fatal("NewTool() returned nil")
	}
	if tool.reader == nil {
		t.Error("NewTool() reader is nil")
	}
	if tool.writer == nil {
		t.Error("NewTool() writer is nil")
	}
}

func TestNewToolWithIO(t *testing.T) {
	reader := strings.NewReader("test")
	writer := &bytes.Buffer{}

	tool := NewToolWithIO(reader, writer)
	if tool == nil {
		t.Fatal("NewToolWithIO() returned nil")
	}
	if tool.reader != reader {
		t.Error("NewToolWithIO() reader not set correctly")
	}
	if tool.writer != writer {
		t.Error("NewToolWithIO() writer not set correctly")
	}
}

func TestTool_Name(t *testing.T) {
	tool := NewTool()
	if got := tool.Name(); got != "ask_user" {
		t.Errorf("Name() = %s, want ask_user", got)
	}
}

func TestTool_Description(t *testing.T) {
	tool := NewTool()
	desc := tool.Description()
	if desc == "" {
		t.Error("Description() returned empty string")
	}
}

func TestTool_Parameters(t *testing.T) {
	tool := NewTool()
	params := tool.Parameters()

	if params.Type != "object" {
		t.Errorf("Parameters().Type = %s, want object", params.Type)
	}

	if len(params.Properties) != 1 {
		t.Errorf("Parameters() has %d properties, want 1", len(params.Properties))
	}

	if _, ok := params.Properties["question"]; !ok {
		t.Error("Parameters() missing 'question' property")
	}

	if len(params.Required) != 1 || params.Required[0] != "question" {
		t.Errorf("Parameters().Required = %v, want [question]", params.Required)
	}
}

func TestTool_Execute(t *testing.T) {
	tests := []struct {
		name           string
		args           map[string]any
		input          string
		wantAnswer     string
		wantQuestion   string
		wantErr        bool
		wantErrMessage string
	}{
		{
			name:         "ask simple question",
			args:         map[string]any{"question": "What is your name?"},
			input:        "Alice\n",
			wantAnswer:   "Alice",
			wantQuestion: "What is your name? ",
		},
		{
			name:         "ask question with empty answer",
			args:         map[string]any{"question": "Press enter to continue"},
			input:        "\n",
			wantAnswer:   "",
			wantQuestion: "Press enter to continue ",
		},
		{
			name:         "ask question with multiword answer",
			args:         map[string]any{"question": "How are you?"},
			input:        "I am doing great\n",
			wantAnswer:   "I am doing great",
			wantQuestion: "How are you? ",
		},
		{
			name:           "missing question parameter",
			args:           map[string]any{},
			input:          "answer\n",
			wantErr:        true,
			wantErrMessage: "missing or invalid 'question' parameter",
		},
		{
			name:           "empty question parameter",
			args:           map[string]any{"question": ""},
			input:          "answer\n",
			wantErr:        true,
			wantErrMessage: "missing or invalid 'question' parameter",
		},
		{
			name:           "wrong type for question",
			args:           map[string]any{"question": 123},
			input:          "answer\n",
			wantErr:        true,
			wantErrMessage: "missing or invalid 'question' parameter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := strings.NewReader(tt.input)
			writer := &bytes.Buffer{}
			tool := NewToolWithIO(reader, writer)

			got, err := tool.Execute(context.Background(), tt.args)

			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				if err == nil {
					t.Errorf("Execute() expected error containing %q, got nil", tt.wantErrMessage)
				} else if !strings.Contains(err.Error(), tt.wantErrMessage) {
					t.Errorf("Execute() error = %v, want error containing %q", err, tt.wantErrMessage)
				}
				return
			}

			// Check that the question was written to output
			if output := writer.String(); output != tt.wantQuestion {
				t.Errorf("Execute() wrote %q to output, want %q", output, tt.wantQuestion)
			}

			// Check the returned answer
			gotMap, ok := got.(map[string]string)
			if !ok {
				t.Errorf("Execute() returned %T, want map[string]string", got)
				return
			}

			if gotMap["answer"] != tt.wantAnswer {
				t.Errorf("Execute() answer = %q, want %q", gotMap["answer"], tt.wantAnswer)
			}
		})
	}
}

func TestTool_Execute_EOF(t *testing.T) {
	// Test with EOF (no input)
	reader := strings.NewReader("")
	writer := &bytes.Buffer{}
	tool := NewToolWithIO(reader, writer)

	got, err := tool.Execute(context.Background(), map[string]any{"question": "Test?"})
	if err != nil {
		t.Errorf("Execute() with EOF returned error: %v", err)
		return
	}

	gotMap, ok := got.(map[string]string)
	if !ok {
		t.Errorf("Execute() returned %T, want map[string]string", got)
		return
	}

	if gotMap["answer"] != "" {
		t.Errorf("Execute() with EOF returned answer %q, want empty string", gotMap["answer"])
	}
}

func TestTool_Execute_MultipleLines(t *testing.T) {
	// Test that only the first line is read
	reader := strings.NewReader("first line\nsecond line\nthird line\n")
	writer := &bytes.Buffer{}
	tool := NewToolWithIO(reader, writer)

	got, err := tool.Execute(context.Background(), map[string]any{"question": "Question?"})
	if err != nil {
		t.Errorf("Execute() returned error: %v", err)
		return
	}

	gotMap, ok := got.(map[string]string)
	if !ok {
		t.Errorf("Execute() returned %T, want map[string]string", got)
		return
	}

	if gotMap["answer"] != "first line" {
		t.Errorf("Execute() answer = %q, want 'first line'", gotMap["answer"])
	}
}

func TestTool_Execute_ContextCancellation(t *testing.T) {
	// Note: The current implementation doesn't check context during execution
	// This test documents that behavior
	reader := strings.NewReader("answer\n")
	writer := &bytes.Buffer{}
	tool := NewToolWithIO(reader, writer)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// The tool doesn't currently respect context cancellation during read
	// This is acceptable for the MVP since reads are fast
	_, err := tool.Execute(ctx, map[string]any{"question": "Test?"})
	if err != nil {
		t.Errorf("Execute() with cancelled context returned error: %v", err)
	}
}
