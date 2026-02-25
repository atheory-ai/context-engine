// Package crossproject implements the crossproject tool.
// It traverses the org graph to find cross-project relationships
// for the anchored symbols.
//
// Phase 1: stub — activates on "crossproject" predicate, emits placeholder.
// Phase 2: queries the org graph database for cross-project edges.
package crossproject

import (
	"context"

	"github.com/atheory/context-engine/internal/core"
)

// Tool implements the crossproject built-in tool.
type Tool struct {
	sub core.SubstrateReader
}

// New creates a crossproject Tool backed by the given substrate reader.
func New(sub core.SubstrateReader) *Tool {
	return &Tool{sub: sub}
}

func (t *Tool) Name() string        { return "crossproject" }
func (t *Tool) Description() string { return "Finds cross-project relationships via the org graph." }

// ActivationHint satisfies the strategizer.ToolWithHint interface.
func (t *Tool) ActivationHint() string {
	return "query explicitly asks about relationships between multiple projects or repos"
}

// Activate returns true when the IR has the crossproject predicate set.
func (t *Tool) Activate(ir core.IR) bool {
	return ir.Predicates["crossproject"] == "true"
}

// Execute traverses the org graph for cross-project relationships.
// Phase 1 stub: returns a placeholder emission.
func (t *Tool) Execute(_ context.Context, req core.ToolRequest) (core.ToolResult, error) {
	return core.ToolResult{
		Emissions: []core.Emission{{
			RunID:     req.RunID,
			TurnID:    req.TurnID,
			LoopIndex: req.LoopIndex,
			Source:    "tool:crossproject",
			Channel:   core.ChanAction,
			Content:   "crossproject: stub — org graph traversal not yet implemented",
		}},
	}, nil
}
