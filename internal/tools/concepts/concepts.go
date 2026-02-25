// Package concepts implements the concepts tool.
// It expands domain concepts laterally through the substrate,
// finding nodes related to the anchored concept terms.
//
// Phase 1: stub — activates on "concepts" predicate, emits placeholder.
// Phase 2: traverses concept synonym and related edges in the substrate.
package concepts

import (
	"context"

	"github.com/atheory/context-engine/internal/core"
)

// Tool implements the concepts built-in tool.
type Tool struct {
	sub core.SubstrateReader
}

// New creates a concepts Tool backed by the given substrate reader.
func New(sub core.SubstrateReader) *Tool {
	return &Tool{sub: sub}
}

func (t *Tool) Name() string        { return "concepts" }
func (t *Tool) Description() string { return "Expands domain concept vocabulary through the substrate." }

// ActivationHint satisfies the strategizer.ToolWithHint interface.
func (t *Tool) ActivationHint() string {
	return "query involves understanding a domain concept rather than a specific symbol"
}

// Activate returns true when the IR has the concepts predicate set.
func (t *Tool) Activate(ir core.IR) bool {
	return ir.Predicates["concepts"] == "true"
}

// Execute expands concept anchors laterally through the substrate.
// Phase 1 stub: returns a placeholder emission.
func (t *Tool) Execute(_ context.Context, req core.ToolRequest) (core.ToolResult, error) {
	return core.ToolResult{
		Emissions: []core.Emission{{
			RunID:     req.RunID,
			TurnID:    req.TurnID,
			LoopIndex: req.LoopIndex,
			Source:    "tool:concepts",
			Channel:   core.ChanAction,
			Content:   "concepts: stub — concept expansion not yet implemented",
		}},
	}, nil
}
