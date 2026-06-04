package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/atheory-ai/context-engine/internal/runner"
	"github.com/atheory-ai/context-engine/internal/server/mcp/protocol"
)

var sourceRangeTool = protocol.Tool{
	Name:        "ce_source_ranges",
	Description: "Return precise source snippets for CE-cited nodes, canonical IDs, or a search query.",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"node_id": {"type": "string", "description": "Exact substrate node ID."},
			"canonical_id": {"type": "string", "description": "Exact node canonical ID."},
			"query": {"type": "string", "description": "Search query used when node_id/canonical_id are omitted."},
			"type": {"type": "string", "enum": ["symbol", "namespace", "concept", "file"]},
			"limit": {"type": "integer", "minimum": 1, "maximum": 12},
			"context": {"type": "integer", "description": "Context lines around each range. Default: 3.", "minimum": 0, "maximum": 20}
		}
	}`),
}

var investigateTool = protocol.Tool{
	Name: "ce_investigate",
	Description: `Compose CE search, file context, references, callgraph, tests, entrypoints, and source ranges into one deterministic investigation packet.
Use this before broad filesystem search when a task requires understanding an unfamiliar code path.`,
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"node_id": {"type": "string", "description": "Exact substrate node ID."},
			"canonical_id": {"type": "string", "description": "Exact node canonical ID."},
			"query": {"type": "string", "description": "Investigation query."},
			"type": {"type": "string", "enum": ["symbol", "namespace", "concept", "file"]},
			"limit": {"type": "integer", "minimum": 1, "maximum": 12},
			"depth": {"type": "integer", "minimum": 1, "maximum": 4},
			"include_tests": {"type": "boolean", "description": "Include related tests. Default: true."},
			"include_hooks": {"type": "boolean", "description": "Include entrypoints/hooks/framework signals. Default: true."},
			"include_sources": {"type": "boolean", "description": "Include source snippets for top anchors. Default: true."}
		}
	}`),
}

var relatedTestsTool = protocol.Tool{
	Name:        "ce_related_tests",
	Description: "Find tests, specs, fixtures, and test helpers related to a CE query or anchor.",
	InputSchema: relatedContextSchema,
}

var entrypointsTool = protocol.Tool{
	Name:        "ce_entrypoints",
	Description: "Find routes, hooks, handlers, bootstrap registrations, and framework entrypoint signals related to a query or anchor.",
	InputSchema: relatedContextSchema,
}

var lifecycleTool = protocol.Tool{
	Name:        "ce_lifecycle",
	Description: "Find lifecycle-oriented context such as init/load/auth/session/validate/persist/save/merge paths related to a query or anchor.",
	InputSchema: relatedContextSchema,
}

var relatedContextSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"node_id": {"type": "string", "description": "Exact substrate node ID."},
		"canonical_id": {"type": "string", "description": "Exact node canonical ID."},
		"query": {"type": "string", "description": "Search query used when node_id/canonical_id are omitted."},
		"type": {"type": "string", "enum": ["symbol", "namespace", "concept", "file"]},
		"limit": {"type": "integer", "minimum": 1, "maximum": 30}
	}
}`)

func RegisterComposedTools(s Registrar) {
	s.RegisterTool(sourceRangeTool, handleSourceRanges(s.Engine()))
	s.RegisterTool(investigateTool, handleInvestigate(s.Engine()))
	s.RegisterTool(relatedTestsTool, handleRelatedContext(s.Engine(), "tests"))
	s.RegisterTool(entrypointsTool, handleRelatedContext(s.Engine(), "entrypoints"))
	s.RegisterTool(lifecycleTool, handleRelatedContext(s.Engine(), "lifecycle"))
}

func handleSourceRanges(engine *runner.Engine) HandlerFunc {
	return func(ctx context.Context, args json.RawMessage) (protocol.CallToolResult, error) {
		var params runner.SourceRangeOptions
		if err := json.Unmarshal(args, &params); err != nil {
			return protocol.CallToolResult{}, fmt.Errorf("invalid params: %w", err)
		}
		result, err := engine.SourceRanges(ctx, params)
		if err != nil {
			return errorResult(fmt.Sprintf("source ranges failed: %v", err)), nil
		}
		return textResult(result.Content), nil
	}
}

func handleInvestigate(engine *runner.Engine) HandlerFunc {
	return func(ctx context.Context, args json.RawMessage) (protocol.CallToolResult, error) {
		var params struct {
			NodeID         string `json:"node_id"`
			CanonicalID    string `json:"canonical_id"`
			Query          string `json:"query"`
			Type           string `json:"type"`
			Limit          int    `json:"limit"`
			Depth          int    `json:"depth"`
			IncludeTests   *bool  `json:"include_tests"`
			IncludeHooks   *bool  `json:"include_hooks"`
			IncludeSources *bool  `json:"include_sources"`
		}
		if err := json.Unmarshal(args, &params); err != nil {
			return protocol.CallToolResult{}, fmt.Errorf("invalid params: %w", err)
		}
		opts := runner.InvestigateOptions{
			NodeID: params.NodeID, CanonicalID: params.CanonicalID, Query: params.Query,
			Type: params.Type, Limit: params.Limit, Depth: params.Depth,
			IncludeTests: true, IncludeHooks: true, IncludeSources: true,
		}
		if params.IncludeTests != nil {
			opts.IncludeTests = *params.IncludeTests
		}
		if params.IncludeHooks != nil {
			opts.IncludeHooks = *params.IncludeHooks
		}
		if params.IncludeSources != nil {
			opts.IncludeSources = *params.IncludeSources
		}
		result, err := engine.Investigate(ctx, opts)
		if err != nil {
			return errorResult(fmt.Sprintf("investigate failed: %v", err)), nil
		}
		return textResult(result.Content), nil
	}
}

func handleRelatedContext(engine *runner.Engine, kind string) HandlerFunc {
	return func(ctx context.Context, args json.RawMessage) (protocol.CallToolResult, error) {
		var params runner.RelatedContextOptions
		if err := json.Unmarshal(args, &params); err != nil {
			return protocol.CallToolResult{}, fmt.Errorf("invalid params: %w", err)
		}
		var result *runner.DirectToolResult
		var err error
		switch kind {
		case "tests":
			result, err = engine.RelatedTests(ctx, params)
		case "entrypoints":
			result, err = engine.Entrypoints(ctx, params)
		case "lifecycle":
			result, err = engine.Lifecycle(ctx, params)
		}
		if err != nil {
			return errorResult(fmt.Sprintf("%s failed: %v", kind, err)), nil
		}
		return textResult(result.Content), nil
	}
}
