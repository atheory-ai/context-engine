// Package filecontext implements the filecontext tool.
// It retrieves file-level nodes and their immediate neighbors from
// the substrate, providing surrounding code context.
//
// Phase 1: stub — activates on "filecontext" predicate, emits placeholder.
// Phase 2: queries file nodes and their symbol/namespace neighbors.
package filecontext

import (
	"context"

	"github.com/atheory/context-engine/internal/core"
)

// Tool implements the filecontext built-in tool.
type Tool struct {
	sub core.SubstrateReader
}

// New creates a filecontext Tool backed by the given substrate reader.
func New(sub core.SubstrateReader) *Tool {
	return &Tool{sub: sub}
}

func (t *Tool) Name() string        { return "filecontext" }
func (t *Tool) Description() string { return "Retrieves file-level nodes and their substrate neighbors." }

// ActivationHint satisfies the strategizer.ToolWithHint interface.
func (t *Tool) ActivationHint() string {
	return "query involves understanding the context of a specific file or directory"
}

// Activate returns true when the IR has the filecontext predicate set.
func (t *Tool) Activate(ir core.IR) bool {
	return ir.Predicates["filecontext"] == "true"
}

// Execute retrieves file nodes and their substrate neighbors.
// Phase 1 stub: returns a placeholder emission.
func (t *Tool) Execute(_ context.Context, req core.ToolRequest) (core.ToolResult, error) {
	return core.ToolResult{
		Emissions: []core.Emission{{
			RunID:     req.RunID,
			TurnID:    req.TurnID,
			LoopIndex: req.LoopIndex,
			Source:    "tool:filecontext",
			Channel:   core.ChanAction,
			Content:   "filecontext: stub — file context retrieval not yet implemented",
		}},
	}, nil
}
