package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/atheory/context-engine/internal/runner"
	"github.com/atheory/context-engine/internal/server/mcp/protocol"
)

var searchTool = protocol.Tool{
	Name: "ce_search",
	Description: `Lightweight substrate search without running the full cognitive loop.
Returns matching nodes directly from the knowledge graph.
Use for: looking up specific symbols, finding files by name, checking if something is indexed.
Faster than ce_query but less intelligent — no activation propagation or tool fan-out.`,
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"query": {
				"type": "string",
				"description": "Search term. Matches against node labels and canonical IDs."
			},
			"type": {
				"type": "string",
				"description": "Filter by node type: symbol, namespace, concept, file",
				"enum": ["symbol", "namespace", "concept", "file"]
			},
			"limit": {
				"type": "integer",
				"description": "Maximum results (default: 10, max: 50)",
				"minimum": 1,
				"maximum": 50
			}
		},
		"required": ["query"]
	}`),
}

// RegisterSearch registers the ce_search MCP tool.
func RegisterSearch(s Registrar) {
	s.RegisterTool(searchTool, handleSearch(s.Engine()))
}

func handleSearch(engine *runner.Engine) HandlerFunc {
	return func(ctx context.Context, args json.RawMessage) (protocol.CallToolResult, error) {
		var params struct {
			Query string `json:"query"`
			Type  string `json:"type"`
			Limit int    `json:"limit"`
		}
		if err := json.Unmarshal(args, &params); err != nil {
			return protocol.CallToolResult{}, err
		}

		if params.Query == "" {
			return protocol.CallToolResult{}, fmt.Errorf("query is required")
		}

		limit := params.Limit
		if limit <= 0 {
			limit = 10
		}
		if limit > 50 {
			limit = 50
		}

		nodes, err := engine.SearchSubstrate(ctx, runner.SearchOptions{
			Query: params.Query,
			Type:  params.Type,
			Limit: limit,
		})
		if err != nil {
			return errorResult(fmt.Sprintf("Search failed: %v", err)), nil
		}

		if len(nodes) == 0 {
			return textResult(fmt.Sprintf("No nodes found matching %q", params.Query)), nil
		}

		var lines []string
		for _, node := range nodes {
			lines = append(lines, fmt.Sprintf("[%s] %s (%s)",
				node.Type, node.CanonicalID, node.SourceClass))
		}

		return textResult(strings.Join(lines, "\n")), nil
	}
}
