// Package render provides markdown and syntax-highlight rendering for the TUI.
package render

import "github.com/charmbracelet/glamour"

// Markdown renders content as markdown for display in the answer viewport.
// Creates a per-call renderer with the given word-wrap width.
// Falls back to plain text if glamour fails.
func Markdown(content string, width int) string {
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
