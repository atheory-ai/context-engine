package runtime

import (
	"encoding/json"
	"fmt"

	"github.com/atheory/context-engine/internal/core"
)

// wasmAnalyzer implements core.Analyzer via WASM function calls.
// All calls are serialized through plugin.mu.
type wasmAnalyzer struct {
	plugin     *pluginInstance
	descriptor AnalyzerDescriptor
}

func (a *wasmAnalyzer) Name() string        { return a.descriptor.Name }
func (a *wasmAnalyzer) Description() string { return a.descriptor.Description }

// Analyze receives nodes for a file and returns additional edges to add to the graph.
// Input is JSON-encoded []core.Node. Output is JSON-encoded []core.Edge.
func (a *wasmAnalyzer) Analyze(nodes []core.Node) ([]core.Edge, error) {
	a.plugin.mu.Lock()
	defer a.plugin.mu.Unlock()

	input, _ := json.Marshal(nodes)

	_, result, err := a.plugin.wasm.Call("ce_analyzer_run", input)
	if err != nil {
		return nil, fmt.Errorf("ce_analyzer_run %s: %w", a.descriptor.Name, err)
	}

	var edges []core.Edge
	if err := json.Unmarshal(result, &edges); err != nil {
		return nil, fmt.Errorf("parse analyzer result from %s: %w", a.descriptor.Name, err)
	}
	return edges, nil
}
