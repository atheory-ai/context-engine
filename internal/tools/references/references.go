// Package references implements the references tool.
// It finds all substrate nodes that reference the anchored symbols.
//
// Phase 1: stub — activates on "references" predicate, emits placeholder.
// Phase 2: traverses substrate reference edges from anchored symbols.
package references

import (
	"context"

	"github.com/atheory/context-engine/internal/core"
)

// Tool implements the references built-in tool.
type Tool struct {
	sub core.SubstrateReader
}

// New creates a references Tool backed by the given substrate reader.
func New(sub core.SubstrateReader) *Tool {
	return &Tool{sub: sub}
}

func (t *Tool) Name() string        { return "references" }
func (t *Tool) Description() string { return "Finds all references to anchored symbols in the substrate." }

// ActivationHint satisfies the strategizer.ToolWithHint interface.
func (t *Tool) ActivationHint() string {
	return "query asks where a symbol is used, imported, or referenced"
}

// Activate returns true when the IR has the references predicate set.
func (t *Tool) Activate(ir core.IR) bool {
	return ir.Predicates["references"] == "true"
}

// Execute finds references to the anchored symbols.
// Phase 1 stub: returns a placeholder emission.
func (t *Tool) Execute(_ context.Context, req core.ToolRequest) (core.ToolResult, error) {
	return core.ToolResult{
		Emissions: []core.Emission{{
			RunID:     req.RunID,
			TurnID:    req.TurnID,
			LoopIndex: req.LoopIndex,
			Source:    "tool:references",
			Channel:   core.ChanAction,
			Content:   "references: stub — reference traversal not yet implemented",
		}},
	}, nil
}
