package core

import (
	"context"
	"errors"
	"testing"

	falcoadapter "github.com/jaimegago/joe/internal/adapters/security/falco"
)

// --- Mock client ---

type mockFalcoClient struct {
	events []falcoadapter.Event
	rules  []falcoadapter.Rule
	err    error
}

func (m *mockFalcoClient) FalcoEvents(_ context.Context, _, _, _, _ string, _ int) ([]falcoadapter.Event, error) {
	return m.events, m.err
}

func (m *mockFalcoClient) FalcoRules(_ context.Context, _ string) ([]falcoadapter.Rule, error) {
	return m.rules, m.err
}

// --- FalcoAlertsTool tests ---

func TestFalcoAlertsTool_Metadata(t *testing.T) {
	tool := NewFalcoAlertsTool(&mockFalcoClient{})
	if tool.Name() != "falco_alerts" {
		t.Errorf("Name() = %q, want falco_alerts", tool.Name())
	}
	if tool.Description() == "" {
		t.Error("Description() should not be empty")
	}
	params := tool.Parameters()
	if params.Type != "object" {
		t.Errorf("Parameters().Type = %q, want object", params.Type)
	}
	if _, ok := params.Properties["component_id"]; !ok {
		t.Error("Parameters() missing component_id")
	}
	if _, ok := params.Properties["priority"]; !ok {
		t.Error("Parameters() missing priority")
	}
	if _, ok := params.Properties["limit"]; !ok {
		t.Error("Parameters() missing limit")
	}
}

func TestFalcoAlertsTool_Execute(t *testing.T) {
	events := []falcoadapter.Event{
		{UUID: "e1", Priority: "Critical", Rule: "Write below etc", Source: "syscall"},
		{UUID: "e2", Priority: "Warning", Rule: "Terminal shell in container", Source: "syscall"},
	}

	tests := []struct {
		name      string
		args      map[string]any
		mockErr   error
		wantCount int
		wantErr   bool
	}{
		{
			name:      "returns events",
			args:      map[string]any{"component_id": "src-1"},
			wantCount: 2,
		},
		{
			name:      "with all filters",
			args:      map[string]any{"component_id": "src-1", "priority": "Critical", "source": "syscall", "rule": "Write below etc", "limit": float64(10)},
			wantCount: 2,
		},
		{
			name:    "missing component_id",
			args:    map[string]any{},
			wantErr: true,
		},
		{
			name:    "empty component_id",
			args:    map[string]any{"component_id": ""},
			wantErr: true,
		},
		{
			name:    "client error",
			args:    map[string]any{"component_id": "src-1"},
			mockErr: errors.New("connection refused"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mockEvents []falcoadapter.Event
			if tt.mockErr == nil {
				mockEvents = events
			}
			tool := NewFalcoAlertsTool(&mockFalcoClient{events: mockEvents, err: tt.mockErr})

			got, err := tool.Execute(context.Background(), tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}

			result, ok := got.(map[string]any)
			if !ok {
				t.Fatal("Execute() result is not map[string]any")
			}
			if result["count"] != tt.wantCount {
				t.Errorf("count = %v, want %d", result["count"], tt.wantCount)
			}
		})
	}
}

// --- FalcoRulesTool tests ---

func TestFalcoRulesTool_Metadata(t *testing.T) {
	tool := NewFalcoRulesTool(&mockFalcoClient{})
	if tool.Name() != "falco_rules" {
		t.Errorf("Name() = %q, want falco_rules", tool.Name())
	}
	if tool.Description() == "" {
		t.Error("Description() should not be empty")
	}
	params := tool.Parameters()
	if params.Type != "object" {
		t.Errorf("Parameters().Type = %q, want object", params.Type)
	}
	if _, ok := params.Properties["component_id"]; !ok {
		t.Error("Parameters() missing component_id")
	}
}

func TestFalcoRulesTool_Execute(t *testing.T) {
	rules := []falcoadapter.Rule{
		{Name: "Write below etc", Priority: "Critical", Source: "syscall", Count: 5},
		{Name: "Terminal shell in container", Priority: "Warning", Source: "syscall", Count: 2},
	}

	tests := []struct {
		name      string
		args      map[string]any
		mockErr   error
		wantCount int
		wantErr   bool
	}{
		{
			name:      "returns rules",
			args:      map[string]any{"component_id": "src-1"},
			wantCount: 2,
		},
		{
			name:    "missing component_id",
			args:    map[string]any{},
			wantErr: true,
		},
		{
			name:    "empty component_id",
			args:    map[string]any{"component_id": ""},
			wantErr: true,
		},
		{
			name:    "client error",
			args:    map[string]any{"component_id": "src-1"},
			mockErr: errors.New("timeout"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mockRules []falcoadapter.Rule
			if tt.mockErr == nil {
				mockRules = rules
			}
			tool := NewFalcoRulesTool(&mockFalcoClient{rules: mockRules, err: tt.mockErr})

			got, err := tool.Execute(context.Background(), tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}

			result, ok := got.(map[string]any)
			if !ok {
				t.Fatal("Execute() result is not map[string]any")
			}
			if result["count"] != tt.wantCount {
				t.Errorf("count = %v, want %d", result["count"], tt.wantCount)
			}
		})
	}
}
