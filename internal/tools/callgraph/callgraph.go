// Package callgraph implements the callgraph tool.
// It follows call chains through the substrate to trace execution paths.
// Uses BFS from anchor nodes, traversing outbound "calls" edges.
package callgraph

import (
	"context"
	"fmt"
	"strings"

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

// Execute traverses call chains from the anchored symbols via BFS.
func (t *Tool) Execute(ctx context.Context, req core.ToolRequest) (core.ToolResult, error) {
	if len(req.Anchors) == 0 {
		return core.ToolResult{
			Emissions: []core.Emission{{
				RunID:     req.RunID,
				TurnID:    req.TurnID,
				LoopIndex: req.LoopIndex,
				Source:    "tool:callgraph",
				Channel:   core.ChanAction,
				Content:   "callgraph: no anchors to traverse",
			}},
		}, nil
	}

	kLimit := req.IR.KLimit
	if kLimit <= 0 {
		kLimit = core.DefaultKLimit
	}

	// BFS over outbound "calls" edges from each anchor.
	visited := make(map[core.NodeID]bool)
	var queue []core.NodeID
	for _, a := range req.Anchors {
		if a.Node != nil {
			queue = append(queue, a.Node.ID)
		}
	}

	type callEntry struct {
		callerLabel string
		calleeLabel string
		weight      float64
	}
	var calls []callEntry
	nodeLabels := make(map[core.NodeID]string)

	// Seed labels from anchors.
	for _, a := range req.Anchors {
		if a.Node != nil {
			nodeLabels[a.Node.ID] = a.Node.Label
		}
	}

	for len(queue) > 0 && len(calls) < kLimit {
		nodeID := queue[0]
		queue = queue[1:]

		if visited[nodeID] {
			continue
		}
		visited[nodeID] = true

		edges, err := t.sub.Edges(ctx, nodeID, core.EdgeTypeCalls)
		if err != nil {
			continue
		}

		callerLabel := nodeLabels[nodeID]
		if callerLabel == "" {
			callerLabel = string(nodeID)[:8] + "..."
		}

		for _, e := range edges {
			// Resolve target label.
			targetLabel, ok := nodeLabels[e.TargetID]
			if !ok {
				targetNode, err := t.sub.Node(ctx, e.TargetID)
				if err == nil && targetNode != nil {
					targetLabel = targetNode.Label
					nodeLabels[e.TargetID] = targetLabel
					if !visited[e.TargetID] {
						queue = append(queue, e.TargetID)
					}
				} else {
					targetLabel = string(e.TargetID)[:8] + "..."
				}
			}

			calls = append(calls, callEntry{
				callerLabel: callerLabel,
				calleeLabel: targetLabel,
				weight:      e.Weight,
			})

			if len(calls) >= kLimit {
				break
			}
		}
	}

	// Format output.
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Call graph from %d anchor(s) — %d call relationship(s) found:\n\n",
		len(req.Anchors), len(calls)))
	for _, c := range calls {
		sb.WriteString(fmt.Sprintf("  %s → %s", c.callerLabel, c.calleeLabel))
		if c.weight != 1.0 {
			sb.WriteString(fmt.Sprintf(" (weight: %.2f)", c.weight))
		}
		sb.WriteString("\n")
	}
	if len(calls) == 0 {
		sb.WriteString("  (no outbound call edges found for anchored nodes)\n")
	}

	return core.ToolResult{
		Emissions: []core.Emission{{
			RunID:     req.RunID,
			TurnID:    req.TurnID,
			LoopIndex: req.LoopIndex,
			Source:    "tool:callgraph",
			Channel:   core.ChanAction,
			Content:   sb.String(),
		}},
	}, nil
}
