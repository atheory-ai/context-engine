package update

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/runner"
)

// Channel message types — one per AppChannels field.
type ThinkingMsg struct{ Emission core.Emission }
type ActionMsg struct{ Emission core.Emission }
type MessageMsg struct{ Emission core.Emission }
type WarningMsg struct{ Emission core.Emission }
type ErrorMsg struct{ Emission core.Emission }
type CostMsg struct{ Emission core.Emission }
type SystemMsg struct{ Emission core.Emission }
type ProgressMsg struct{ Emission core.Emission }

// TickMsg fires on each poll interval.
type TickMsg struct{}

// QueryCompleteMsg fires when the engine finishes a query.
type QueryCompleteMsg struct{ Err error }

// TickCmd returns a command that fires after 50ms.
func TickCmd() tea.Cmd {
	return tea.Tick(50*time.Millisecond, func(_ time.Time) tea.Msg {
		return TickMsg{}
	})
}

// RunQueryCmd runs a query in the background and sends QueryCompleteMsg when done.
func RunQueryCmd(engine *runner.Engine, query string) tea.Cmd {
	return func() tea.Msg {
		err := engine.Query(context.Background(), query)
		return QueryCompleteMsg{Err: err}
	}
}
