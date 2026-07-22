package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/atheory-ai/context-engine/internal/core"
)

// wasmLanguageHandler implements core.LanguageHandler via WASM function calls.
// All calls are serialized through plugin.mu.
type wasmLanguageHandler struct {
	plugin *pluginInstance
}

// Extensions returns the file extensions this handler processes.
// Reads from the plugin manifest's language.extensions field.
func (h *wasmLanguageHandler) Extensions() []string {
	if h.plugin.manifest.Language != nil {
		return h.plugin.manifest.Language.Extensions
	}
	return nil
}

// GrammarPath returns the resolved absolute path to the grammar WASM file.
// Returns empty string if no grammar is declared.
func (h *wasmLanguageHandler) GrammarPath() string {
	if h.plugin.manifest.Language == nil || h.plugin.manifest.Language.Grammar == "" {
		return ""
	}
	// Grammar path is relative to the plugin .wasm file location.
	if filepath.IsAbs(h.plugin.manifest.Language.Grammar) {
		return h.plugin.manifest.Language.Grammar
	}
	return filepath.Join(h.plugin.wasmDir, h.plugin.manifest.Language.Grammar)
}

// Match returns true if this handler should process the given file path.
// Default: checks the file extension against Extensions().
// If the plugin exports ce_language_match, delegates to it for custom logic.
func (h *wasmLanguageHandler) Match(filePath string) bool {
	if h.plugin.manifest.Language != nil && len(h.plugin.manifest.Language.Extensions) > 0 {
		ext := strings.ToLower(filepath.Ext(filePath))
		for _, e := range h.plugin.manifest.Language.Extensions {
			if strings.ToLower(e) == ext {
				return true
			}
		}
		// If extensions are declared but none match, only call WASM if custom match is set.
		if !h.HasCustomMatch() {
			return false
		}
	}

	// Call WASM match function for custom logic.
	h.plugin.mu.Lock()
	defer h.plugin.mu.Unlock()

	result, err := h.plugin.call("ce_language_match", []byte(filePath))
	if err != nil {
		return false
	}
	return len(result) > 0 && result[0] == 1
}

// HasCustomMatch returns true if the plugin exports ce_language_match for
// logic beyond simple extension matching.
func (h *wasmLanguageHandler) HasCustomMatch() bool {
	return h.plugin.hasExport("ce_language_match")
}

// Extract parses a file and returns the nodes and edges to add to the graph.
// treeData is the compact binary CST produced by wasmparse, or nil if no
// grammar is available. ABI-v4 sends source bytes once, followed by the CST
// node table, avoiding JSON.parse and the duplicated JS object graph.
func (h *wasmLanguageHandler) Extract(filePath string, content []byte, treeData []byte) (core.ExtractionResult, error) {
	input := compactExtractionInput(filePath, content, treeData)

	var out core.ExtractionResult
	err := h.plugin.indexPool.withInstance(context.Background(), func(instance *pluginInstance) error {
		instance.mu.Lock()
		defer instance.mu.Unlock()
		result, err := instance.call("ce_language_extract", input)
		if err != nil {
			return fmt.Errorf("ce_language_extract: %w", err)
		}
		if err := json.Unmarshal(result, &out); err != nil {
			return fmt.Errorf("parse extraction result: %w", err)
		}
		return nil
	})
	if err != nil {
		return core.ExtractionResult{}, err
	}
	return out, nil
}

// Concepts returns the concept seeds this language plugin contributes.
// Calls ce_language_concepts with no input.
func (h *wasmLanguageHandler) Concepts() []core.ConceptSeed {
	h.plugin.mu.Lock()
	defer h.plugin.mu.Unlock()

	result, err := h.plugin.call("ce_language_concepts", nil)
	if err != nil {
		return nil
	}
	var seeds []core.ConceptSeed
	_ = json.Unmarshal(result, &seeds) //nolint:errcheck // bad WASM output yields empty seeds; plugin author error, not a runtime fault
	return seeds
}
