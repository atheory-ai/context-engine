// Package tools implements the four CE MCP tools: ce_query, ce_index, ce_status, ce_search.
package tools

import (
	"context"
	"encoding/json"

	"github.com/atheory/context-engine/internal/runner"
	"github.com/atheory/context-engine/internal/server/mcp/protocol"
)

// HandlerFunc is the function signature for MCP tool implementations.
type HandlerFunc func(ctx context.Context, args json.RawMessage) (protocol.CallToolResult, error)

// Registrar is the interface through which tools register themselves with the MCP server.
// Implemented by *mcp.Server — defined here to avoid import cycles.
type Registrar interface {
	RegisterTool(tool protocol.Tool, handler HandlerFunc)
	Engine() *runner.Engine
}

// textResult is a convenience helper for a successful single-text result.
func textResult(text string) protocol.CallToolResult {
	return protocol.CallToolResult{
		Content: []protocol.ToolContent{{Type: "text", Text: text}},
	}
}

// errorResult returns an IsError result with the given message.
func errorResult(msg string) protocol.CallToolResult {
	return protocol.CallToolResult{
		Content: []protocol.ToolContent{{Type: "text", Text: msg}},
		IsError: true,
	}
}
