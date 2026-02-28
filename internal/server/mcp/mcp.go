// Package mcp implements the Model Context Protocol server.
// Supports both stdio and SSE transports.
package mcp

import (
	"context"
	"encoding/json"

	"github.com/atheory/context-engine/internal/config"
	"github.com/atheory/context-engine/internal/runner"
	"github.com/atheory/context-engine/internal/server/mcp/protocol"
	"github.com/atheory/context-engine/internal/server/mcp/tools"
)

// MCPProtocolVersion is the MCP spec version this server implements.
const MCPProtocolVersion = "2024-11-05"

// serverVersion is returned in initialize responses.
const serverVersion = "0.1.0-dev"

// toolHandler is the function signature for MCP tool implementations.
type toolHandler = tools.HandlerFunc

// Server handles MCP protocol over both stdio and SSE transports.
type Server struct {
	cfg      *config.Config
	engine   *runner.Engine
	tools    []protocol.Tool
	handlers map[string]toolHandler
	httpSrv  interface{ Shutdown(context.Context) error }
}

// New creates a new MCP Server and registers all CE tools.
func New(cfg *config.Config, engine *runner.Engine) *Server {
	s := &Server{
		cfg:      cfg,
		engine:   engine,
		handlers: make(map[string]toolHandler),
	}
	s.registerTools()
	return s
}

// Engine returns the engine for use by tool registration functions.
func (s *Server) Engine() *runner.Engine { return s.engine }

// RegisterTool adds a tool to the server.
// Called by each tool's Register function during registerTools().
func (s *Server) RegisterTool(tool protocol.Tool, handler tools.HandlerFunc) {
	s.tools = append(s.tools, tool)
	s.handlers[tool.Name] = handler
}

func (s *Server) registerTools() {
	tools.RegisterQuery(s)
	tools.RegisterIndex(s)
	tools.RegisterStatus(s)
	tools.RegisterSearch(s)
}

// Shutdown gracefully stops the SSE HTTP server (if running).
func (s *Server) Shutdown(ctx context.Context) error {
	if s.httpSrv != nil {
		return s.httpSrv.Shutdown(ctx)
	}
	return nil
}

// handleRequest routes a JSON-RPC 2.0 request to the appropriate handler.
func (s *Server) handleRequest(ctx context.Context, req protocol.Request) protocol.Response {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(ctx, req)
	case "ping":
		return protocol.Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  json.RawMessage(`{}`),
		}
	default:
		return protocol.ErrorResponse(req.ID, -32601, "method not found", req.Method)
	}
}

func (s *Server) handleInitialize(req protocol.Request) protocol.Response {
	result := protocol.InitializeResult{
		ProtocolVersion: MCPProtocolVersion,
		Capabilities: protocol.ServerCapabilities{
			Tools: &protocol.ToolsCapability{ListChanged: false},
		},
		ServerInfo: protocol.ServerInfo{
			Name:    "context-engine",
			Version: serverVersion,
		},
	}
	return protocol.OKResponse(req.ID, result)
}

func (s *Server) handleToolsList(req protocol.Request) protocol.Response {
	type listResult struct {
		Tools []protocol.Tool `json:"tools"`
	}
	return protocol.OKResponse(req.ID, listResult{Tools: s.tools})
}

func (s *Server) handleToolsCall(ctx context.Context, req protocol.Request) protocol.Response {
	var params protocol.CallToolParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return protocol.ErrorResponse(req.ID, -32602, "invalid params", err.Error())
	}

	handler, ok := s.handlers[params.Name]
	if !ok {
		return protocol.ErrorResponse(req.ID, -32602, "unknown tool", params.Name)
	}

	result, err := handler(ctx, params.Arguments)
	if err != nil {
		result = protocol.CallToolResult{
			Content: []protocol.ToolContent{{Type: "text", Text: err.Error()}},
			IsError: true,
		}
	}

	return protocol.OKResponse(req.ID, result)
}
