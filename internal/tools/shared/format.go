package shared

import "fmt"

// Truncate limits a slice to maxLen items.
// Returns the (possibly truncated) slice and a note string.
// If note is "", no truncation occurred.
func Truncate[T any](items []T, maxLen int) ([]T, string) {
	if len(items) <= maxLen {
		return items, ""
	}
	return items[:maxLen], fmt.Sprintf("_(showing %d of %d)_\n", maxLen, len(items))
}

// TruncateContent truncates a string to maxLen bytes, appending a note if cut.
func TruncateContent(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + fmt.Sprintf("\n_(content truncated at %d chars)_", maxLen)
}
