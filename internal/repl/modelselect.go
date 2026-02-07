package repl

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ModelSelector is a bubbletea model for interactively selecting a model
type ModelSelector struct {
	models    []string // Available model keys
	current   string   // Currently active model key
	cursor    int      // Cursor position
	selected  string   // Selected model (empty if cancelled)
	cancelled bool     // User pressed Esc
}

// NewModelSelector creates a new model selector
func NewModelSelector(models []string, current string) *ModelSelector {
	// Find cursor position to start at current model
	cursor := 0
	for i, m := range models {
		if m == current {
			cursor = i
			break
		}
	}

	return &ModelSelector{
		models:  models,
		current: current,
		cursor:  cursor,
	}
}

// Init implements tea.Model
func (m *ModelSelector) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model
func (m *ModelSelector) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.models)-1 {
				m.cursor++
			}
		case "enter":
			m.selected = m.models[m.cursor]
			return m, tea.Quit
		case "esc", "ctrl+c":
			m.cancelled = true
			return m, tea.Quit
		}
	}
	return m, nil
}

// View implements tea.Model
func (m *ModelSelector) View() string {
	var b strings.Builder

	headerStyle := lipgloss.NewStyle().Bold(true)
	b.WriteString(headerStyle.Render("Select model:"))
	b.WriteString("\n")

	for i, model := range m.models {
		cursor := "  "
		if i == m.cursor {
			cursor = "> "
		}

		marker := " "
		suffix := ""
		if model == m.current {
			marker = "•"
			suffix = " (current)"
		}

		line := fmt.Sprintf("%s %s %s%s", cursor, marker, model, suffix)

		if i == m.cursor {
			// Highlight the line under cursor
			highlightStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("cyan"))
			line = highlightStyle.Render(line)
		}

		b.WriteString(line)
		b.WriteString("\n")
	}

	b.WriteString("\n")
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	b.WriteString(hintStyle.Render("Use ↑/↓ to navigate, Enter to select, Esc to cancel"))

	return b.String()
}

// RunModelSelector runs the interactive model selector and returns the selected model key
// Returns empty string if cancelled
func RunModelSelector(models []string, current string) (string, error) {
	if len(models) == 0 {
		return "", fmt.Errorf("no models available")
	}

	m := NewModelSelector(models, current)
	p := tea.NewProgram(m)

	finalModel, err := p.Run()
	if err != nil {
		return "", fmt.Errorf("error running selector: %w", err)
	}

	selector := finalModel.(*ModelSelector)
	if selector.cancelled {
		return "", nil
	}

	return selector.selected, nil
}
