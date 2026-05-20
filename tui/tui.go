// Package tui implements the interactive terminal UI for ce query.
package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/atheory-ai/context-engine/internal/config"
	"github.com/atheory-ai/context-engine/internal/runner"
)

// Run starts the TUI in interactive mode (no pre-filled query).
// Called from cli/query.go when ce query is run without arguments.
func Run(engine *runner.Engine, cfg *config.Config) error {
	model := NewModel(engine, cfg)

	p := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	_, err := p.Run()
	return err
}

// RunWithQuery starts the TUI with a pre-filled query that is auto-submitted.
// Used when --tui flag is provided alongside query text.
func RunWithQuery(engine *runner.Engine, cfg *config.Config, query string) error {
	model := NewModel(engine, cfg)
	model.queryInput.SetValue(query)
	started, _ := model.startQuery(query)

	p := tea.NewProgram(
		started,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	_, err := p.Run()
	return err
}
