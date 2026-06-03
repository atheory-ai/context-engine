package indexer

import (
	"crypto/sha256"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// defaultPluginFiles embeds the defaults/ directory.
// In production, this directory contains compiled .wasm plugin files built
// from the plugin SDK repo as part of the release pipeline.
// In development builds, the directory contains only a placeholder file;
// ExtractDefaults silently skips files that are not present.
//
//go:embed defaults
var defaultPluginFiles embed.FS

// ExtractDefaults extracts embedded default plugins to the data directory
// if they are not already present or if the embedded version is newer.
// Called at engine startup before plugin loading.
// Silently skips files that are not yet embedded (development builds).
func ExtractDefaults(dataDir string) error {
	defaultsDir := filepath.Join(dataDir, "plugins", "defaults")
	if err := os.MkdirAll(defaultsDir, 0755); err != nil {
		return fmt.Errorf("create defaults dir: %w", err)
	}

	files := []string{
		"go-language.wasm",
		"go-grammar.wasm",
		"typescript.wasm",
		"typescript-grammar.wasm",
		"python.wasm",
		"python-grammar.wasm",
		"php.wasm",
		"wordpress-conventions.wasm",
		"woocommerce-conventions.wasm",
	}

	for _, name := range files {
		data, err := defaultPluginFiles.ReadFile("defaults/" + name)
		if err != nil {
			if isNotExist(err) {
				continue // not yet built — skip silently
			}
			return fmt.Errorf("read embedded %s: %w", name, err)
		}
		if len(data) == 0 {
			continue // empty stub — skip
		}

		destPath := filepath.Join(defaultsDir, name)
		if shouldWrite(destPath, data) {
			if err := os.WriteFile(destPath, data, 0644); err != nil {
				return fmt.Errorf("write %s: %w", destPath, err)
			}
		}
	}

	return nil
}

func shouldWrite(path string, newContent []byte) bool {
	existing, err := os.ReadFile(path)
	if err != nil {
		return true // absent — write it
	}
	existingHash := sha256.Sum256(existing)
	newHash := sha256.Sum256(newContent)
	return existingHash != newHash
}

func isNotExist(err error) bool {
	return errors.Is(err, fs.ErrNotExist)
}
