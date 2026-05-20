// Package concepts implements the concepts tool.
// It expands concept anchors into related nodes and surfaces domain vocabulary.
package concepts

import (
	"context"
	"fmt"
	"strings"

	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/tools/shared"
)

// Tool implements the concepts built-in tool.
type Tool struct {
	sub core.SubstrateReader
}

// New creates a concepts Tool backed by the given substrate reader.
func New(sub core.SubstrateReader) *Tool {
	return &Tool{sub: sub}
}

func (t *Tool) Name() string { return "concepts" }
func (t *Tool) Description() string {
	return "Expands domain concept vocabulary through the substrate."
}

// ActivationHint satisfies the strategizer.ToolWithHint interface.
func (t *Tool) ActivationHint() string {
	return "predicate.concepts=true, or any concept-type anchors in IR"
}

// Activate returns true when the IR has the concepts predicate OR has concept-type anchors.
func (t *Tool) Activate(ir core.IR) bool {
	if ir.Predicates["concepts"] == "true" {
		return true
	}
	for _, anchor := range ir.Anchors {
		if anchor.Type == "concept" {
			return true
		}
	}
	return false
}

// Execute expands concept anchors through the substrate.
func (t *Tool) Execute(ctx context.Context, req core.ToolRequest) (core.ToolResult, error) {
	var emissions []core.Emission

	for _, anchor := range req.Anchors {
		if anchor.Node == nil || anchor.Node.Type != core.NodeTypeConcept {
			continue
		}

		implementors, err := t.sub.GetConceptImplementors(ctx, req.ProjectID, anchor.Node.ID)
		if err != nil {
			return core.ToolResult{}, fmt.Errorf("get concept implementors: %w", err)
		}

		seed, err := t.sub.GetConceptSeed(ctx, req.ProjectID, anchor.Node.CanonicalID)
		if err != nil {
			return core.ToolResult{}, fmt.Errorf("get concept seed: %w", err)
		}

		content := shared.TruncateContent(formatConcepts(anchor.Node, seed, implementors), 1500)
		emissions = append(emissions, shared.Thinking(req, "concepts", content, map[string]any{
			"tool":         "concepts",
			"concept":      anchor.Node.CanonicalID,
			"implementors": len(implementors),
		}))
	}

	return core.ToolResult{Emissions: emissions}, nil
}

func formatConcepts(concept *core.Node, seed *core.ConceptSeed, implementors []core.NodeWithActivation) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("## Concept: %s\n\n", concept.Label))

	if seed != nil {
		if seed.Definition != "" {
			b.WriteString(fmt.Sprintf("**Definition:** %s\n\n", seed.Definition))
		}
		if len(seed.Related) > 0 {
			b.WriteString(fmt.Sprintf("**Related:** %s\n\n", strings.Join(seed.Related, ", ")))
		}
		if len(seed.Synonyms) > 0 {
			b.WriteString(fmt.Sprintf("**Synonyms:** %s\n\n", strings.Join(seed.Synonyms, ", ")))
		}
	}

	if len(implementors) > 0 {
		display, note := shared.Truncate(implementors, 15)
		b.WriteString(fmt.Sprintf("**Implemented by** (%d nodes):\n", len(implementors)))
		if note != "" {
			b.WriteString("  " + note)
		}
		for _, impl := range display {
			b.WriteString(fmt.Sprintf("  - `%s` (%s)\n", impl.CanonicalID, impl.Type))
		}
	}

	return b.String()
}
