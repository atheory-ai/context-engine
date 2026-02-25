// Package watcher provides filesystem watching for incremental indexing.
// Phase 2 implementation — currently a no-op stub.
//
// In Phase 2, Watcher will use fsnotify to detect file changes and feed
// them to the incremental indexer, enabling live re-indexing on save.
package watcher

import "context"

// Watcher watches a directory tree for file changes.
// Phase 2 stub — the returned channel is never written to.
type Watcher struct{}

// New creates a Watcher. Phase 2 stub.
func New() *Watcher { return &Watcher{} }

// Watch starts watching rootDir and returns a channel of changed absolute paths.
// Phase 2 stub — the returned channel is never written to and ctx is ignored.
func (w *Watcher) Watch(_ context.Context, _ string) (<-chan string, error) {
	return make(chan string), nil
}

// Close stops the watcher. Phase 2 stub.
func (w *Watcher) Close() error { return nil }
