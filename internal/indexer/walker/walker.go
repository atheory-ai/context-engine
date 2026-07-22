// Package walker discovers files to index.
// It walks a root directory respecting include/exclude patterns,
// .gitignore rules, maximum file size, and common directories to skip.
package walker

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Config configures a Walker.
type Config struct {
	// ExcludePatterns are glob patterns (from ce.yaml) to exclude.
	ExcludePatterns []string
	// MaxFileSizeBytes is the maximum file size to index (0 = use default 10MB).
	MaxFileSizeBytes int
}

// WalkResult describes a file discovered by the walker.
type WalkResult struct {
	Path    string      // absolute path
	RelPath string      // path relative to project root
	Info    fs.FileInfo // file metadata
	// Deleted is true when a requested path no longer exists. It is only
	// produced by WalkPaths, allowing a targeted reindex to remove the prior
	// derived contribution without walking the rest of the project.
	Deleted bool
}

// WalkPaths sends a deduplicated, explicitly requested set of files to
// results. Unlike Walk it does not traverse the project tree. Missing paths
// are emitted with Deleted set so the indexer can remove their prior output.
// Paths outside root and ignored/non-regular/oversize files are omitted.
func (w *Walker) WalkPaths(ctx context.Context, paths []string, results chan<- WalkResult) error {
	defer close(results)
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if !filepath.IsAbs(path) {
			path = filepath.Join(w.root, path)
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			continue
		}
		relPath, err := filepath.Rel(w.root, abs)
		if err != nil || relPath == ".." || strings.HasPrefix(relPath, ".."+string(filepath.Separator)) {
			continue
		}
		if _, ok := seen[relPath]; ok {
			continue
		}
		seen[relPath] = struct{}{}

		info, err := os.Stat(abs)
		if os.IsNotExist(err) {
			results <- WalkResult{Path: abs, RelPath: relPath, Deleted: true}
			continue
		}
		if err != nil || !info.Mode().IsRegular() || info.Size() > w.maxSize || w.ignore.MatchFile(relPath) {
			continue
		}
		results <- WalkResult{Path: abs, RelPath: relPath, Info: info}
	}
	return nil
}

// Walker walks a directory tree respecting ignore patterns.
type Walker struct {
	root    string
	ignore  *IgnoreMatcher
	maxSize int64
}

// New creates a Walker for the given root directory.
func New(root string, cfg Config) (*Walker, error) {
	maxSize := int64(cfg.MaxFileSizeBytes)
	if maxSize <= 0 {
		maxSize = 10 * 1024 * 1024 // 10 MB default
	}

	ignore, err := newIgnoreMatcher(root, cfg.ExcludePatterns)
	if err != nil {
		return nil, err
	}

	return &Walker{
		root:    root,
		ignore:  ignore,
		maxSize: maxSize,
	}, nil
}

// Walk sends all non-ignored files to the results channel.
// The results channel is closed when Walk returns (whether by completion or error).
// Walk should be called in a goroutine; the caller reads from results concurrently.
func (w *Walker) Walk(ctx context.Context, results chan<- WalkResult) error {
	defer close(results)

	return filepath.WalkDir(w.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		relPath, _ := filepath.Rel(w.root, path)

		// Skip directories — pruning or continuing.
		if d.IsDir() {
			if w.ignore.MatchDir(relPath) {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip non-regular files.
		if !d.Type().IsRegular() {
			return nil
		}

		// Skip ignored files.
		if w.ignore.MatchFile(relPath) {
			return nil
		}

		// Skip files over the size limit.
		info, err := d.Info()
		if err != nil || info.Size() > w.maxSize {
			return nil
		}

		results <- WalkResult{
			Path:    path,
			RelPath: relPath,
			Info:    info,
		}

		return nil
	})
}

// StatDir returns the size reported by os.Stat for a path. Used by tests.
func StatDir(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}
