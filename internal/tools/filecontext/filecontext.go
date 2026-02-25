// Package filecontext implements the filecontext tool.
// It retrieves file-level nodes and their immediate symbol neighbors from
// the substrate, providing surrounding code context.
package filecontext

import (
	"context"
	"fmt"
	"strings"

	"github.com/atheory/context-engine/internal/core"
)

// Tool implements the filecontext built-in tool.
type Tool struct {
	sub core.SubstrateReader
}

// New creates a filecontext Tool backed by the given substrate reader.
func New(sub core.SubstrateReader) *Tool {
	return &Tool{sub: sub}
}

func (t *Tool) Name() string        { return "filecontext" }
func (t *Tool) Description() string { return "Retrieves file-level nodes and their substrate neighbors." }

// ActivationHint satisfies the strategizer.ToolWithHint interface.
func (t *Tool) ActivationHint() string {
	return "query involves understanding the context of a specific file or directory"
}

// Activate returns true when the IR has the filecontext predicate set.
func (t *Tool) Activate(ir core.IR) bool {
	return ir.Predicates["filecontext"] == "true"
}

// Execute retrieves file nodes and their contained symbols.
// For file/directory anchors: lists defined symbols via "contains" and "defines" edges.
// For symbol anchors: walks up to the containing file and lists siblings.
func (t *Tool) Execute(ctx context.Context, req core.ToolRequest) (core.ToolResult, error) {
	if len(req.Anchors) == 0 {
		return core.ToolResult{
			Emissions: []core.Emission{{
				RunID:     req.RunID,
				TurnID:    req.TurnID,
				LoopIndex: req.LoopIndex,
				Source:    "tool:filecontext",
				Channel:   core.ChanAction,
				Content:   "filecontext: no anchors to retrieve",
			}},
		}, nil
	}

	kLimit := req.IR.KLimit
	if kLimit <= 0 {
		kLimit = core.DefaultKLimit
	}

	type fileEntry struct {
		fileLabel string
		symbols   []string
		imports   []string
	}
	var files []fileEntry

	for _, anchor := range req.Anchors {
		if anchor.Node == nil {
			continue
		}
		if len(files) >= kLimit {
			break
		}

		var fileNode *core.Node

		// If anchor is a file node, use it directly.
		if anchor.Node.Type == core.NodeTypeFile || anchor.Node.Type == core.NodeTypeDirectory {
			fileNode = anchor.Node
		} else {
			// For symbol anchors, find the containing file via belongs_to or defines edges.
			parentEdges, err := t.sub.EdgesTo(ctx, anchor.Node.ID, core.EdgeTypeDefines)
			if err == nil && len(parentEdges) > 0 {
				fileNode, _ = t.sub.Node(ctx, parentEdges[0].SourceID)
			}
			if fileNode == nil {
				parentEdges2, err := t.sub.Edges(ctx, anchor.Node.ID, core.EdgeTypeBelongsTo)
				if err == nil && len(parentEdges2) > 0 {
					fileNode, _ = t.sub.Node(ctx, parentEdges2[0].TargetID)
				}
			}
		}

		// Get children of the file node.
		var entry fileEntry
		if fileNode != nil {
			entry.fileLabel = fileNode.Label
		} else {
			entry.fileLabel = anchor.Node.Label
			fileNode = anchor.Node
		}

		// Get symbols defined in this file.
		for _, edgeType := range []string{core.EdgeTypeContains, core.EdgeTypeDefines} {
			children, err := t.sub.Edges(ctx, fileNode.ID, edgeType)
			if err != nil {
				continue
			}
			for _, e := range children {
				child, err := t.sub.Node(ctx, e.TargetID)
				if err != nil || child == nil {
					continue
				}
				if child.Type == core.NodeTypeSymbol {
					entry.symbols = append(entry.symbols, child.Label)
				}
			}
		}

		// Get imports.
		importEdges, err := t.sub.Edges(ctx, fileNode.ID, core.EdgeTypeImports)
		if err == nil {
			for _, e := range importEdges {
				imp, err := t.sub.Node(ctx, e.TargetID)
				if err == nil && imp != nil {
					entry.imports = append(entry.imports, imp.Label)
				}
			}
		}

		files = append(files, entry)
	}

	// Format output.
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("File context for %d anchor(s):\n\n", len(req.Anchors)))
	for _, f := range files {
		sb.WriteString(fmt.Sprintf("### %s\n", f.fileLabel))
		if len(f.symbols) > 0 {
			sb.WriteString(fmt.Sprintf("Defined symbols (%d): %s\n",
				len(f.symbols), strings.Join(f.symbols, ", ")))
		}
		if len(f.imports) > 0 {
			sb.WriteString(fmt.Sprintf("Imports (%d): %s\n",
				len(f.imports), strings.Join(f.imports, ", ")))
		}
		if len(f.symbols) == 0 && len(f.imports) == 0 {
			sb.WriteString("(no symbols or imports found)\n")
		}
		sb.WriteString("\n")
	}
	if len(files) == 0 {
		sb.WriteString("(no file nodes found for anchors)\n")
	}

	return core.ToolResult{
		Emissions: []core.Emission{{
			RunID:     req.RunID,
			TurnID:    req.TurnID,
			LoopIndex: req.LoopIndex,
			Source:    "tool:filecontext",
			Channel:   core.ChanAction,
			Content:   sb.String(),
		}},
	}, nil
}
