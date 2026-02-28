// Package callgraph implements the callgraph tool.
// It follows call chains through the substrate to trace execution paths.
package callgraph

import (
	"context"
	"fmt"
	"strings"

	"github.com/atheory/context-engine/internal/core"
	"github.com/atheory/context-engine/internal/tools/shared"
)

const callgraphDepth = 3 // traverse up to 3 hops in each direction

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
	return "predicate.callgraph=true, or anchors contain symbol nodes with confidence >= medium"
}

// Activate returns true when the IR has the callgraph predicate OR has symbol anchors
// with medium/high confidence.
func (t *Tool) Activate(ir core.IR) bool {
	if ir.Predicates["callgraph"] == "true" {
		return true
	}
	for _, anchor := range ir.Anchors {
		if anchor.Type == "symbol" && anchor.Confidence != "low" {
			return true
		}
	}
	return false
}

// Execute traverses call chains from anchored symbols and proposes any undiscovered
// call edges back to the Reviewer.
func (t *Tool) Execute(ctx context.Context, req core.ToolRequest) (core.ToolResult, error) {
	var emissions []core.Emission
	var proposedEdges []core.Edge

	for _, anchor := range req.Anchors {
		if anchor.Node == nil || anchor.Node.Type != core.NodeTypeSymbol {
			continue
		}

		callers, err := t.sub.GetCallers(ctx, req.ProjectID, anchor.Node.ID, callgraphDepth)
		if err != nil {
			return core.ToolResult{}, fmt.Errorf("get callers for %s: %w", anchor.Node.ID, err)
		}

		callees, err := t.sub.GetCallees(ctx, req.ProjectID, anchor.Node.ID, callgraphDepth)
		if err != nil {
			return core.ToolResult{}, fmt.Errorf("get callees for %s: %w", anchor.Node.ID, err)
		}

		if len(callers) == 0 && len(callees) == 0 {
			continue
		}

		content := shared.TruncateContent(formatCallgraph(anchor.Node, callers, callees), 2000)
		emissions = append(emissions, shared.Thinking(req, "callgraph", content, map[string]any{
			"tool":    "callgraph",
			"symbol":  anchor.Node.CanonicalID,
			"callers": len(callers),
			"callees": len(callees),
		}))

		// Propose speculative edges for undiscovered call relationships.
		for _, callee := range callees {
			if !edgeExists(anchor.Edges, anchor.Node.ID, callee.Node.ID, core.EdgeTypeCalls) {
				proposedEdges = append(proposedEdges, core.Edge{
					ID:          core.EdgeID(core.MakeEdgeID(string(anchor.Node.ID), core.EdgeTypeCalls, string(callee.Node.ID))),
					SourceID:    anchor.Node.ID,
					TargetID:    callee.Node.ID,
					Type:        core.EdgeTypeCalls,
					SourceClass: core.SourceSpeculative,
					Properties:  map[string]any{"discovered_by": "callgraph_tool"},
				})
			}
		}
	}

	return core.ToolResult{
		Emissions:     emissions,
		ProposedEdges: proposedEdges,
	}, nil
}

func formatCallgraph(symbol *core.Node, callers, callees []core.NodeWithActivation) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("## Call graph: %s\n\n", symbol.Label))

	display, note := shared.Truncate(callers, 25)
	if len(display) > 0 {
		b.WriteString("**Called by:**\n")
		if note != "" {
			b.WriteString("  " + note)
		}
		for _, c := range display {
			b.WriteString(fmt.Sprintf("  - `%s` (activation: %.2f)\n", c.CanonicalID, c.Activation))
		}
		b.WriteString("\n")
	}

	display, note = shared.Truncate(callees, 25)
	if len(display) > 0 {
		b.WriteString("**Calls:**\n")
		if note != "" {
			b.WriteString("  " + note)
		}
		for _, c := range display {
			b.WriteString(fmt.Sprintf("  - `%s` (activation: %.2f)\n", c.CanonicalID, c.Activation))
		}
	}

	return b.String()
}

// edgeExists checks whether an edge with the given source, target, and type
// already exists in the anchor's edge set.
func edgeExists(edges []core.EdgeWithWeight, sourceID, targetID core.NodeID, edgeType string) bool {
	for _, e := range edges {
		if e.SourceID == sourceID && e.TargetID == targetID && e.Type == edgeType {
			return true
		}
	}
	return false
}
