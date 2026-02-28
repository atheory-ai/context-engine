package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/atheory/context-engine/internal/runner"
	"github.com/atheory/context-engine/internal/server/mcp/protocol"
)

var indexTool = protocol.Tool{
	Name: "ce_index",
	Description: `Trigger reindexing of the project codebase.
Use when: files have changed and you want CE to pick up the changes before querying.
Returns indexing statistics when complete.`,
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"project": {
				"type": "string",
				"description": "Git remote URL of the project to index. Omit for active project."
			},
			"full": {
				"type": "boolean",
				"description": "Force full reindex (ignore file hashes). Default: false (incremental)."
			}
		}
	}`),
}

// RegisterIndex registers the ce_index MCP tool.
func RegisterIndex(s Registrar) {
	s.RegisterTool(indexTool, handleIndex(s.Engine()))
}

func handleIndex(engine *runner.Engine) HandlerFunc {
	return func(ctx context.Context, args json.RawMessage) (protocol.CallToolResult, error) {
		var params struct {
			Project string `json:"project"`
			Full    bool   `json:"full"`
		}
		if err := json.Unmarshal(args, &params); err != nil {
			return protocol.CallToolResult{}, fmt.Errorf("invalid params: %w", err)
		}

		// Get active project path from engine config.
		rootDir := engine.ActiveProjectPath()
		if rootDir == "" {
			return errorResult("no active project — run 'ce project init' first"), nil
		}

		stats, err := engine.Index(ctx, rootDir, params.Full)
		if err != nil {
			return errorResult(fmt.Sprintf("Index failed: %v", err)), nil
		}

		return textResult(fmt.Sprintf(
			"Indexing complete.\nFiles: %d indexed, %d skipped\nNodes: %d written\nEdges: %d written",
			stats.FilesIndexed, stats.FilesSkipped,
			stats.NodesWritten, stats.EdgesWritten,
		)), nil
	}
}
