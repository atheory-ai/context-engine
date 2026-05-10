package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/atheory-ai/context-engine/internal/runner"
	"github.com/atheory-ai/context-engine/internal/server/mcp/protocol"
)

var statusTool = protocol.Tool{
	Name: "ce_status",
	Description: `Get the current index status for a project.
Returns: node count, edge count, last indexed time, index completeness.
Use before querying to verify the project is indexed.`,
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"project": {
				"type": "string",
				"description": "Git remote URL. Omit for active project."
			}
		}
	}`),
}

// RegisterStatus registers the ce_status MCP tool.
func RegisterStatus(s Registrar) {
	s.RegisterTool(statusTool, handleStatus(s.Engine()))
}

func handleStatus(engine *runner.Engine) HandlerFunc {
	return func(ctx context.Context, args json.RawMessage) (protocol.CallToolResult, error) {
		status, err := engine.ProjectStatus(ctx)
		if err != nil {
			return protocol.CallToolResult{}, err
		}

		lastIndexed := "never"
		if !status.LastIndexed.IsZero() {
			lastIndexed = status.LastIndexed.Format(time.RFC3339)
		}

		text := fmt.Sprintf(`Project: %s
Status: %s
Nodes: %d
Edges: %d
Last indexed: %s`,
			status.GitURL,
			status.IndexState,
			status.NodeCount,
			status.EdgeCount,
			lastIndexed,
		)

		return textResult(text), nil
	}
}
