// Package tui implements the interactive terminal UI for ce query.
package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/atheory/context-engine/internal/config"
	"github.com/atheory/context-engine/internal/runner"
	"github.com/atheory/context-engine/tui/render"
	"github.com/atheory/context-engine/tui/update"
)

// ViewState controls which view the main area shows.
type ViewState int

const (
	ViewStateInput    ViewState = iota // waiting for query input
	ViewStateThinking                  // query running, showing trace
	ViewStateAnswer                    // answer received, showing answer
	ViewStateTrace                     // user pressed [T] to see trace
)

// Model is the root Bubbletea model.
type Model struct {
	engine *runner.Engine
	cfg    *config.Config

	state ViewState
	query string
	ready bool // true once terminal size is known

	width  int
	height int

	queryInput  textinput.Model
	traceView   viewport.Model
	answerView  viewport.Model
	spin        spinner.Model

	// Top bar data
	loopIndex int
	maxLoops  int
	costUSD   float64
	elapsed   time.Duration
	startTime time.Time

	// Status bar data
	activeTools []string
	nodeCount   int

	// Accumulated content
	traceLines []string
	answerText string

	channelReader *update.ChannelReader

	lastError string
}

// NewModel creates a Model ready to run.
func NewModel(engine *runner.Engine, cfg *config.Config) Model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = Spinner

	ti := textinput.New()
	ti.Placeholder = "Ask anything about your codebase..."
	ti.Focus()
	ti.CharLimit = 500
	ti.Width = 60

	return Model{
		engine:        engine,
		cfg:           cfg,
		state:         ViewStateInput,
		spin:          sp,
		queryInput:    ti,
		channelReader: update.NewChannelReader(engine.Channels()),
	}
}

// ── Init ─────────────────────────────────────────────────────────────────────

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		m.spin.Tick,
	)
}

// ── Update ───────────────────────────────────────────────────────────────────

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true
		m = m.recalculateSizes()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	// Engine channel messages
	case update.ThinkingMsg:
		return m.handleThinking(msg)
	case update.ActionMsg:
		return m.handleAction(msg)
	case update.MessageMsg:
		return m.handleMessage(msg)
	case update.WarningMsg:
		return m.handleWarning(msg)
	case update.ErrorMsg:
		return m.handleError(msg)
	case update.CostMsg:
		return m.handleCost(msg)
	case update.SystemMsg:
		return m.handleSystem(msg)
	case update.ProgressMsg:
		return m.handleProgress(msg)

	// BatchMsg: dispatch each message in turn
	case update.BatchMsg:
		var cmds []tea.Cmd
		var model tea.Model = m
		for _, inner := range msg.Messages {
			model, _ = model.Update(inner)
		}
		return model, tea.Batch(cmds...)

	case update.TickMsg:
		m.elapsed = time.Since(m.startTime)
		return m, tea.Batch(
			m.channelReader.PollCmd(),
			update.TickCmd(),
		)

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case update.QueryCompleteMsg:
		if m.answerText != "" {
			m.state = ViewStateAnswer
			m.answerView.SetContent(render.Markdown(m.answerText, m.width-4))
			m.answerView.GotoTop()
		}
		return m, nil
	}

	return m.updateFocused(msg)
}

// updateFocused delegates unhandled messages to the focused component.
func (m Model) updateFocused(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.state {
	case ViewStateInput:
		m.queryInput, cmd = m.queryInput.Update(msg)
	case ViewStateThinking, ViewStateTrace:
		m.traceView, cmd = m.traceView.Update(msg)
	case ViewStateAnswer:
		m.answerView, cmd = m.answerView.Update(msg)
	}
	return m, cmd
}

// ── Key handling ─────────────────────────────────────────────────────────────

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.state {

	case ViewStateInput:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "enter":
			query := strings.TrimSpace(m.queryInput.Value())
			if query == "" {
				return m, nil
			}
			return m.startQuery(query)
		default:
			var cmd tea.Cmd
			m.queryInput, cmd = m.queryInput.Update(msg)
			return m, cmd
		}

	case ViewStateThinking:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "up", "k":
			m.traceView.LineUp(1)
		case "down", "j":
			m.traceView.LineDown(1)
		case "g":
			m.traceView.GotoTop()
		case "G":
			m.traceView.GotoBottom()
		}
		return m, nil

	case ViewStateAnswer:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "t", "T":
			m.state = ViewStateTrace
			m.traceView.GotoTop()
		case "n", "N":
			m.resetForNewQuery()
			m.state = ViewStateInput
		case "up", "k":
			m.answerView.LineUp(1)
		case "down", "j":
			m.answerView.LineDown(1)
		case "g":
			m.answerView.GotoTop()
		case "G":
			m.answerView.GotoBottom()
		}
		return m, nil

	case ViewStateTrace:
		switch msg.String() {
		case "t", "T", "escape":
			m.state = ViewStateAnswer
		case "up", "k":
			m.traceView.LineUp(1)
		case "down", "j":
			m.traceView.LineDown(1)
		case "q":
			return m, tea.Quit
		}
		return m, nil
	}

	return m, nil
}

func (m Model) startQuery(query string) (Model, tea.Cmd) {
	m.query = query
	m.state = ViewStateThinking
	m.startTime = time.Now()
	m.traceLines = nil
	m.answerText = ""
	m.activeTools = nil
	m.nodeCount = 0
	m.costUSD = 0
	m.loopIndex = 0
	m.traceView.SetContent("")

	return m, tea.Batch(
		update.RunQueryCmd(m.engine, query),
		m.channelReader.PollCmd(),
		update.TickCmd(),
		m.spin.Tick,
	)
}

func (m *Model) resetForNewQuery() {
	m.query = ""
	m.traceLines = nil
	m.answerText = ""
	m.activeTools = nil
	m.nodeCount = 0
	m.costUSD = 0
	m.loopIndex = 0
	m.lastError = ""
	m.queryInput.SetValue("")
	m.queryInput.Focus()
	m.traceView.SetContent("")
	m.answerView.SetContent("")
}

// ── Message handlers ──────────────────────────────────────────────────────────

func (m Model) handleThinking(msg update.ThinkingMsg) (Model, tea.Cmd) {
	line := Dim.Render("  ↳ " + truncateLine(msg.Emission.Content, m.width-6))
	m.traceLines = append(m.traceLines, line)
	m.traceView.SetContent(strings.Join(m.traceLines, "\n"))
	m.traceView.GotoBottom()
	return m, nil
}

func (m Model) handleAction(msg update.ActionMsg) (Model, tea.Cmd) {
	content := msg.Emission.Content

	if strings.HasPrefix(content, "activating") {
		m.activeTools = extractToolNames(content)
	} else if strings.HasPrefix(content, "tool:") && strings.Contains(content, "complete") {
		toolName := extractCompletedTool(content)
		m.activeTools = removeFromSlice(m.activeTools, toolName)
	}

	line := Action.Render("  • " + truncateLine(content, m.width-6))
	m.traceLines = append(m.traceLines, line)
	m.traceView.SetContent(strings.Join(m.traceLines, "\n"))
	m.traceView.GotoBottom()
	return m, nil
}

func (m Model) handleMessage(msg update.MessageMsg) (Model, tea.Cmd) {
	m.answerText = msg.Emission.Content
	m.activeTools = nil
	return m, nil
}

func (m Model) handleSystem(msg update.SystemMsg) (Model, tea.Cmd) {
	content := msg.Emission.Content

	if strings.HasPrefix(content, "loop ") {
		m.loopIndex, m.maxLoops = parseLoopProgress(content)
	}

	line := Dim.Render("  " + content)
	m.traceLines = append(m.traceLines, line)
	m.traceView.SetContent(strings.Join(m.traceLines, "\n"))
	m.traceView.GotoBottom()
	return m, nil
}

func (m Model) handleCost(msg update.CostMsg) (Model, tea.Cmd) {
	m.costUSD = parseCostUSD(msg.Emission.Content)
	return m, nil
}

func (m Model) handleProgress(msg update.ProgressMsg) (Model, tea.Cmd) {
	if p, ok := msg.Emission.Metadata["nodes_created"].(int); ok {
		m.nodeCount = p
	}
	return m, nil
}

func (m Model) handleWarning(msg update.WarningMsg) (Model, tea.Cmd) {
	line := Warning.Render("  ⚠ " + truncateLine(msg.Emission.Content, m.width-6))
	m.traceLines = append(m.traceLines, line)
	m.traceView.SetContent(strings.Join(m.traceLines, "\n"))
	m.traceView.GotoBottom()
	return m, nil
}

func (m Model) handleError(msg update.ErrorMsg) (Model, tea.Cmd) {
	m.lastError = msg.Emission.Content
	line := Error.Render("  ✗ " + truncateLine(msg.Emission.Content, m.width-6))
	m.traceLines = append(m.traceLines, line)
	m.traceView.SetContent(strings.Join(m.traceLines, "\n"))
	m.traceView.GotoBottom()
	return m, nil
}

// ── View ──────────────────────────────────────────────────────────────────────

func (m Model) View() string {
	if !m.ready {
		return "\n  Initializing..."
	}

	return lipgloss.JoinVertical(
		lipgloss.Left,
		m.renderTopBar(),
		m.renderMainArea(),
		m.renderStatusBar(),
	)
}

func (m Model) recalculateSizes() Model {
	topBarHeight    := 3
	statusBarHeight := 1
	mainHeight      := m.height - topBarHeight - statusBarHeight - 2
	if mainHeight < 1 {
		mainHeight = 1
	}

	m.traceView  = viewport.New(m.width, mainHeight)
	m.answerView = viewport.New(m.width, mainHeight)
	m.traceView.Style  = Viewport
	m.answerView.Style = Viewport
	return m
}

// ── Top bar ───────────────────────────────────────────────────────────────────

func (m Model) renderTopBar() string {
	queryLabel := Label.Render("query: ")
	queryText  := QueryText.Render(truncateLine(m.query, m.width-10))

	loopStr := renderLoopProgress(m.loopIndex, m.maxLoops)
	costStr := renderCost(m.costUSD)
	timeStr := renderElapsed(m.elapsed)

	toolsStr := ""
	if m.state == ViewStateThinking && len(m.activeTools) > 0 {
		toolsStr = Active.Render(fmt.Sprintf("%d tool%s active",
			len(m.activeTools), pluralize(len(m.activeTools))))
	}

	meta := strings.Join(filterEmpty([]string{loopStr, toolsStr, costStr, timeStr}), "  |  ")

	return TopBar.Render(
		lipgloss.JoinVertical(lipgloss.Left,
			queryLabel+queryText,
			Dim.Render(meta),
		),
	)
}

func renderLoopProgress(current, max int) string {
	if max == 0 {
		return ""
	}
	filled := current
	if filled > max {
		filled = max
	}
	empty := max - filled

	bar := LoopFilled.Render(strings.Repeat("●", filled)) +
		LoopEmpty.Render(strings.Repeat("○", empty))

	return fmt.Sprintf("loop %d/%d  %s", current, max, bar)
}

func renderCost(usd float64) string {
	if usd == 0 {
		return ""
	}
	if usd < 0.01 {
		return Dim.Render(fmt.Sprintf("$%.4f", usd))
	}
	return Dim.Render(fmt.Sprintf("$%.3f", usd))
}

func renderElapsed(d time.Duration) string {
	if d == 0 {
		return ""
	}
	if d < time.Second {
		return Dim.Render(fmt.Sprintf("%.0fms", float64(d.Milliseconds())))
	}
	return Dim.Render(fmt.Sprintf("%.1fs", d.Seconds()))
}

// ── Main area ─────────────────────────────────────────────────────────────────

func (m Model) renderMainArea() string {
	switch m.state {

	case ViewStateInput:
		return m.renderInputPrompt()

	case ViewStateThinking:
		header := lipgloss.JoinHorizontal(lipgloss.Left,
			m.spin.View(), " ", Active.Render("investigating"),
		)
		return lipgloss.JoinVertical(lipgloss.Left,
			TraceHeader.Render(header),
			m.traceView.View(),
		)

	case ViewStateAnswer:
		header := AnswerHeader.Render("Answer")
		hint   := Dim.Render("[T] view trace  [N] new query  [↑↓] scroll")
		return lipgloss.JoinVertical(lipgloss.Left,
			lipgloss.JoinHorizontal(lipgloss.Left, header, "  ", hint),
			m.answerView.View(),
		)

	case ViewStateTrace:
		header := TraceHeader.Render("Investigation trace")
		hint   := Dim.Render("[T] back to answer  [↑↓] scroll")
		return lipgloss.JoinVertical(lipgloss.Left,
			lipgloss.JoinHorizontal(lipgloss.Left, header, "  ", hint),
			m.traceView.View(),
		)
	}

	return ""
}

func (m Model) renderInputPrompt() string {
	title    := Title.Render("Context Engine")
	subtitle := Dim.Render("Ask anything about your codebase")

	prompt := lipgloss.JoinVertical(lipgloss.Left,
		title,
		subtitle,
		"",
		m.queryInput.View(),
		"",
		Dim.Render("[enter] submit  [ctrl+c] quit"),
	)

	mainHeight := m.height - 6
	topPad := (mainHeight - 8) / 2
	if topPad < 0 {
		topPad = 0
	}

	return strings.Repeat("\n", topPad) +
		lipgloss.NewStyle().
			Width(m.width).
			Align(lipgloss.Center).
			Render(prompt)
}

// ── Status bar ────────────────────────────────────────────────────────────────

func (m Model) renderStatusBar() string {
	left := m.renderToolIndicators()

	nodeStr := ""
	if m.nodeCount > 0 {
		nodeStr = Dim.Render(fmt.Sprintf("%d nodes", m.nodeCount))
	}
	errorStr := ""
	if m.lastError != "" {
		errorStr = Error.Render("⚠ error")
	}

	var shortcuts string
	switch m.state {
	case ViewStateInput:
		shortcuts = Dim.Render("q quit")
	case ViewStateThinking:
		shortcuts = Dim.Render("ctrl+c cancel")
	case ViewStateAnswer:
		shortcuts = Dim.Render("t trace  n new  q quit")
	case ViewStateTrace:
		shortcuts = Dim.Render("t answer  q quit")
	}

	middle := strings.Join(filterEmpty([]string{nodeStr, errorStr}), "  |  ")
	right  := shortcuts

	middleWidth := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 4
	if middleWidth < 0 {
		middleWidth = 0
	}
	middle = lipgloss.NewStyle().Width(middleWidth).Align(lipgloss.Center).Render(middle)

	return StatusBar.Render(
		lipgloss.JoinHorizontal(lipgloss.Left, left, middle, right),
	)
}

func (m Model) renderToolIndicators() string {
	allTools := []string{"callgraph", "references", "crossproject", "concepts", "filecontext", "summary"}

	activeSet := make(map[string]bool)
	for _, t := range m.activeTools {
		activeSet[t] = true
	}

	var parts []string
	for _, tool := range allTools {
		if activeSet[tool] {
			parts = append(parts, ToolActive.Render("● "+tool))
		} else {
			parts = append(parts, ToolInactive.Render("○ "+tool))
		}
	}

	return strings.Join(parts, "  ")
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func truncateLine(s string, max int) string {
	if max <= 0 {
		return s
	}
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

func pluralize(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func filterEmpty(ss []string) []string {
	var out []string
	for _, s := range ss {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func removeFromSlice(ss []string, target string) []string {
	var out []string
	for _, s := range ss {
		if s != target {
			out = append(out, s)
		}
	}
	return out
}

// extractToolNames parses "activating 3 tools: callgraph, references, summary"
func extractToolNames(content string) []string {
	idx := strings.Index(content, ": ")
	if idx == -1 {
		return nil
	}
	parts := strings.Split(content[idx+2:], ", ")
	var names []string
	for _, p := range parts {
		if name := strings.TrimSpace(p); name != "" {
			names = append(names, name)
		}
	}
	return names
}

// extractCompletedTool parses "tool:callgraph complete (1 emissions)"
func extractCompletedTool(content string) string {
	// Format: "tool:<name> complete..."
	rest := strings.TrimPrefix(content, "tool:")
	if idx := strings.Index(rest, " "); idx != -1 {
		return rest[:idx]
	}
	return rest
}

// parseLoopProgress parses "loop N/M" → (N, M)
func parseLoopProgress(content string) (int, int) {
	// "loop 2/5"
	rest := strings.TrimPrefix(content, "loop ")
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) != 2 {
		return 0, 0
	}
	current, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
	max, _     := strconv.Atoi(strings.TrimSpace(parts[1]))
	return current, max
}

// parseCostUSD attempts to parse a cost value from a cost emission's content.
func parseCostUSD(content string) float64 {
	if v, ok := tryParseFloat(content); ok {
		return v
	}
	return 0
}

func tryParseFloat(s string) (float64, bool) {
	s = strings.TrimSpace(strings.TrimPrefix(s, "$"))
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f, true
	}
	return 0, false
}
