package repl

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestNewModelSelector_CursorStartsAtCurrent(t *testing.T) {
	m := NewModelSelector([]string{"a", "b", "c"}, "b")
	if m.cursor != 1 {
		t.Fatalf("cursor=%d want 1", m.cursor)
	}
}

func TestModelSelector_UpdateNavigationAndSelect(t *testing.T) {
	m := NewModelSelector([]string{"a", "b", "c"}, "a")

	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = model.(*ModelSelector)
	if m.cursor != 1 {
		t.Fatalf("cursor=%d want 1", m.cursor)
	}

	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = model.(*ModelSelector)
	if m.cursor != 0 {
		t.Fatalf("cursor=%d want 0", m.cursor)
	}

	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = model.(*ModelSelector)
	if m.cursor != 0 {
		t.Fatalf("cursor should stay at 0, got %d", m.cursor)
	}

	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = model.(*ModelSelector)
	if m.cursor != 1 {
		t.Fatalf("cursor=%d want 1", m.cursor)
	}

	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(*ModelSelector)
	if m.selected != "b" {
		t.Fatalf("selected=%q want b", m.selected)
	}
}

func TestModelSelector_UpdateCancel(t *testing.T) {
	m := NewModelSelector([]string{"a", "b"}, "a")
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = model.(*ModelSelector)
	if !m.cancelled {
		t.Fatal("expected cancelled=true")
	}
}

func TestModelSelector_ViewAndInit(t *testing.T) {
	m := NewModelSelector([]string{"a", "b"}, "a")
	if cmd := m.Init(); cmd != nil {
		t.Fatal("expected Init to return nil")
	}

	v := m.View()
	if !strings.Contains(v, "Select model:") || !strings.Contains(v, "(current)") || !strings.Contains(v, "Use ↑/↓") {
		t.Fatalf("unexpected view: %q", v)
	}
}

func TestRunModelSelector_NoModels(t *testing.T) {
	selected, err := RunModelSelector(nil, "")
	if err == nil || !strings.Contains(err.Error(), "no models available") {
		t.Fatalf("expected no models error, got selected=%q err=%v", selected, err)
	}
}
