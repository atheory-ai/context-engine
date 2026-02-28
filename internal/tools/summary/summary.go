// Package summary implements the summary tool.
// It produces structural summaries of namespace/package nodes.
package summary

import (
	"context"
	"fmt"
	"strings"

	"github.com/atheory/context-engine/internal/core"
	"github.com/atheory/context-engine/internal/tools/shared"
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
	return "predicate.summary=true, or namespace-type anchors in IR"
}

// Activate returns true when the IR has the summary predicate OR has namespace anchors.
func (t *Tool) Activate(ir core.IR) bool {
	if ir.Predicates["summary"] == "true" {
		return true
	}
	for _, anchor := range ir.Anchors {
		if anchor.Type == "namespace" {
			return true
		}
	}
	return false
}

// Execute produces a structural summary for each namespace anchor.
func (t *Tool) Execute(ctx context.Context, req core.ToolRequest) (core.ToolResult, error) {
	var emissions []core.Emission

	for _, anchor := range req.Anchors {
		if anchor.Node == nil || anchor.Node.Type != core.NodeTypeNamespace {
			continue
		}

		members, err := t.sub.GetNamespaceMembers(ctx, req.ProjectID, anchor.Node.ID)
		if err != nil {
			return core.ToolResult{}, fmt.Errorf("get namespace members: %w", err)
		}

		deps, err := t.sub.GetNamespaceDependencies(ctx, req.ProjectID, anchor.Node.ID)
		if err != nil {
			return core.ToolResult{}, fmt.Errorf("get namespace deps: %w", err)
		}

		dependents, err := t.sub.GetNamespaceDependents(ctx, req.ProjectID, anchor.Node.ID)
		if err != nil {
			return core.ToolResult{}, fmt.Errorf("get namespace dependents: %w", err)
		}

		content := shared.TruncateContent(formatNamespaceSummary(anchor.Node, members, deps, dependents), 2000)
		emissions = append(emissions, shared.Thinking(req, "summary", content, map[string]any{
			"tool":       "summary",
			"namespace":  anchor.Node.CanonicalID,
			"members":    len(members),
			"deps":       len(deps),
			"dependents": len(dependents),
		}))
	}

	return core.ToolResult{Emissions: emissions}, nil
}

func formatNamespaceSummary(ns *core.Node, members, deps, dependents []core.Node) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("## Package: %s\n\n", ns.CanonicalID))

	// Categorize members.
	var exported, internal, types, interfaces []core.Node
	for _, m := range members {
		switch m.Type {
		case "symbol":
			if len(m.Label) > 0 && m.Label[0] >= 'A' && m.Label[0] <= 'Z' {
				exported = append(exported, m)
			} else {
				internal = append(internal, m)
			}
		case "type":
			types = append(types, m)
		case "interface":
			interfaces = append(interfaces, m)
		}
	}

	if len(exported) > 0 {
		b.WriteString(fmt.Sprintf("**Exported symbols** (%d):\n", len(exported)))
		for _, s := range exported {
			b.WriteString(fmt.Sprintf("  - `%s`\n", s.Label))
		}
		b.WriteString("\n")
	}

	if len(interfaces) > 0 {
		b.WriteString(fmt.Sprintf("**Interfaces** (%d):\n", len(interfaces)))
		for _, i := range interfaces {
			b.WriteString(fmt.Sprintf("  - `%s`\n", i.Label))
		}
		b.WriteString("\n")
	}

	if len(types) > 0 {
		b.WriteString(fmt.Sprintf("**Types** (%d):\n", len(types)))
		for _, t := range types {
			b.WriteString(fmt.Sprintf("  - `%s`\n", t.Label))
		}
		b.WriteString("\n")
	}

	// Cap internal symbols at 20.
	displayInternal, _ := shared.Truncate(internal, 20)
	if len(displayInternal) > 0 {
		b.WriteString(fmt.Sprintf("**Internal symbols** (%d):\n", len(internal)))
		for _, s := range displayInternal {
			b.WriteString(fmt.Sprintf("  - `%s`\n", s.Label))
		}
		b.WriteString("\n")
	}

	if len(deps) > 0 {
		b.WriteString(fmt.Sprintf("**Depends on** (%d packages):\n", len(deps)))
		for _, d := range deps {
			b.WriteString(fmt.Sprintf("  - `%s`\n", d.CanonicalID))
		}
		b.WriteString("\n")
	}

	if len(dependents) > 0 {
		b.WriteString(fmt.Sprintf("**Used by** (%d packages):\n", len(dependents)))
		for _, d := range dependents {
			b.WriteString(fmt.Sprintf("  - `%s`\n", d.CanonicalID))
		}
	}

	return b.String()
}
