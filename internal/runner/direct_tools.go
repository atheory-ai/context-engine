package runner

import (
	"context"
	"fmt"
	"strings"

	"github.com/atheory-ai/context-engine/internal/core"
)

type DirectToolOptions struct {
	Tool        string
	NodeID      string
	CanonicalID string
	Query       string
	Type        string
	Limit       int
}

type DirectToolResult struct {
	Content string
}

func (e *Engine) RunDirectTool(ctx context.Context, opts DirectToolOptions) (*DirectToolResult, error) {
	if opts.Tool == "" {
		return nil, fmt.Errorf("tool is required")
	}

	anchor, err := e.resolveDirectToolAnchor(ctx, opts)
	if err != nil {
		return nil, err
	}

	tools := buildToolList(e.substrate, e.plugins)
	var selected core.Tool
	for _, tool := range tools {
		if tool.Name() == opts.Tool {
			selected = tool
			break
		}
	}
	if selected == nil {
		return nil, fmt.Errorf("unknown tool %q", opts.Tool)
	}

	ir := core.IR{
		Mode:       core.IRModeThinking,
		Anchors:    []core.AnchorRef{anchor.Ref},
		Predicates: map[string]string{opts.Tool: "true"},
		OpenQueries: []string{
			fmt.Sprintf("Run %s for %s", opts.Tool, anchor.Node.CanonicalID),
		},
	}

	result, err := selected.Execute(ctx, core.ToolRequest{
		RunID:     core.RunID("mcp"),
		TurnID:    core.TurnID("mcp"),
		LoopIndex: 0,
		ProjectID: core.ProjectID("local"),
		IR:        ir,
		Anchors:   []core.Anchor{anchor},
		Substrate: e.substrate,
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", opts.Tool, err)
	}

	var parts []string
	for _, emission := range result.Emissions {
		if emission.Content != "" {
			parts = append(parts, emission.Content)
		}
	}
	if len(parts) == 0 {
		return &DirectToolResult{Content: fmt.Sprintf("%s found no context for %s", opts.Tool, anchor.Node.CanonicalID)}, nil
	}
	return &DirectToolResult{Content: strings.Join(parts, "\n\n")}, nil
}

func (e *Engine) resolveDirectToolAnchor(ctx context.Context, opts DirectToolOptions) (core.Anchor, error) {
	node, err := e.resolveDirectToolNode(ctx, opts)
	if err != nil {
		return core.Anchor{}, err
	}
	if node == nil {
		return core.Anchor{}, fmt.Errorf("no matching node found")
	}

	out, err := e.substrate.GetEdgesFrom(ctx, core.ProjectID("local"), node.ID)
	if err != nil {
		return core.Anchor{}, fmt.Errorf("get outgoing edges for %s: %w", node.ID, err)
	}
	in, err := e.substrate.GetEdgesTo(ctx, core.ProjectID("local"), node.ID)
	if err != nil {
		return core.Anchor{}, fmt.Errorf("get incoming edges for %s: %w", node.ID, err)
	}
	edges := append(out, in...)

	return core.Anchor{
		Ref: core.AnchorRef{
			Type:       node.Type,
			ID:         node.CanonicalID,
			Confidence: "high",
		},
		Node:       node,
		Edges:      edges,
		Activation: 1,
	}, nil
}

func (e *Engine) resolveDirectToolNode(ctx context.Context, opts DirectToolOptions) (*core.Node, error) {
	projectID := core.ProjectID("local")

	if opts.NodeID != "" {
		node, err := e.substrate.GetNode(ctx, projectID, core.NodeID(opts.NodeID))
		if err != nil {
			return nil, fmt.Errorf("resolve node_id %q: %w", opts.NodeID, err)
		}
		if node != nil {
			return node, nil
		}
	}

	if opts.CanonicalID != "" {
		node, err := e.substrate.GetNodeByCanonicalID(ctx, projectID, opts.CanonicalID)
		if err != nil {
			return nil, fmt.Errorf("resolve canonical_id %q: %w", opts.CanonicalID, err)
		}
		if node != nil {
			return node, nil
		}
	}

	query := opts.Query
	if query == "" {
		return nil, fmt.Errorf("one of node_id, canonical_id, or query is required")
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 1
	}
	nodes, err := e.SearchSubstrate(ctx, SearchOptions{Query: query, Type: opts.Type, Limit: limit})
	if err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("no nodes found matching %q", query)
	}
	return e.substrate.GetNode(ctx, projectID, core.NodeID(nodes[0].ID))
}
