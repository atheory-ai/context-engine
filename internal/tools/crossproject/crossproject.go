// Package crossproject implements the crossproject tool.
// It searches the org graph to find cross-project relationships for anchored symbols.
package crossproject

import (
	"context"
	"fmt"
	"strings"

	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/tools/shared"
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
	return "predicate.crossproject=true, or 2+ concept anchors (suggests cross-cutting concern)"
}

// Activate returns true when the IR has the crossproject predicate OR has 2+ concept anchors.
func (t *Tool) Activate(ir core.IR) bool {
	if ir.Predicates["crossproject"] == "true" {
		return true
	}
	conceptCount := 0
	for _, anchor := range ir.Anchors {
		if anchor.Type == "concept" {
			conceptCount++
		}
	}
	return conceptCount >= 2
}

// Execute searches the org graph for nodes sharing canonical IDs with the anchors.
func (t *Tool) Execute(ctx context.Context, req core.ToolRequest) (core.ToolResult, error) {
	var emissions []core.Emission

	for _, anchor := range req.Anchors {
		if anchor.Node == nil {
			continue
		}

		matches, err := t.sub.FindInOrgGraph(ctx, anchor.Node.CanonicalID, anchor.Node.Type)
		if err != nil {
			return core.ToolResult{}, fmt.Errorf("org graph search for %s: %w", anchor.Node.CanonicalID, err)
		}

		// Filter out matches from the same project.
		var filtered []core.OrgMatch
		for _, m := range matches {
			if m.ProjectID != req.ProjectID {
				filtered = append(filtered, m)
			}
		}
		if len(filtered) == 0 {
			continue
		}

		display, _ := shared.Truncate(filtered, 20)
		content := shared.TruncateContent(formatCrossProject(anchor.Node, display), 1500)
		emissions = append(emissions, shared.Thinking(req, "crossproject", content, map[string]any{
			"tool":        "crossproject",
			"symbol":      anchor.Node.CanonicalID,
			"org_matches": len(filtered),
		}))
	}

	return core.ToolResult{Emissions: emissions}, nil
}

func formatCrossProject(node *core.Node, matches []core.OrgMatch) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("## Cross-project: %s\n\n", node.Label))
	b.WriteString(fmt.Sprintf("Found in %d other project(s):\n\n", len(matches)))

	for _, m := range matches {
		b.WriteString(fmt.Sprintf("**%s** (`%s`)\n", m.ProjectName, m.Node.CanonicalID))
		b.WriteString(fmt.Sprintf("  type: %s | similarity: %.0f%%\n\n", m.Node.Type, m.Similarity*100))
	}

	return b.String()
}
