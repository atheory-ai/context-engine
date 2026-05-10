package runtime

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/atheory-ai/context-engine/internal/core"
)

// wasmTool implements core.Tool via WASM function calls.
// All calls are serialized through plugin.mu.
type wasmTool struct {
	plugin     *pluginInstance
	descriptor ToolDescriptor
}

func (t *wasmTool) Name() string        { return t.descriptor.Name }
func (t *wasmTool) Description() string { return t.descriptor.Description }

// Activate returns true if this tool should run given the current IR.
// Input is JSON-encoded {"tool_name": "...", "ir": {...}}.
// Pure function — no side effects.
func (t *wasmTool) Activate(ir core.IR) bool {
	t.plugin.mu.Lock()
	defer t.plugin.mu.Unlock()

	input, _ := json.Marshal(map[string]any{
		"tool_name": t.descriptor.Name,
		"ir":        ir,
	})

	_, result, err := t.plugin.wasm.Call("ce_tool_activate", input)
	if err != nil {
		return false
	}
	// Plugin returns a single byte: 1 = activate, 0 = skip.
	return len(result) > 0 && result[0] == 1
}

// Execute runs the tool and returns emissions and proposed substrate changes.
// Input is JSON-encoded {"tool_name": "...", "request": {...}}.
func (t *wasmTool) Execute(ctx context.Context, req core.ToolRequest) (core.ToolResult, error) {
	t.plugin.mu.Lock()
	defer t.plugin.mu.Unlock()

	input, _ := json.Marshal(map[string]any{
		"tool_name": t.descriptor.Name,
		"request":   req,
	})

	_, result, err := t.plugin.wasm.Call("ce_tool_execute", input)
	if err != nil {
		return core.ToolResult{}, fmt.Errorf("ce_tool_execute %s: %w", t.descriptor.Name, err)
	}

	var out core.ToolResult
	if err := json.Unmarshal(result, &out); err != nil {
		return core.ToolResult{}, fmt.Errorf("parse tool result: %w", err)
	}
	return out, nil
}
