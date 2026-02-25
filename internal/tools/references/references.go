// Package references implements the references tool.
// It finds all substrate nodes that reference (point to) the anchored symbols
// by traversing incoming edges.
package references

import (
	"context"
	"fmt"
	"strings"

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

// Execute finds all nodes that reference the anchored symbols by traversing
// incoming edges (edges where the anchor's node is the target).
func (t *Tool) Execute(ctx context.Context, req core.ToolRequest) (core.ToolResult, error) {
	if len(req.Anchors) == 0 {
		return core.ToolResult{
			Emissions: []core.Emission{{
				RunID:     req.RunID,
				TurnID:    req.TurnID,
				LoopIndex: req.LoopIndex,
				Source:    "tool:references",
				Channel:   core.ChanAction,
				Content:   "references: no anchors to search",
			}},
		}, nil
	}

	kLimit := req.IR.KLimit
	if kLimit <= 0 {
		kLimit = core.DefaultKLimit
	}

	type refEntry struct {
		sourceLabel string
		edgeType    string
		targetLabel string
		weight      float64
	}
	var refs []refEntry

	for _, anchor := range req.Anchors {
		if anchor.Node == nil {
			continue
		}
		if len(refs) >= kLimit {
			break
		}

		// Get all incoming edges (edges where this node is the target).
		inEdges, err := t.sub.EdgesTo(ctx, anchor.Node.ID, "")
		if err != nil {
			continue
		}

		targetLabel := anchor.Node.Label

		for _, e := range inEdges {
			if len(refs) >= kLimit {
				break
			}

			// Resolve source node label.
			sourceLabel := ""
			sourceNode, err := t.sub.Node(ctx, e.SourceID)
			if err == nil && sourceNode != nil {
				sourceLabel = sourceNode.Label
			}
			if sourceLabel == "" {
				sourceLabel = string(e.SourceID)[:8] + "..."
			}

			refs = append(refs, refEntry{
				sourceLabel: sourceLabel,
				edgeType:    e.Type,
				targetLabel: targetLabel,
				weight:      e.Weight,
			})
		}
	}

	// Format output.
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("References to %d anchor(s) — %d reference(s) found:\n\n",
		len(req.Anchors), len(refs)))
	for _, r := range refs {
		sb.WriteString(fmt.Sprintf("  %s -[%s]→ %s", r.sourceLabel, r.edgeType, r.targetLabel))
		if r.weight != 1.0 {
			sb.WriteString(fmt.Sprintf(" (weight: %.2f)", r.weight))
		}
		sb.WriteString("\n")
	}
	if len(refs) == 0 {
		sb.WriteString("  (no incoming reference edges found for anchored nodes)\n")
	}

	return core.ToolResult{
		Emissions: []core.Emission{{
			RunID:     req.RunID,
			TurnID:    req.TurnID,
			LoopIndex: req.LoopIndex,
			Source:    "tool:references",
			Channel:   core.ChanAction,
			Content:   sb.String(),
		}},
	}, nil
}
