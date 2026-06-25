// Package watcher provides filesystem watching for incremental indexing.
// It uses fsnotify to detect file changes and feeds them through a Debouncer
// before triggering a targeted reindex.
package watcher

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watcher watches a directory tree for file changes.
type Watcher struct {
	root     string
	fsw      *fsnotify.Watcher
	debounce *Debouncer
	onChange func(paths []string) // called with changed absolute file paths
}

// New creates a Watcher for root. onChange is called with batches of changed
// file paths after the debounce interval has elapsed.
// debounceMS is the quiet-period duration in milliseconds (0 → 300ms default).
func New(root string, debounceMS int, onChange func(paths []string)) (*Watcher, error) {
	if debounceMS <= 0 {
		debounceMS = 300
	}
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	w := &Watcher{
		root:     root,
		fsw:      fsw,
		debounce: NewDebouncer(time.Duration(debounceMS) * time.Millisecond),
		onChange: onChange,
	}

	if err := w.addRecursive(root); err != nil {
		_ = fsw.Close()
		return nil, err
	}

	return w, nil
}

// Run starts the watch loop. Blocks until ctx is cancelled.
func (w *Watcher) Run(ctx context.Context) {
	for {
		select {
		case event, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Rename) {
				w.debounce.Add(event.Name)
			}
			// If a new directory appeared, watch it recursively.
			if event.Has(fsnotify.Create) {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					_ = w.addRecursive(event.Name) //nolint:errcheck // watcher self-heals on next event; logging would be too chatty
				}
			}

		case <-w.debounce.Ready():
			paths := w.debounce.Flush()
			if len(paths) > 0 {
				w.onChange(paths)
			}

		case <-ctx.Done():
			_ = w.fsw.Close()
			return
		}
	}
}

// Close stops the watcher immediately.
func (w *Watcher) Close() error {
	return w.fsw.Close()
}

// addRecursive adds dir and all its subdirectories to the fsnotify watcher.
func (w *Watcher) addRecursive(dir string) error {
	return filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable
		}
		if d.IsDir() {
			return w.fsw.Add(path)
		}
		return nil
	})
}
