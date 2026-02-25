// Package callgraph implements the callgraph tool.
// It follows call chains through the substrate to trace execution paths.
//
// Phase 1: stub — activates on "callgraph" predicate, emits placeholder.
// Phase 2: traverses substrate call edges from anchored symbols.
package callgraph

import (
	"context"

	"github.com/atheory/context-engine/internal/core"
)

// Tool implements the callgraph built-in tool.
type Tool struct {
	sub core.SubstrateReader
}

// New creates a callgraph Tool backed by the given substrate reader.
func New(sub core.SubstrateReader) *Tool {
	return &Tool{sub: sub}
}

func (t *Tool) Name() string        { return "callgraph" }
func (t *Tool) Description() string { return "Follows call chains through the substrate graph." }

// ActivationHint satisfies the strategizer.ToolWithHint interface.
func (t *Tool) ActivationHint() string {
	return "query is explicitly about execution flow, call chains, or data flow"
}

// Activate returns true when the IR has the callgraph predicate set.
func (t *Tool) Activate(ir core.IR) bool {
	return ir.Predicates["callgraph"] == "true"
}

// Execute traverses call chains from the anchored symbols.
// Phase 1 stub: returns a placeholder emission.
func (t *Tool) Execute(_ context.Context, req core.ToolRequest) (core.ToolResult, error) {
	return core.ToolResult{
		Emissions: []core.Emission{{
			RunID:     req.RunID,
			TurnID:    req.TurnID,
			LoopIndex: req.LoopIndex,
			Source:    "tool:callgraph",
			Channel:   core.ChanAction,
			Content:   "callgraph: stub — call chain traversal not yet implemented",
		}},
	}, nil
}
