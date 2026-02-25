package watcher

import "time"

// Debouncer coalesces rapid file-change events into a single notification.
// Phase 2 implementation — currently a no-op stub.
//
// In Phase 2, Debouncer wraps a Watcher channel and suppresses events that
// arrive within the configured interval of each other, preventing the indexer
// from re-running on every keystroke during active editing.
type Debouncer struct {
	interval time.Duration
}

// NewDebouncer creates a Debouncer with the given debounce interval.
func NewDebouncer(interval time.Duration) *Debouncer {
	return &Debouncer{interval: interval}
}

// Wrap wraps an input channel with debouncing.
// Phase 2 stub — returns the input channel unchanged.
func (d *Debouncer) Wrap(in <-chan string) <-chan string { return in }
