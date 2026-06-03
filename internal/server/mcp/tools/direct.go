package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/atheory-ai/context-engine/internal/runner"
	"github.com/atheory-ai/context-engine/internal/server/mcp/protocol"
)

var directToolInputSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"node_id": {
			"type": "string",
			"description": "Exact substrate node ID. Prefer this when chaining from ce_search output."
		},
		"canonical_id": {
			"type": "string",
			"description": "Exact node canonical ID."
		},
		"query": {
			"type": "string",
			"description": "Search term used to resolve the first matching node when node_id/canonical_id are omitted."
		},
		"type": {
			"type": "string",
			"description": "Optional node type filter when resolving by query.",
			"enum": ["symbol", "namespace", "concept", "file"]
		},
		"limit": {
			"type": "integer",
			"description": "Maximum search candidates considered when resolving by query. Default: 1.",
			"minimum": 1,
			"maximum": 50
		}
	}
}`)

func RegisterDirectTools(s Registrar) {
	registerDirectTool(s, "ce_callgraph",
		"Trace callers and callees for a symbol node from the CE graph.",
		"callgraph")
	registerDirectTool(s, "ce_references",
		"Find incoming references to a symbol or namespace node from the CE graph.",
		"references")
	registerDirectTool(s, "ce_file_context",
		"Show file-level symbols, namespaces, concepts, and imports for a CE file or symbol.",
		"filecontext")
	registerDirectTool(s, "ce_concepts",
		"Expand a concept node into definitions, related terms, and implementors.",
		"concepts")
	registerDirectTool(s, "ce_summary",
		"Summarize namespace members, dependencies, and dependents from the CE graph.",
		"summary")
	registerDirectTool(s, "ce_crossproject",
		"Find matching nodes in other indexed projects through the CE org graph.",
		"crossproject")
	RegisterComposedTools(s)
}

func registerDirectTool(s Registrar, name, description, internalName string) {
	tool := protocol.Tool{
		Name:        name,
		Description: description,
		InputSchema: directToolInputSchema,
	}
	s.RegisterTool(tool, handleDirectTool(s.Engine(), internalName))
}

func handleDirectTool(engine *runner.Engine, toolName string) HandlerFunc {
	return func(ctx context.Context, args json.RawMessage) (protocol.CallToolResult, error) {
		var params struct {
			NodeID      string `json:"node_id"`
			CanonicalID string `json:"canonical_id"`
			Query       string `json:"query"`
			Type        string `json:"type"`
			Limit       int    `json:"limit"`
		}
		if err := json.Unmarshal(args, &params); err != nil {
			return protocol.CallToolResult{}, fmt.Errorf("invalid params: %w", err)
		}

		result, err := engine.RunDirectTool(ctx, runner.DirectToolOptions{
			Tool:        toolName,
			NodeID:      params.NodeID,
			CanonicalID: params.CanonicalID,
			Query:       params.Query,
			Type:        params.Type,
			Limit:       params.Limit,
		})
		if err != nil {
			return errorResult(fmt.Sprintf("%s failed: %v", toolName, err)), nil
		}
		return textResult(result.Content), nil
	}
}
