package watcher

import (
	"sync"
	"time"
)

// Debouncer accumulates file-change events and signals readiness after
// a quiet period, preventing reindexing on every individual keystroke.
type Debouncer struct {
	mu       sync.Mutex
	paths    map[string]struct{}
	timer    *time.Timer
	interval time.Duration
	ready    chan struct{}
}

// NewDebouncer creates a Debouncer with the given quiet-period interval.
func NewDebouncer(interval time.Duration) *Debouncer {
	return &Debouncer{
		paths:    make(map[string]struct{}),
		interval: interval,
		ready:    make(chan struct{}, 1),
	}
}

// Add records a changed path and resets the debounce timer.
func (d *Debouncer) Add(path string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.paths[path] = struct{}{}

	if d.timer != nil {
		d.timer.Reset(d.interval)
	} else {
		d.timer = time.AfterFunc(d.interval, func() {
			select {
			case d.ready <- struct{}{}:
			default:
			}
		})
	}
}

// Ready returns a channel that receives a value when the debounce interval
// has elapsed and accumulated paths are ready to be flushed.
func (d *Debouncer) Ready() <-chan struct{} { return d.ready }

// Flush returns all accumulated paths and resets the debouncer.
func (d *Debouncer) Flush() []string {
	d.mu.Lock()
	defer d.mu.Unlock()

	paths := make([]string, 0, len(d.paths))
	for p := range d.paths {
		paths = append(paths, p)
	}
	d.paths = make(map[string]struct{})
	d.timer = nil
	return paths
}
