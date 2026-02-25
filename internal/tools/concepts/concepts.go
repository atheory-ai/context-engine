// Package concepts implements the concepts tool.
// It expands domain concepts laterally through the substrate,
// finding nodes related to the anchored concept terms.
package concepts

import (
	"context"
	"fmt"
	"strings"

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

// Execute expands concept anchors through the substrate.
// For concept-type anchors: follows synonym_of and co_activates edges.
// For symbol/namespace anchors: finds concept nodes referencing them.
func (t *Tool) Execute(ctx context.Context, req core.ToolRequest) (core.ToolResult, error) {
	if len(req.Anchors) == 0 {
		return core.ToolResult{
			Emissions: []core.Emission{{
				RunID:     req.RunID,
				TurnID:    req.TurnID,
				LoopIndex: req.LoopIndex,
				Source:    "tool:concepts",
				Channel:   core.ChanAction,
				Content:   "concepts: no anchors to expand",
			}},
		}, nil
	}

	kLimit := req.IR.KLimit
	if kLimit <= 0 {
		kLimit = core.DefaultKLimit
	}

	type conceptEntry struct {
		anchorLabel   string
		relatedLabel  string
		relationType  string
	}
	var entries []conceptEntry

	for _, anchor := range req.Anchors {
		if anchor.Node == nil {
			continue
		}
		if len(entries) >= kLimit {
			break
		}

		// Follow synonym_of and co_activates edges from concept nodes.
		for _, edgeType := range []string{core.EdgeTypeSynonymOf, core.EdgeTypeCoActivates, core.EdgeTypeAnnotates} {
			edges, err := t.sub.Edges(ctx, anchor.Node.ID, edgeType)
			if err != nil {
				continue
			}
			for _, e := range edges {
				if len(entries) >= kLimit {
					break
				}
				relNode, err := t.sub.Node(ctx, e.TargetID)
				if err != nil || relNode == nil {
					continue
				}
				entries = append(entries, conceptEntry{
					anchorLabel:  anchor.Node.Label,
					relatedLabel: relNode.Label,
					relationType: edgeType,
				})
			}
		}

		// Also search the project for concept nodes related to this anchor's label.
		if anchor.Ref.Type == "concept" || anchor.Node.Type == core.NodeTypeConcept {
			relatedNodes, err := t.sub.Query(ctx, core.SubstrateQuery{
				ProjectID: req.ProjectID,
				NodeTypes: []string{core.NodeTypeConcept},
				Limit:     20,
			})
			if err == nil {
				for _, n := range relatedNodes {
					if n.ID == anchor.Node.ID {
						continue
					}
					if len(entries) >= kLimit {
						break
					}
					// Check if related via label similarity (contains anchor term).
					if strings.Contains(strings.ToLower(n.Label), strings.ToLower(anchor.Node.Label)) ||
						strings.Contains(strings.ToLower(anchor.Node.Label), strings.ToLower(n.Label)) {
						entries = append(entries, conceptEntry{
							anchorLabel:  anchor.Node.Label,
							relatedLabel: n.Label,
							relationType: "related_concept",
						})
					}
				}
			}
		}
	}

	// Format output.
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Concept expansion for %d anchor(s) — %d related concept(s) found:\n\n",
		len(req.Anchors), len(entries)))
	for _, c := range entries {
		sb.WriteString(fmt.Sprintf("  %s → %s (%s)\n", c.anchorLabel, c.relatedLabel, c.relationType))
	}
	if len(entries) == 0 {
		sb.WriteString("  (no related concepts found in substrate)\n")
	}

	return core.ToolResult{
		Emissions: []core.Emission{{
			RunID:     req.RunID,
			TurnID:    req.TurnID,
			LoopIndex: req.LoopIndex,
			Source:    "tool:concepts",
			Channel:   core.ChanAction,
			Content:   sb.String(),
		}},
	}, nil
}
