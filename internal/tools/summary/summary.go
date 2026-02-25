// Package summary implements the summary tool.
// It produces a substrate summary for the anchored namespaces,
// listing their contained symbols and relationships.
//
// Phase 1: stub — activates on "summary" predicate, emits placeholder.
// Phase 2: queries namespace nodes and their contained symbols.
package summary

import (
	"context"

	"github.com/atheory/context-engine/internal/core"
)

// Tool implements the summary built-in tool.
type Tool struct {
	sub core.SubstrateReader
}

// New creates a summary Tool backed by the given substrate reader.
func New(sub core.SubstrateReader) *Tool {
	return &Tool{sub: sub}
}

func (t *Tool) Name() string        { return "summary" }
func (t *Tool) Description() string { return "Produces a substrate summary for anchored namespaces." }

// ActivationHint satisfies the strategizer.ToolWithHint interface.
func (t *Tool) ActivationHint() string {
	return "query asks for an overview of a package, namespace, or subsystem"
}

// Activate returns true when the IR has the summary predicate set.
func (t *Tool) Activate(ir core.IR) bool {
	return ir.Predicates["summary"] == "true"
}

// Execute produces a substrate summary for the anchored namespaces.
// Phase 1 stub: returns a placeholder emission.
func (t *Tool) Execute(_ context.Context, req core.ToolRequest) (core.ToolResult, error) {
	return core.ToolResult{
		Emissions: []core.Emission{{
			RunID:     req.RunID,
			TurnID:    req.TurnID,
			LoopIndex: req.LoopIndex,
			Source:    "tool:summary",
			Channel:   core.ChanAction,
			Content:   "summary: stub — namespace summary not yet implemented",
		}},
	}, nil
}
