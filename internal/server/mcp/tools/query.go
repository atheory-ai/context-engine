package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/atheory-ai/context-engine/internal/runner"
	"github.com/atheory-ai/context-engine/internal/server/mcp/protocol"
)

var queryTool = protocol.Tool{
	Name: "ce_query",
	Description: `Run an AI-powered investigation query against the indexed codebase.
Returns a structured answer with specific code references.
Use for: understanding how code works, tracing call chains, finding where concepts are implemented.
Experimental in CE v1 and hidden unless features.ce_query is enabled. Requires configured LLM credentials for the selected provider. Without an LLM key, use deterministic tools such as ce_search, ce_file_context, ce_references, ce_callgraph, ce_summary, and ce_concepts.`,
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"query": {
				"type": "string",
				"description": "The investigation question. Be specific about what you want to understand."
			},
			"project": {
				"type": "string",
				"description": "Git remote URL of the project to query. Omit to use the active project."
			},
			"max_loops": {
				"type": "integer",
				"description": "Maximum cognitive loop iterations (1-10). Default: project setting.",
				"minimum": 1,
				"maximum": 10
			}
		},
		"required": ["query"]
	}`),
}

// RegisterQuery registers the ce_query MCP tool.
func RegisterQuery(s Registrar) {
	s.RegisterTool(queryTool, handleQuery(s.Engine()))
}

func handleQuery(engine *runner.Engine) HandlerFunc {
	return func(ctx context.Context, args json.RawMessage) (protocol.CallToolResult, error) {
		var params struct {
			Query    string `json:"query"`
			Project  string `json:"project"`
			MaxLoops int    `json:"max_loops"`
		}
		if err := json.Unmarshal(args, &params); err != nil {
			return protocol.CallToolResult{}, fmt.Errorf("invalid params: %w", err)
		}

		if params.Query == "" {
			return protocol.CallToolResult{}, fmt.Errorf("query is required")
		}

		result, err := engine.QuerySync(ctx, runner.QueryOptions{
			Query:    params.Query,
			MaxLoops: params.MaxLoops,
		})
		if err != nil {
			return errorResult(fmt.Sprintf("Query failed: %v", err)), nil
		}

		return textResult(result.Answer), nil
	}
}
