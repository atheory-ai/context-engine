// Package summary implements the summary tool.
// It produces a substrate summary for the anchored namespaces,
// listing their contained symbols and relationships.
package summary

import (
	"context"
	"fmt"
	"sort"
	"strings"

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

// Execute produces a substrate summary for the project.
// Retrieves top-K activated nodes, groups by type, and lists relationships.
func (t *Tool) Execute(ctx context.Context, req core.ToolRequest) (core.ToolResult, error) {
	kLimit := req.IR.KLimit
	if kLimit <= 0 {
		kLimit = core.DefaultKLimit
	}

	var sb strings.Builder
	sb.WriteString("## Substrate Summary\n\n")

	// ── Anchor-level summary ──────────────────────────────────────────────
	if len(req.Anchors) > 0 {
		sb.WriteString(fmt.Sprintf("### Anchored Nodes (%d)\n\n", len(req.Anchors)))
		for _, a := range req.Anchors {
			if a.Node == nil {
				sb.WriteString(fmt.Sprintf("- [unresolved] %s (%s)\n", a.Ref.ID, a.Ref.Type))
				continue
			}
			sb.WriteString(fmt.Sprintf("- **%s** `%s` type=%s activation=%.3f\n",
				a.Node.Label, a.Node.CanonicalID, a.Node.Type, a.Activation))

			// List outbound edges for each anchor.
			edges, err := t.sub.Edges(ctx, a.Node.ID, "")
			if err == nil && len(edges) > 0 {
				edgeTypeCounts := make(map[string]int)
				for _, e := range edges {
					edgeTypeCounts[e.Type]++
				}
				parts := make([]string, 0, len(edgeTypeCounts))
				for et, cnt := range edgeTypeCounts {
					parts = append(parts, fmt.Sprintf("%s(%d)", et, cnt))
				}
				sort.Strings(parts)
				sb.WriteString(fmt.Sprintf("  edges: %s\n", strings.Join(parts, ", ")))
			}
		}
		sb.WriteString("\n")
	}

	// ── Top-K activated nodes ─────────────────────────────────────────────
	topK, err := t.sub.TopK(ctx, req.ProjectID, kLimit)
	if err == nil && len(topK) > 0 {
		sb.WriteString(fmt.Sprintf("### Top-%d Activated Nodes\n\n", len(topK)))

		// Group by node type.
		byType := make(map[string][]core.Anchor)
		for _, a := range topK {
			if a.Node == nil {
				continue
			}
			byType[a.Node.Type] = append(byType[a.Node.Type], a)
		}

		// Sort types for stable output.
		types := make([]string, 0, len(byType))
		for nt := range byType {
			types = append(types, nt)
		}
		sort.Strings(types)

		for _, nodeType := range types {
			nodes := byType[nodeType]
			sb.WriteString(fmt.Sprintf("**%s** (%d nodes):\n", nodeType, len(nodes)))
			for _, a := range nodes {
				sb.WriteString(fmt.Sprintf("  - %s (activation: %.3f)\n",
					a.Node.Label, a.Activation))
			}
			sb.WriteString("\n")
		}
	} else {
		// Fall back to namespace query if TopK returns nothing.
		namespaceNodes, queryErr := t.sub.Query(ctx, core.SubstrateQuery{
			ProjectID: req.ProjectID,
			NodeTypes: []string{core.NodeTypeNamespace},
			Limit:     kLimit,
		})
		if queryErr == nil && len(namespaceNodes) > 0 {
			sb.WriteString(fmt.Sprintf("### Namespaces (%d)\n\n", len(namespaceNodes)))
			for _, n := range namespaceNodes {
				sb.WriteString(fmt.Sprintf("- %s\n", n.Label))
			}
			sb.WriteString("\n")
		}
	}

	return core.ToolResult{
		Emissions: []core.Emission{{
			RunID:     req.RunID,
			TurnID:    req.TurnID,
			LoopIndex: req.LoopIndex,
			Source:    "tool:summary",
			Channel:   core.ChanAction,
			Content:   sb.String(),
		}},
	}, nil
}
