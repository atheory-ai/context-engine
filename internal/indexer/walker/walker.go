// Package walker discovers files to index.
// It walks a root directory respecting include/exclude glob patterns,
// maximum file size, and common directories to skip (.git, vendor, etc.).
package walker

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/atheory/context-engine/internal/config"
)

// File is a file discovered by the walker.
type File struct {
	Path    string // absolute path
	RelPath string // path relative to root
	Size    int64
}

// Walk walks rootDir and returns all files that pass the filters.
// Returns early if ctx is cancelled.
func Walk(ctx context.Context, rootDir string, cfg config.IndexerConfig) ([]File, error) {
	maxSize := int64(cfg.MaxFileSizeBytes)
	if maxSize <= 0 {
		maxSize = 10 * 1024 * 1024 // 10 MB default
	}

	var files []File
	err := filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		relPath, _ := filepath.Rel(rootDir, path)

		// Skip hidden and common generated/vendor directories.
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == ".hg" || name == ".svn" ||
				name == "vendor" || name == "node_modules" ||
				name == ".cache" || name == "__pycache__" {
				return filepath.SkipDir
			}
			if shouldExclude(relPath, cfg.Exclude) {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip non-regular files (symlinks, devices, etc.).
		if !d.Type().IsRegular() {
			return nil
		}

		// Apply exclude patterns.
		if shouldExclude(relPath, cfg.Exclude) {
			return nil
		}

		// Apply include filter — if non-empty, file must match at least one pattern.
		if len(cfg.Include) > 0 && !matchesAny(relPath, cfg.Include) {
			return nil
		}

		// Skip test files if configured.
		if !cfg.IncludeTestFiles && isTestFile(relPath) {
			return nil
		}

		// Check file size.
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if info.Size() > maxSize {
			return nil
		}

		files = append(files, File{
			Path:    path,
			RelPath: relPath,
			Size:    info.Size(),
		})
		return nil
	})

	if err != nil && ctx.Err() != nil {
		return files, ctx.Err()
	}
	return files, err
}

// shouldExclude reports whether relPath matches any exclude pattern.
func shouldExclude(relPath string, patterns []string) bool {
	for _, pattern := range patterns {
		if matchPattern(pattern, relPath) {
			return true
		}
	}
	return false
}

// matchesAny reports whether relPath matches at least one of the given patterns.
func matchesAny(relPath string, patterns []string) bool {
	for _, pattern := range patterns {
		if matchPattern(pattern, relPath) {
			return true
		}
	}
	return false
}

// matchPattern reports whether relPath matches a glob pattern.
// Supports filepath.Match patterns and simple ** path globs.
func matchPattern(pattern, relPath string) bool {
	// Normalize to forward slashes.
	relSlash := filepath.ToSlash(relPath)
	patSlash := filepath.ToSlash(pattern)

	// Direct match against full relative path.
	if m, _ := filepath.Match(patSlash, relSlash); m {
		return true
	}

	// Match against base name only (e.g., "*.go" matches "src/main.go").
	base := filepath.Base(relPath)
	if m, _ := filepath.Match(patSlash, base); m {
		return true
	}

	if strings.Contains(patSlash, "**") {
		// Pattern like "vendor/**" — matches anything under vendor/.
		if strings.HasSuffix(patSlash, "/**") {
			prefix := strings.TrimSuffix(patSlash, "/**")
			if relSlash == prefix || strings.HasPrefix(relSlash, prefix+"/") {
				return true
			}
		}
		// Pattern like "**/*.go" — matches any file with the given suffix/name.
		if strings.HasPrefix(patSlash, "**/") {
			suffix := strings.TrimPrefix(patSlash, "**/")
			if m, _ := filepath.Match(suffix, base); m {
				return true
			}
		}
	}

	// Check if any single path segment matches the pattern (e.g., pattern "vendor"
	// applied to "a/vendor/b.go" for directory exclusion).
	parts := strings.Split(relSlash, "/")
	for _, part := range parts {
		if m, _ := filepath.Match(patSlash, part); m {
			return true
		}
	}

	return false
}

// isTestFile reports whether relPath looks like a test file.
// Covers Go (*_test.go), JS/TS (*.test.*, *.spec.*), and test directories.
func isTestFile(relPath string) bool {
	base := filepath.Base(relPath)
	if strings.HasSuffix(base, "_test.go") {
		return true
	}
	if strings.Contains(base, ".test.") || strings.Contains(base, ".spec.") {
		return true
	}
	// Common test directory names.
	for _, part := range strings.Split(filepath.ToSlash(relPath), "/") {
		if part == "test" || part == "tests" || part == "__tests__" || part == "spec" {
			return true
		}
	}
	return false
}

// StatDir returns the size of a directory entry — reads the FileInfo if needed.
// Used by tests.
func StatDir(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}
