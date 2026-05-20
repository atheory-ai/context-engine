// Package references implements the references tool.
// It finds all nodes that reference the anchored symbols by traversing incoming edges.
package references

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/tools/shared"
)

// Tool implements the references built-in tool.
type Tool struct {
	sub core.SubstrateReader
}

// New creates a references Tool backed by the given substrate reader.
func New(sub core.SubstrateReader) *Tool {
	return &Tool{sub: sub}
}

func (t *Tool) Name() string { return "references" }
func (t *Tool) Description() string {
	return "Finds all references to anchored symbols in the substrate."
}

// ActivationHint satisfies the strategizer.ToolWithHint interface.
func (t *Tool) ActivationHint() string {
	return "predicate.references=true, or thinking mode with symbol/namespace anchors"
}

// Activate returns true when the IR has the references predicate OR is in thinking
// mode with symbol or namespace anchors.
func (t *Tool) Activate(ir core.IR) bool {
	if ir.Predicates["references"] == "true" {
		return true
	}
	if ir.Mode == core.IRModeThinking {
		for _, anchor := range ir.Anchors {
			if anchor.Type == "symbol" || anchor.Type == "namespace" {
				return true
			}
		}
	}
	return false
}

// Execute finds all nodes that reference the anchored symbols.
func (t *Tool) Execute(ctx context.Context, req core.ToolRequest) (core.ToolResult, error) {
	var emissions []core.Emission

	for _, anchor := range req.Anchors {
		if anchor.Node == nil {
			continue
		}
		if anchor.Node.Type != core.NodeTypeSymbol && anchor.Node.Type != core.NodeTypeNamespace {
			continue
		}

		refs, err := t.sub.GetReferences(ctx, req.ProjectID, anchor.Node.ID)
		if err != nil {
			return core.ToolResult{}, fmt.Errorf("get references for %s: %w", anchor.Node.ID, err)
		}
		if len(refs) == 0 {
			continue
		}

		grouped := groupReferencesByType(refs)
		content := shared.TruncateContent(formatReferences(anchor.Node, grouped), 2000)
		emissions = append(emissions, shared.Thinking(req, "references", content, map[string]any{
			"tool":      "references",
			"symbol":    anchor.Node.CanonicalID,
			"ref_count": len(refs),
		}))
	}

	return core.ToolResult{Emissions: emissions}, nil
}

// referenceGroup groups references by edge type.
type referenceGroup struct {
	EdgeType   string
	References []core.NodeWithActivation
}

func groupReferencesByType(refs []core.ReferenceResult) []referenceGroup {
	groups := make(map[string][]core.NodeWithActivation)
	for _, ref := range refs {
		groups[ref.EdgeType] = append(groups[ref.EdgeType], ref.Node)
	}

	// Standard ordering for common edge types.
	order := []string{core.EdgeTypeImplements, core.EdgeTypeExtends, core.EdgeTypeImports,
		core.EdgeTypeReferences, core.EdgeTypeCalls}
	var result []referenceGroup
	for _, t := range order {
		if nodes, ok := groups[t]; ok {
			result = append(result, referenceGroup{EdgeType: t, References: nodes})
		}
	}
	// Append any remaining edge types not in the standard order.
	for t, nodes := range groups {
		if !slices.Contains(order, t) {
			result = append(result, referenceGroup{EdgeType: t, References: nodes})
		}
	}
	return result
}

func formatReferences(symbol *core.Node, groups []referenceGroup) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("## References to: %s\n\n", symbol.Label))

	for _, g := range groups {
		display, note := shared.Truncate(g.References, 10)
		b.WriteString(fmt.Sprintf("**%s** (%d):\n", g.EdgeType, len(g.References)))
		if note != "" {
			b.WriteString("  " + note)
		}
		for _, ref := range display {
			b.WriteString(fmt.Sprintf("  - `%s`\n", ref.CanonicalID))
		}
		b.WriteString("\n")
	}

	return b.String()
}
