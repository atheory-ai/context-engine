package runtime

import (
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

	_, result, err := h.plugin.wasm.Call("ce_language_match", []byte(filePath))
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
// treeJSON is the serialized SyntaxTree (JSON bytes), or nil if no grammar available.
// Input to ce_language_extract: {"file_path":"...","content":"...","tree":{...}|null}
func (h *wasmLanguageHandler) Extract(filePath string, content []byte, treeJSON []byte) (core.ExtractionResult, error) {
	h.plugin.mu.Lock()
	defer h.plugin.mu.Unlock()

	var treeRaw json.RawMessage
	if treeJSON != nil {
		treeRaw = json.RawMessage(treeJSON)
	}

	input, _ := json.Marshal(map[string]any{
		"file_path": filePath,
		"content":   string(content),
		"tree":      treeRaw,
	})

	_, result, err := h.plugin.wasm.Call("ce_language_extract", input)
	if err != nil {
		return core.ExtractionResult{}, fmt.Errorf("ce_language_extract: %w", err)
	}

	var out core.ExtractionResult
	if err := json.Unmarshal(result, &out); err != nil {
		return core.ExtractionResult{}, fmt.Errorf("parse extraction result: %w", err)
	}
	return out, nil
}

// Concepts returns the concept seeds this language plugin contributes.
// Calls ce_language_concepts with no input.
func (h *wasmLanguageHandler) Concepts() []core.ConceptSeed {
	h.plugin.mu.Lock()
	defer h.plugin.mu.Unlock()

	_, result, err := h.plugin.wasm.Call("ce_language_concepts", nil)
	if err != nil {
		return nil
	}
	var seeds []core.ConceptSeed
	_ = json.Unmarshal(result, &seeds)
	return seeds
}
