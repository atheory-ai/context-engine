// Package filecontext implements the filecontext tool.
// It surfaces the full structural context of files containing anchor nodes.
package filecontext

import (
	"context"
	"fmt"
	"strings"

	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/tools/shared"
)

// Tool implements the filecontext built-in tool.
type Tool struct {
	sub core.SubstrateReader
}

// New creates a filecontext Tool backed by the given substrate reader.
func New(sub core.SubstrateReader) *Tool {
	return &Tool{sub: sub}
}

func (t *Tool) Name() string { return "filecontext" }
func (t *Tool) Description() string {
	return "Retrieves file-level nodes and their substrate neighbors."
}

// ActivationHint satisfies the strategizer.ToolWithHint interface.
func (t *Tool) ActivationHint() string {
	return "predicate.filecontext=true, or file-type anchors in IR"
}

// Activate returns true when the IR has the filecontext predicate OR has file-type anchors.
func (t *Tool) Activate(ir core.IR) bool {
	if ir.Predicates["filecontext"] == "true" {
		return true
	}
	for _, anchor := range ir.Anchors {
		if anchor.Type == "file" {
			return true
		}
	}
	return false
}

// Execute retrieves file nodes and their contained symbols.
func (t *Tool) Execute(ctx context.Context, req core.ToolRequest) (core.ToolResult, error) {
	var emissions []core.Emission

	// Collect unique file paths from anchors.
	fileSet := make(map[string]struct{})
	for _, anchor := range req.Anchors {
		if anchor.Node == nil {
			continue
		}
		if anchor.Node.Type == core.NodeTypeFile {
			fileSet[anchor.Node.CanonicalID] = struct{}{}
		} else {
			if fp := extractFilePath(anchor.Node); fp != "" {
				fileSet[fp] = struct{}{}
			}
		}
	}

	for filePath := range fileSet {
		fileNode, err := t.sub.GetFileNode(ctx, req.ProjectID, filePath)
		if err != nil || fileNode == nil {
			continue
		}

		fileNodes, err := t.sub.GetNodesForFile(ctx, req.ProjectID, filePath)
		if err != nil {
			return core.ToolResult{}, fmt.Errorf("get nodes for file %s: %w", filePath, err)
		}

		imports, err := t.sub.GetFileImports(ctx, req.ProjectID, fileNode.ID)
		if err != nil {
			return core.ToolResult{}, fmt.Errorf("get imports for %s: %w", filePath, err)
		}

		content := shared.TruncateContent(formatFileContext(filePath, fileNodes, imports), 2000)
		emissions = append(emissions, shared.Thinking(req, "filecontext", content, map[string]any{
			"tool":       "filecontext",
			"file":       filePath,
			"node_count": len(fileNodes),
		}))
	}

	return core.ToolResult{Emissions: emissions}, nil
}

// extractFilePath infers the file path from a node's properties or canonical ID.
func extractFilePath(node *core.Node) string {
	if node.Type == core.NodeTypeFile {
		return node.CanonicalID
	}
	if fp, ok := node.Properties["file_path"].(string); ok {
		return fp
	}
	return ""
}

func formatFileContext(filePath string, nodes []core.Node, imports []core.Node) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("## File: %s\n\n", filePath))

	byType := make(map[string][]core.Node)
	for _, n := range nodes {
		if n.Type != core.NodeTypeFile {
			byType[n.Type] = append(byType[n.Type], n)
		}
	}

	for _, t := range []string{core.NodeTypeSymbol, core.NodeTypeNamespace, core.NodeTypeConcept} {
		if nodeGroup, ok := byType[t]; ok {
			b.WriteString(fmt.Sprintf("**%ss** (%d):\n", t, len(nodeGroup)))
			for _, n := range nodeGroup {
				b.WriteString(fmt.Sprintf("  - `%s`\n", n.Label))
			}
			b.WriteString("\n")
		}
	}

	if len(imports) > 0 {
		b.WriteString(fmt.Sprintf("**Imports** (%d):\n", len(imports)))
		for _, imp := range imports {
			b.WriteString(fmt.Sprintf("  - `%s`\n", imp.CanonicalID))
		}
	}

	return b.String()
}
