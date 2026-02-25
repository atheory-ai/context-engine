// Package crossproject implements the crossproject tool.
// It queries the org graph to find cross-project relationships for
// the anchored symbols by matching on canonical IDs.
package crossproject

import (
	"context"
	"fmt"
	"strings"

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

// Execute queries the org graph for nodes sharing canonical IDs with the anchors,
// then follows cross-project edges from those nodes.
func (t *Tool) Execute(ctx context.Context, req core.ToolRequest) (core.ToolResult, error) {
	if len(req.Anchors) == 0 {
		return core.ToolResult{
			Emissions: []core.Emission{{
				RunID:     req.RunID,
				TurnID:    req.TurnID,
				LoopIndex: req.LoopIndex,
				Source:    "tool:crossproject",
				Channel:   core.ChanAction,
				Content:   "crossproject: no anchors to search",
			}},
		}, nil
	}

	kLimit := req.IR.KLimit
	if kLimit <= 0 {
		kLimit = core.DefaultKLimit
	}

	type crossEntry struct {
		localLabel   string
		orgLabel     string
		orgProjectID string
		edgeType     string
	}
	var crosses []crossEntry

	for _, anchor := range req.Anchors {
		if anchor.Node == nil {
			continue
		}
		if len(crosses) >= kLimit {
			break
		}

		// Search org graph for nodes with the same canonical_id.
		orgNodes, err := t.sub.Query(ctx, core.SubstrateQuery{
			ProjectID: "org",
			Properties: map[string]string{
				"canonical_id": anchor.Node.CanonicalID,
			},
			Limit: 20,
		})
		if err != nil {
			continue
		}

		for _, orgNode := range orgNodes {
			if orgNode.ProjectID == req.ProjectID {
				continue // skip same project
			}
			// Get cross-project edges from org node.
			orgEdges, err := t.sub.Edges(ctx, orgNode.ID, "")
			if err == nil {
				for _, e := range orgEdges {
					crosses = append(crosses, crossEntry{
						localLabel:   anchor.Node.Label,
						orgLabel:     orgNode.Label,
						orgProjectID: string(orgNode.ProjectID),
						edgeType:     e.Type,
					})
				}
			}
			// Even without edges, surface the matching org node.
			if len(orgEdges) == 0 {
				crosses = append(crosses, crossEntry{
					localLabel:   anchor.Node.Label,
					orgLabel:     orgNode.Label,
					orgProjectID: string(orgNode.ProjectID),
					edgeType:     "shared_canonical_id",
				})
			}
			if len(crosses) >= kLimit {
				break
			}
		}
	}

	// Format output.
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Cross-project relationships for %d anchor(s) — %d match(es) found:\n\n",
		len(req.Anchors), len(crosses)))
	for _, c := range crosses {
		sb.WriteString(fmt.Sprintf("  [local] %s ↔ [%s] %s (via %s)\n",
			c.localLabel, c.orgProjectID, c.orgLabel, c.edgeType))
	}
	if len(crosses) == 0 {
		sb.WriteString("  (no cross-project relationships found in org graph)\n")
	}

	return core.ToolResult{
		Emissions: []core.Emission{{
			RunID:     req.RunID,
			TurnID:    req.TurnID,
			LoopIndex: req.LoopIndex,
			Source:    "tool:crossproject",
			Channel:   core.ChanAction,
			Content:   sb.String(),
		}},
	}, nil
}
