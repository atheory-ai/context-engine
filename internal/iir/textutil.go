package iir

import "strings"

// normalizeWhitespace collapses runs of whitespace to single spaces and trims
// the ends, so extracted/generated text compares stably regardless of source
// formatting.
func normalizeWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
