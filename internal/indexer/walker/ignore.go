package walker

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/gobwas/glob"
)

// hardSkipDirs are directory names that are always skipped, regardless of patterns.
var hardSkipDirs = map[string]bool{
	".git":         true,
	".hg":          true,
	".svn":         true,
	"vendor":       true,
	"node_modules": true,
	".cache":       true,
	"__pycache__":  true,
	".DS_Store":    true,
}

// IgnoreMatcher combines ce.yaml exclude patterns with .gitignore rules.
// Paths are always relative to the project root, with forward slashes.
type IgnoreMatcher struct {
	patterns []glob.Glob
}

// newIgnoreMatcher builds an IgnoreMatcher from config patterns and .gitignore.
func newIgnoreMatcher(root string, configPatterns []string) (*IgnoreMatcher, error) {
	var patterns []glob.Glob

	// ce.yaml exclude patterns.
	for _, p := range configPatterns {
		g, err := glob.Compile(p, '/')
		if err != nil {
			// Non-fatal — skip invalid pattern.
			continue
		}
		patterns = append(patterns, g)
	}

	// Read .gitignore if present.
	gitignorePath := filepath.Join(root, ".gitignore")
	if data, err := os.ReadFile(gitignorePath); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			// Negation patterns (!pattern) are not supported — skip.
			if strings.HasPrefix(line, "!") {
				continue
			}
			pattern := gitignoreToGlob(line)
			if g, err := glob.Compile(pattern, '/'); err == nil {
				patterns = append(patterns, g)
			}
		}
	}

	return &IgnoreMatcher{patterns: patterns}, nil
}

// MatchFile returns true if the relative file path should be excluded.
func (m *IgnoreMatcher) MatchFile(relPath string) bool {
	// Always skip hard-skip base names.
	base := filepath.Base(relPath)
	if hardSkipDirs[base] {
		return true
	}

	relSlash := filepath.ToSlash(relPath)
	for _, p := range m.patterns {
		if p.Match(relSlash) {
			return true
		}
		// Also try matching just the base name.
		if p.Match(base) {
			return true
		}
	}
	return false
}

// MatchDir returns true if the relative directory path should be skipped (pruned).
func (m *IgnoreMatcher) MatchDir(relPath string) bool {
	// Always skip hard-skip directory names.
	base := filepath.Base(relPath)
	if hardSkipDirs[base] {
		return true
	}
	// Check against patterns with and without trailing slash.
	return m.MatchFile(relPath) || m.MatchFile(relPath+"/")
}

// gitignoreToGlob converts a .gitignore pattern to a gobwas/glob pattern.
// This is a best-effort conversion for common patterns.
func gitignoreToGlob(pattern string) string {
	// Strip leading slash (root-relative patterns become prefix patterns).
	pattern = strings.TrimPrefix(pattern, "/")
	// Strip trailing slash (directory patterns — treat as prefix).
	pattern = strings.TrimSuffix(pattern, "/")

	// If the pattern contains no slash, it matches any path component.
	// E.g., "*.log" should match "foo/bar.log" → convert to "**/*.log".
	if !strings.Contains(pattern, "/") && !strings.HasPrefix(pattern, "**/") {
		return "**/" + pattern
	}

	return pattern
}
