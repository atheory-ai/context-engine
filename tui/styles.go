package tui

import "github.com/charmbracelet/lipgloss"

// Color palette — dark theme.
var (
	colorPrimary  = lipgloss.Color("#7C3AED") // violet
	colorMuted    = lipgloss.Color("#6B7280")
	colorActive   = lipgloss.Color("#10B981") // emerald
	colorWarning  = lipgloss.Color("#F59E0B")
	colorError    = lipgloss.Color("#EF4444")
	colorDim      = lipgloss.Color("#4B5563")
	colorText     = lipgloss.Color("#F9FAFB")
	colorBgAlt    = lipgloss.Color("#1F2937")
	colorBorder   = lipgloss.Color("#374151")
)

// Inline text styles.
var (
	Dim = lipgloss.NewStyle().Foreground(colorDim)

	Label = lipgloss.NewStyle().Foreground(colorMuted)

	Active = lipgloss.NewStyle().Foreground(colorActive)

	Warning = lipgloss.NewStyle().Foreground(colorWarning)

	Error = lipgloss.NewStyle().Foreground(colorError)

	Action = lipgloss.NewStyle().Foreground(colorPrimary)

	QueryText = lipgloss.NewStyle().Foreground(colorText)

	Title = lipgloss.NewStyle().
		Foreground(colorPrimary).
		Bold(true).
		MarginBottom(1)

	Spinner = lipgloss.NewStyle().Foreground(colorActive)

	LoopFilled = lipgloss.NewStyle().Foreground(colorPrimary)
	LoopEmpty  = lipgloss.NewStyle().Foreground(colorDim)

	ToolActive   = lipgloss.NewStyle().Foreground(colorActive)
	ToolInactive = lipgloss.NewStyle().Foreground(colorDim)
)

// Component/container styles.
var (
	TopBar = lipgloss.NewStyle().
		Background(colorBgAlt).
		Padding(0, 1).
		BorderBottom(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(colorBorder)

	StatusBar = lipgloss.NewStyle().
		Background(colorBgAlt).
		Padding(0, 1).
		BorderTop(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(colorBorder)

	Viewport = lipgloss.NewStyle().Padding(0, 1)

	TraceHeader = lipgloss.NewStyle().
		Foreground(colorMuted).
		Padding(0, 1)

	AnswerHeader = lipgloss.NewStyle().
		Foreground(colorPrimary).
		Bold(true).
		Padding(0, 1)
)
