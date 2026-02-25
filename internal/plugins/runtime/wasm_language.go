package runtime

import (
	"encoding/json"
	"fmt"

	"github.com/atheory/context-engine/internal/core"
)

// wasmLanguageHandler implements core.LanguageHandler via WASM function calls.
// All calls are serialized through plugin.mu.
type wasmLanguageHandler struct {
	plugin *pluginInstance
}

// Match returns true if this handler should process the given file path.
// Calls ce_language_match with the file path as input bytes.
func (h *wasmLanguageHandler) Match(filePath string) bool {
	h.plugin.mu.Lock()
	defer h.plugin.mu.Unlock()

	_, result, err := h.plugin.wasm.Call("ce_language_match", []byte(filePath))
	if err != nil {
		return false
	}
	// Plugin returns a single byte: 1 = match, 0 = no match.
	return len(result) > 0 && result[0] == 1
}

// Extract parses a file and returns the nodes and edges to add to the graph.
// Input is JSON-encoded {"file_path": "...", "content": "..."}.
func (h *wasmLanguageHandler) Extract(filePath string, content []byte) (core.ExtractionResult, error) {
	h.plugin.mu.Lock()
	defer h.plugin.mu.Unlock()

	input, _ := json.Marshal(map[string]any{
		"file_path": filePath,
		"content":   string(content),
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
