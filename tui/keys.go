package tui

// KeyBinding is a single key → description pair for display in the status bar.
type KeyBinding struct {
	Key  string
	Desc string
}

// KeyBindings maps each ViewState to its relevant key bindings.
var KeyBindings = map[ViewState][]KeyBinding{
	ViewStateInput: {
		{Key: "enter", Desc: "submit query"},
		{Key: "ctrl+c", Desc: "quit"},
	},
	ViewStateThinking: {
		{Key: "↑/k", Desc: "scroll up"},
		{Key: "↓/j", Desc: "scroll down"},
		{Key: "g", Desc: "top"},
		{Key: "G", Desc: "bottom"},
		{Key: "ctrl+c", Desc: "cancel"},
	},
	ViewStateAnswer: {
		{Key: "↑/k", Desc: "scroll up"},
		{Key: "↓/j", Desc: "scroll down"},
		{Key: "g/G", Desc: "top/bottom"},
		{Key: "t", Desc: "view trace"},
		{Key: "n", Desc: "new query"},
		{Key: "q", Desc: "quit"},
	},
	ViewStateTrace: {
		{Key: "↑/k", Desc: "scroll up"},
		{Key: "↓/j", Desc: "scroll down"},
		{Key: "t/esc", Desc: "back to answer"},
		{Key: "q", Desc: "quit"},
	},
}
