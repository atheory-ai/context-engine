package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/atheory-ai/context-engine/internal/server/mcp/protocol"
)

// RunStdio runs the MCP server over the stdio transport.
// This is the transport used by Claude Desktop, Cursor, and Claude Code.
// Reads newline-delimited JSON-RPC requests from stdin; writes responses to stdout.
func (s *Server) RunStdio(ctx context.Context) error {
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)

	// Increase scanner buffer to handle large tool responses.
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		var req protocol.Request
		if err := json.Unmarshal(line, &req); err != nil {
			_ = encoder.Encode(protocol.ErrorResponse(nil, -32700, "parse error", err.Error()))
			continue
		}

		// Notifications have no ID and expect no response.
		if req.ID == nil && req.Method != "" {
			s.handleNotification(ctx, req)
			continue
		}

		resp := s.handleRequest(ctx, req)
		if err := encoder.Encode(resp); err != nil {
			return fmt.Errorf("write response: %w", err)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("stdin read: %w", err)
	}

	return nil
}

// handleNotification processes MCP notifications (fire-and-forget, no response).
func (s *Server) handleNotification(_ context.Context, req protocol.Request) {
	switch req.Method {
	case "notifications/initialized":
		// Client confirmed initialization — nothing to do.
	case "notifications/cancelled":
		// Client cancelled a request — context cancellation handles this.
	}
}
