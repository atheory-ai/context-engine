# Context Engine — Spec 13: TUI
## Implementation Spec — Bubbletea Dashboard, Component Tree, Channel Rendering
### Version 1.0 | February 2026

---

> This spec covers the interactive terminal UI for `ce query`.
> Hand to Claude Code alongside spec-2-packages.md, spec-3-engine-runner.md,
> and spec-6-cli-config.md.
> Companion: Context Engine PRD v0.5 Section 15. Decisions Log v1.0 Section 9.

---

## 1. Overview

The TUI is the primary interactive interface for the engine. It launches
when `ce query` is run without arguments, or with `--tui` flag.

It is a Bubbletea application with three regions:

```
┌─────────────────────────────────────────────────────────────────┐
│ TOP BAR                                                         │
│ query: "how does volunteer assignment affect billing?"          │
│ loop 2/5  ●●●○○  |  3 tools active  |  $0.0042  |  4.2s       │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│ MAIN AREA                                                       │
│                                                                 │
│ [During query]   streaming thought process                      │
│                  tool findings as they arrive                   │
│                  reviewer decisions                             │
│                                                                 │
│ [After query]    final answer (markdown rendered)               │
│                  with [T] shortcut to re-open trace             │
│                                                                 │
├─────────────────────────────────────────────────────────────────┤
│ STATUS BAR                                                      │
│ ● callgraph  ● references  ○ crossproject  |  42 nodes  |  q quit │
└─────────────────────────────────────────────────────────────────┘
```

The main area transitions from trace view to answer view when the
Synthesizer emits to ChanMessage. The trace is preserved and accessible
via [T] key after the answer appears.

---

## 2. Package Structure

```
tui/
  model.go          — root Model, Init(), Update(), View()
  styles.go         — Lipgloss style definitions
  keys.go           — key bindings
  components/
    topbar.go       — query display, loop progress, cost, elapsed time
    trace.go        — streaming thought process (viewport)
    answer.go       — final answer renderer (viewport + glamour markdown)
    statusbar.go    — tool status indicators, node count, shortcuts
    input.go        — query input (used in interactive mode)
    spinner.go      — thinking indicator
  update/
    channels.go     — channel → Msg converters (tick-based polling)
    events.go       — all Msg types
  render/
    markdown.go     — glamour markdown rendering
    highlight.go    — syntax highlighting for code blocks
```

---

## 3. Dependencies

```go
// go.mod additions
require (
    github.com/charmbracelet/bubbletea  v0.x.x
    github.com/charmbracelet/lipgloss   v0.x.x
    github.com/charmbracelet/bubbles    v0.x.x  // viewport, textinput, spinner
    github.com/charmbracelet/glamour    v0.x.x  // markdown rendering
)
```

---

## 4. Root Model

```go
// tui/model.go

package tui

import (
    "github.com/charmbracelet/bubbletea"
    "github.com/charmbracelet/bubbles/viewport"
    "github.com/charmbracelet/bubbles/textinput"
    "github.com/charmbracelet/bubbles/spinner"
)

// ViewState controls which view the main area shows.
type ViewState int

const (
    ViewStateInput     ViewState = iota // waiting for query input
    ViewStateThinking                   // query running, showing trace
    ViewStateAnswer                     // answer received, showing answer
    ViewStateTrace                      // user pressed [T] to see trace
)

// Model is the root Bubbletea model.
type Model struct {
    // Engine reference
    engine *runner.Engine
    cfg    *config.Config

    // View state
    state      ViewState
    query      string
    ready      bool  // true once terminal size is known

    // Terminal dimensions
    width  int
    height int

    // Components
    queryInput  textinput.Model
    traceView   viewport.Model
    answerView  viewport.Model
    spinner     spinner.Model

    // Top bar data
    loopIndex   int
    maxLoops    int
    costUSD     float64
    elapsed     time.Duration
    startTime   time.Time

    // Status bar data
    activeTools  []string  // tool names currently executing
    nodeCount    int
    errorCount   int

    // Accumulated content
    traceLines   []string  // all trace content (thinking, actions, etc.)
    answerText   string    // final answer markdown

    // Channel reader — polls engine channels
    channelReader *ChannelReader

    // Error display
    lastError string
}

func NewModel(engine *runner.Engine, cfg *config.Config) Model {
    sp := spinner.New()
    sp.Spinner = spinner.Dot
    sp.Style = styles.Spinner

    ti := textinput.New()
    ti.Placeholder = "Ask anything about your codebase..."
    ti.Focus()
    ti.CharLimit = 500
    ti.Width = 60

    return Model{
        engine:      engine,
        cfg:         cfg,
        state:       ViewStateInput,
        spinner:     sp,
        queryInput:  ti,
        channelReader: NewChannelReader(engine.Channels()),
    }
}
```

---

## 5. Init and Update

```go
// tui/model.go (continued)

func (m Model) Init() tea.Cmd {
    return tea.Batch(
        textinput.Blink,
        m.spinner.Tick,
    )
}

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
    case ThinkingMsg:
        return m.handleThinking(msg)
    case ActionMsg:
        return m.handleAction(msg)
    case MessageMsg:
        return m.handleMessage(msg)
    case WarningMsg:
        return m.handleWarning(msg)
    case ErrorMsg:
        return m.handleError(msg)
    case CostMsg:
        return m.handleCost(msg)
    case SystemMsg:
        return m.handleSystem(msg)
    case ProgressMsg:
        return m.handleProgress(msg)

    // Tick for polling channels + elapsed timer
    case TickMsg:
        cmds := []tea.Cmd{
            m.channelReader.PollCmd(),
            tickCmd(),
        }
        m.elapsed = time.Since(m.startTime)
        return m, tea.Batch(cmds...)

    case spinner.TickMsg:
        var cmd tea.Cmd
        m.spinner, cmd = m.spinner.Update(msg)
        return m, cmd

    case QueryCompleteMsg:
        // Query finished — if we have an answer, show it
        if m.answerText != "" {
            m.state = ViewStateAnswer
            m.answerView.SetContent(renderMarkdown(m.answerText, m.width-4))
            m.answerView.GotoTop()
        }
        return m, nil
    }

    // Delegate to focused component
    return m.updateFocused(msg)
}
```

### Key handling

```go
// tui/model.go (continued)

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
            // Cancel the running query
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
            // Toggle to trace view
            m.state = ViewStateTrace
            m.traceView.GotoTop()
        case "n", "N":
            // New query
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
            // Back to answer
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

    // Reset viewports
    m.traceView.SetContent("")

    return m, tea.Batch(
        // Run query in background
        runQueryCmd(m.engine, query),
        // Start polling channels
        m.channelReader.PollCmd(),
        // Start elapsed timer
        tickCmd(),
        // Spinner
        m.spinner.Tick,
    )
}
```

---

## 6. Channel Reader

The channel reader bridges Bubbletea's message system and the engine's
Go channels. It runs a poll on each tick rather than blocking goroutines,
which keeps Bubbletea's single-threaded model intact.

```go
// tui/update/channels.go

package update

// ChannelReader polls the engine's AppChannels and converts
// emissions to Bubbletea Msg types.
type ChannelReader struct {
    ch *core.AppChannels
}

func NewChannelReader(ch *core.AppChannels) *ChannelReader {
    return &ChannelReader{ch: ch}
}

// PollCmd returns a Bubbletea command that drains all pending
// channel messages in a single tick. Non-blocking.
func (r *ChannelReader) PollCmd() tea.Cmd {
    return func() tea.Msg {
        return r.poll()
    }
}

// BatchMsg carries multiple messages from a single poll.
type BatchMsg struct {
    Messages []tea.Msg
}

func (r *ChannelReader) poll() tea.Msg {
    var messages []tea.Msg

    // Drain each channel — non-blocking select
    for {
        select {
        case e, ok := <-r.ch.Thinking:
            if ok {
                messages = append(messages, ThinkingMsg{Emission: e})
                continue
            }
        case e, ok := <-r.ch.Action:
            if ok {
                messages = append(messages, ActionMsg{Emission: e})
                continue
            }
        case e, ok := <-r.ch.Message:
            if ok {
                messages = append(messages, MessageMsg{Emission: e})
                continue
            }
        case e, ok := <-r.ch.Warning:
            if ok {
                messages = append(messages, WarningMsg{Emission: e})
                continue
            }
        case e, ok := <-r.ch.Error:
            if ok {
                messages = append(messages, ErrorMsg{Emission: e})
                continue
            }
        case e, ok := <-r.ch.Cost:
            if ok {
                messages = append(messages, CostMsg{Emission: e})
                continue
            }
        case e, ok := <-r.ch.System:
            if ok {
                messages = append(messages, SystemMsg{Emission: e})
                continue
            }
        case e, ok := <-r.ch.Progress:
            if ok {
                messages = append(messages, ProgressMsg{Emission: e})
                continue
            }
        default:
            // All channels empty
            if len(messages) == 0 {
                return nil
            }
            return BatchMsg{Messages: messages}
        }
    }
}
```

### Msg types

```go
// tui/update/events.go

package update

import "github.com/atheory/context-engine/internal/core"

type ThinkingMsg  struct { Emission core.Emission }
type ActionMsg    struct { Emission core.Emission }
type MessageMsg   struct { Emission core.Emission }
type WarningMsg   struct { Emission core.Emission }
type ErrorMsg     struct { Emission core.Emission }
type CostMsg      struct { Emission core.Emission }
type SystemMsg    struct { Emission core.Emission }
type ProgressMsg  struct { Emission core.Emission }

type TickMsg         struct{}
type QueryCompleteMsg struct{ Err error }

// tickCmd returns a command that fires after a short interval.
// Used for elapsed timer and channel polling cadence.
func TickCmd() tea.Cmd {
    return tea.Tick(50*time.Millisecond, func(t time.Time) tea.Msg {
        return TickMsg{}
    })
}

// RunQueryCmd runs a query in a background goroutine and sends
// QueryCompleteMsg when done.
func RunQueryCmd(engine *runner.Engine, query string) tea.Cmd {
    return func() tea.Msg {
        err := engine.Query(context.Background(), query)
        return QueryCompleteMsg{Err: err}
    }
}
```

---

## 7. Message Handlers

```go
// tui/model.go (message handlers)

func (m Model) handleThinking(msg ThinkingMsg) (Model, tea.Cmd) {
    // Add to trace with dim styling
    line := styles.Dim.Render("  ↳ " + truncateLine(msg.Emission.Content, m.width-6))
    m.traceLines = append(m.traceLines, line)
    m.traceView.SetContent(strings.Join(m.traceLines, "\n"))
    m.traceView.GotoBottom()
    return m, nil
}

func (m Model) handleAction(msg ActionMsg) (Model, tea.Cmd) {
    content := msg.Emission.Content

    // Detect tool start/complete from action content
    if strings.HasPrefix(content, "activating") {
        m.activeTools = extractToolNames(content)
    } else if strings.HasPrefix(content, "tool:") && strings.Contains(content, "complete") {
        toolName := extractCompletedTool(content)
        m.activeTools = removeFromSlice(m.activeTools, toolName)
    }

    line := styles.Action.Render("  • " + truncateLine(content, m.width-6))
    m.traceLines = append(m.traceLines, line)
    m.traceView.SetContent(strings.Join(m.traceLines, "\n"))
    m.traceView.GotoBottom()
    return m, nil
}

func (m Model) handleMessage(msg MessageMsg) (Model, tea.Cmd) {
    // Final answer received
    m.answerText = msg.Emission.Content
    m.activeTools = nil
    // State transition to ViewStateAnswer happens in QueryCompleteMsg handler
    return m, nil
}

func (m Model) handleSystem(msg SystemMsg) (Model, tea.Cmd) {
    content := msg.Emission.Content

    // Parse loop progress from system messages
    // Format: "loop N/M"
    if strings.HasPrefix(content, "loop ") {
        m.loopIndex, m.maxLoops = parseLoopProgress(content)
    }

    // Parse node count from indexing progress
    if strings.Contains(content, "nodes") {
        m.nodeCount = parseNodeCount(content)
    }

    line := styles.Dim.Render("  " + content)
    m.traceLines = append(m.traceLines, line)
    m.traceView.SetContent(strings.Join(m.traceLines, "\n"))
    m.traceView.GotoBottom()
    return m, nil
}

func (m Model) handleCost(msg CostMsg) (Model, tea.Cmd) {
    m.costUSD = parseCostUSD(msg.Emission.Content)
    return m, nil
}

func (m Model) handleProgress(msg ProgressMsg) (Model, tea.Cmd) {
    // Indexing progress — update node count
    if p, ok := msg.Emission.Metadata["nodes_created"].(int); ok {
        m.nodeCount = p
    }
    return m, nil
}

func (m Model) handleWarning(msg WarningMsg) (Model, tea.Cmd) {
    line := styles.Warning.Render("  ⚠ " + truncateLine(msg.Emission.Content, m.width-6))
    m.traceLines = append(m.traceLines, line)
    m.traceView.SetContent(strings.Join(m.traceLines, "\n"))
    m.traceView.GotoBottom()
    return m, nil
}

func (m Model) handleError(msg ErrorMsg) (Model, tea.Cmd) {
    m.lastError = msg.Emission.Content
    line := styles.Error.Render("  ✗ " + truncateLine(msg.Emission.Content, m.width-6))
    m.traceLines = append(m.traceLines, line)
    m.traceView.SetContent(strings.Join(m.traceLines, "\n"))
    m.traceView.GotoBottom()
    return m, nil
}
```

---

## 8. View

```go
// tui/model.go (View)

func (m Model) View() string {
    if !m.ready {
        return "\n  Initializing..."
    }

    topBar    := m.renderTopBar()
    mainArea  := m.renderMainArea()
    statusBar := m.renderStatusBar()

    return lipgloss.JoinVertical(
        lipgloss.Left,
        topBar,
        mainArea,
        statusBar,
    )
}

func (m Model) recalculateSizes() Model {
    topBarHeight    := 3
    statusBarHeight := 1
    mainHeight      := m.height - topBarHeight - statusBarHeight - 2

    m.traceView  = viewport.New(m.width, mainHeight)
    m.answerView = viewport.New(m.width, mainHeight)

    m.traceView.Style  = styles.Viewport
    m.answerView.Style = styles.Viewport

    return m
}
```

---

## 9. Top Bar

```go
// tui/components/topbar.go

func (m Model) renderTopBar() string {
    // Line 1: query text
    queryLabel := styles.Label.Render("query: ")
    queryText  := styles.QueryText.Render(truncateLine(m.query, m.width-10))

    // Line 2: loop progress + tools active + cost + elapsed
    loopStr := renderLoopProgress(m.loopIndex, m.maxLoops)
    costStr := renderCost(m.costUSD)
    timeStr := renderElapsed(m.elapsed)

    toolsStr := ""
    if m.state == ViewStateThinking && len(m.activeTools) > 0 {
        toolsStr = styles.Active.Render(
            fmt.Sprintf("%d tool%s active",
                len(m.activeTools),
                pluralize(len(m.activeTools))))
    }

    meta := strings.Join(filterEmpty([]string{
        loopStr, toolsStr, costStr, timeStr,
    }), "  |  ")

    return styles.TopBar.Render(
        lipgloss.JoinVertical(lipgloss.Left,
            queryLabel+queryText,
            styles.Dim.Render(meta),
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

    bar := styles.LoopFilled.Render(strings.Repeat("●", filled)) +
           styles.LoopEmpty.Render(strings.Repeat("○", empty))

    return fmt.Sprintf("loop %d/%d  %s", current, max, bar)
}

func renderCost(usd float64) string {
    if usd == 0 {
        return ""
    }
    if usd < 0.01 {
        return styles.Dim.Render(fmt.Sprintf("$%.4f", usd))
    }
    return styles.Dim.Render(fmt.Sprintf("$%.3f", usd))
}

func renderElapsed(d time.Duration) string {
    if d == 0 {
        return ""
    }
    if d < time.Second {
        return styles.Dim.Render(fmt.Sprintf("%.0fms", float64(d.Milliseconds())))
    }
    return styles.Dim.Render(fmt.Sprintf("%.1fs", d.Seconds()))
}
```

---

## 10. Main Area

```go
// tui/components/trace.go and answer.go

func (m Model) renderMainArea() string {
    switch m.state {

    case ViewStateInput:
        return m.renderInputPrompt()

    case ViewStateThinking:
        // Show trace viewport with spinner header
        header := lipgloss.JoinHorizontal(lipgloss.Left,
            m.spinner.View(),
            " ",
            styles.Active.Render("investigating"),
        )
        return lipgloss.JoinVertical(lipgloss.Left,
            styles.TraceHeader.Render(header),
            m.traceView.View(),
        )

    case ViewStateAnswer:
        // Show answer viewport
        header := styles.AnswerHeader.Render("Answer")
        hint := styles.Dim.Render("[T] view trace  [N] new query  [↑↓] scroll")
        return lipgloss.JoinVertical(lipgloss.Left,
            lipgloss.JoinHorizontal(lipgloss.Left,
                header, "  ", hint),
            m.answerView.View(),
        )

    case ViewStateTrace:
        // Show trace viewport over the answer
        header := styles.TraceHeader.Render("Investigation trace")
        hint := styles.Dim.Render("[T] back to answer  [↑↓] scroll")
        return lipgloss.JoinVertical(lipgloss.Left,
            lipgloss.JoinHorizontal(lipgloss.Left,
                header, "  ", hint),
            m.traceView.View(),
        )
    }

    return ""
}

func (m Model) renderInputPrompt() string {
    title := styles.Title.Render("Context Engine")
    subtitle := styles.Dim.Render("Ask anything about your codebase")

    prompt := lipgloss.JoinVertical(lipgloss.Left,
        title,
        subtitle,
        "",
        m.queryInput.View(),
        "",
        styles.Dim.Render("[enter] submit  [ctrl+c] quit"),
    )

    // Center vertically in main area
    mainHeight := m.height - 6 // account for top/status bars
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
```

---

## 11. Status Bar

```go
// tui/components/statusbar.go

func (m Model) renderStatusBar() string {
    // Left: tool indicators
    toolIndicators := m.renderToolIndicators()

    // Center: node count (during indexing or after)
    nodeStr := ""
    if m.nodeCount > 0 {
        nodeStr = styles.Dim.Render(fmt.Sprintf("%d nodes", m.nodeCount))
    }

    // Right: shortcuts
    var shortcuts string
    switch m.state {
    case ViewStateInput:
        shortcuts = styles.Dim.Render("q quit")
    case ViewStateThinking:
        shortcuts = styles.Dim.Render("ctrl+c cancel")
    case ViewStateAnswer:
        shortcuts = styles.Dim.Render("t trace  n new  q quit")
    case ViewStateTrace:
        shortcuts = styles.Dim.Render("t answer  q quit")
    }

    // Error indicator
    errorStr := ""
    if m.lastError != "" {
        errorStr = styles.Error.Render("⚠ error")
    }

    left   := toolIndicators
    middle := strings.Join(filterEmpty([]string{nodeStr, errorStr}), "  |  ")
    right  := shortcuts

    // Distribute across full width
    middleWidth := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 4
    if middleWidth < 0 {
        middleWidth = 0
    }

    middle = lipgloss.NewStyle().Width(middleWidth).Align(lipgloss.Center).Render(middle)

    return styles.StatusBar.Render(
        lipgloss.JoinHorizontal(lipgloss.Left, left, middle, right),
    )
}

func (m Model) renderToolIndicators() string {
    // All six built-in tools
    allTools := []string{"callgraph", "references", "crossproject",
                         "concepts", "filecontext", "summary"}

    activeSet := make(map[string]bool)
    for _, t := range m.activeTools {
        activeSet[t] = true
    }

    var parts []string
    for _, tool := range allTools {
        if activeSet[tool] {
            parts = append(parts, styles.ToolActive.Render("● "+tool))
        } else {
            parts = append(parts, styles.ToolInactive.Render("○ "+tool))
        }
    }

    return strings.Join(parts, "  ")
}
```

---

## 12. Styles

```go
// tui/styles.go

package tui

import "github.com/charmbracelet/lipgloss"

var (
    // Colors
    colorPrimary   = lipgloss.Color("#7C3AED") // violet
    colorSecondary = lipgloss.Color("#6D28D9")
    colorMuted     = lipgloss.Color("#6B7280")
    colorActive    = lipgloss.Color("#10B981") // emerald
    colorWarning   = lipgloss.Color("#F59E0B")
    colorError     = lipgloss.Color("#EF4444")
    colorDim       = lipgloss.Color("#4B5563")
    colorText      = lipgloss.Color("#F9FAFB")
    colorBg        = lipgloss.Color("#111827")
    colorBgAlt     = lipgloss.Color("#1F2937")
    colorBorder    = lipgloss.Color("#374151")

    // Base styles
    Dim = lipgloss.NewStyle().Foreground(colorDim)

    Label = lipgloss.NewStyle().
        Foreground(colorMuted).
        Bold(false)

    Active = lipgloss.NewStyle().
        Foreground(colorActive)

    Warning = lipgloss.NewStyle().
        Foreground(colorWarning)

    Error = lipgloss.NewStyle().
        Foreground(colorError)

    // Component styles
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

    Viewport = lipgloss.NewStyle().
        Padding(0, 1)

    TraceHeader = lipgloss.NewStyle().
        Foreground(colorMuted).
        Padding(0, 1)

    AnswerHeader = lipgloss.NewStyle().
        Foreground(colorPrimary).
        Bold(true).
        Padding(0, 1)

    Title = lipgloss.NewStyle().
        Foreground(colorPrimary).
        Bold(true).
        MarginBottom(1)

    QueryText = lipgloss.NewStyle().
        Foreground(colorText)

    Spinner = lipgloss.NewStyle().
        Foreground(colorActive)

    // Loop progress
    LoopFilled = lipgloss.NewStyle().
        Foreground(colorPrimary)

    LoopEmpty = lipgloss.NewStyle().
        Foreground(colorDim)

    // Tool indicators
    ToolActive = lipgloss.NewStyle().
        Foreground(colorActive)

    ToolInactive = lipgloss.NewStyle().
        Foreground(colorDim)

    Action = lipgloss.NewStyle().
        Foreground(colorPrimary)
)
```

---

## 13. Markdown Rendering

The final answer is rendered as markdown using glamour with a dark theme
that matches the TUI color scheme.

```go
// tui/render/markdown.go

package render

import "github.com/charmbracelet/glamour"

var markdownRenderer *glamour.TermRenderer

func init() {
    var err error
    markdownRenderer, err = glamour.NewTermRenderer(
        glamour.WithStylePath("dark"),
        glamour.WithWordWrap(0), // width handled by viewport
    )
    if err != nil {
        // Fallback — plain text if glamour fails
        markdownRenderer = nil
    }
}

// Markdown renders markdown content for display in the answer viewport.
// Falls back to plain text if rendering fails.
func Markdown(content string, width int) string {
    if markdownRenderer == nil {
        return content
    }

    renderer, err := glamour.NewTermRenderer(
        glamour.WithStylePath("dark"),
        glamour.WithWordWrap(width),
    )
    if err != nil {
        return content
    }

    rendered, err := renderer.Render(content)
    if err != nil {
        return content
    }

    return rendered
}
```

---

## 14. Key Bindings Reference

```go
// tui/keys.go

package tui

// Key bindings by view state.
// Displayed in status bar and help text.
var KeyBindings = map[ViewState][]KeyBinding{
    ViewStateInput: {
        {Key: "enter",  Desc: "submit query"},
        {Key: "ctrl+c", Desc: "quit"},
    },
    ViewStateThinking: {
        {Key: "↑/k",    Desc: "scroll up"},
        {Key: "↓/j",    Desc: "scroll down"},
        {Key: "g",      Desc: "top"},
        {Key: "G",      Desc: "bottom"},
        {Key: "ctrl+c", Desc: "cancel"},
    },
    ViewStateAnswer: {
        {Key: "↑/k",    Desc: "scroll up"},
        {Key: "↓/j",    Desc: "scroll down"},
        {Key: "g/G",    Desc: "top/bottom"},
        {Key: "t",      Desc: "view trace"},
        {Key: "n",      Desc: "new query"},
        {Key: "q",      Desc: "quit"},
    },
    ViewStateTrace: {
        {Key: "↑/k",    Desc: "scroll up"},
        {Key: "↓/j",    Desc: "scroll down"},
        {Key: "t/esc",  Desc: "back to answer"},
        {Key: "q",      Desc: "quit"},
    },
}

type KeyBinding struct {
    Key  string
    Desc string
}
```

---

## 15. TUI Entry Point

```go
// tui/tui.go

package tui

// Run starts the TUI. Called from cli/query.go when in TUI mode.
func Run(engine *runner.Engine, cfg *config.Config) error {
    model := NewModel(engine, cfg)

    p := tea.NewProgram(
        model,
        tea.WithAltScreen(),       // full screen
        tea.WithMouseCellMotion(), // mouse scroll support
    )

    _, err := p.Run()
    return err
}

// RunWithQuery starts the TUI with a pre-filled query and immediately runs it.
// Used when --tui flag is provided alongside query text.
func RunWithQuery(engine *runner.Engine, cfg *config.Config, query string) error {
    model := NewModel(engine, cfg)

    // Pre-fill and auto-submit the query
    model.queryInput.SetValue(query)
    model, _ = model.startQuery(query)

    p := tea.NewProgram(
        model,
        tea.WithAltScreen(),
        tea.WithMouseCellMotion(),
    )

    _, err := p.Run()
    return err
}
```

---

## 16. CLI Integration Amendment

```go
// cli/query.go (amended runQuery)

func runQuery(cmd *cobra.Command, args []string) error {
    cfg, err := config.Load()
    if err != nil {
        return err
    }

    ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
    defer cancel()

    engine, err := runner.New(ctx, cfg)
    if err != nil {
        return fmt.Errorf("engine init: %w", err)
    }
    defer engine.Close(context.Background())

    forceTUI, _ := cmd.Flags().GetBool("tui")

    switch {
    case len(args) == 0 || forceTUI && len(args) == 0:
        // Interactive TUI — no query
        return tui.Run(engine, cfg)

    case forceTUI && len(args) > 0:
        // TUI with pre-filled query
        return tui.RunWithQuery(engine, cfg, strings.Join(args, " "))

    default:
        // CLI mode — plain renderer
        return runCLIQuery(cmd, cfg, strings.Join(args, " "))
    }
}
```

---

## 17. Package Layout Summary

```
tui/
  tui.go              — Run(), RunWithQuery() entry points
  model.go            — Model struct, Init(), Update(), View()
                        all message handlers (handleThinking etc.)
                        startQuery(), resetForNewQuery()
                        recalculateSizes()
  styles.go           — all Lipgloss style definitions
  keys.go             — KeyBindings map, KeyBinding type
  components/
    topbar.go         — renderTopBar(), renderLoopProgress(),
                        renderCost(), renderElapsed()
    mainarea.go       — renderMainArea(), renderInputPrompt()
    statusbar.go      — renderStatusBar(), renderToolIndicators()
  update/
    channels.go       — ChannelReader, PollCmd(), BatchMsg
    events.go         — all Msg types, TickCmd(), RunQueryCmd()
  render/
    markdown.go       — Markdown(), glamour renderer
```

---

## 18. Key Decisions Captured in This Spec

| Decision | Value |
|----------|-------|
| TUI library | Bubbletea |
| Layout | Three-region dashboard (top bar / main area / status bar) |
| Main area states | Input → Thinking → Answer, with Trace accessible via [T] |
| Channel bridging | Poll-based (50ms tick) — not blocking goroutines |
| Trace preservation | Always preserved — accessible after answer via [T] |
| Answer rendering | Glamour markdown with dark theme |
| Tool indicators | All 6 built-ins shown in status bar, active/inactive state |
| Loop progress | Dot progress bar (●●○○○) in top bar |
| Cost display | Top bar, hidden until first LLM call |
| Elapsed timer | Top bar, 50ms resolution |
| Mouse support | Scroll only (WithMouseCellMotion) |
| Full screen | AltScreen — restores terminal on exit |
| Vim-style navigation | j/k for scroll, g/G for top/bottom |
| New query | [N] key resets without restarting the process |
| Cancel | Ctrl+C cancels running query via context cancellation |

---

*Spec 13: TUI — v1.0 — February 2026*
*Next: Spec 14 — MCP + API Server*
*Companion: Context Engine PRD v0.5 Section 15 | Decisions Log v1.0 Section 9*
