package runtime

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/atheory-ai/context-engine/internal/core"
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
	input, _ := json.Marshal(nodes)
	var edges []core.Edge
	err := a.plugin.indexPool.withInstance(context.Background(), func(instance *pluginInstance) error {
		instance.mu.Lock()
		defer instance.mu.Unlock()
		result, err := instance.call("ce_analyzer_run", input)
		if err != nil {
			return fmt.Errorf("ce_analyzer_run %s: %w", a.descriptor.Name, err)
		}
		if err := json.Unmarshal(result, &edges); err != nil {
			return fmt.Errorf("parse analyzer result from %s: %w", a.descriptor.Name, err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return edges, nil
}
