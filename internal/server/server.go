// Package server manages both the MCP server and the REST/WebSocket API server.
// Both servers share the same Engine instance.
package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/atheory-ai/context-engine/internal/config"
	"github.com/atheory-ai/context-engine/internal/runner"
	"github.com/atheory-ai/context-engine/internal/server/api"
	"github.com/atheory-ai/context-engine/internal/server/mcp"
)

// Server manages both the MCP server and API server.
type Server struct {
	cfg    *config.Config
	engine *runner.Engine
	mcpSrv *mcp.Server
	apiSrv *api.Server
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// New creates a Server. Call Start() to begin listening.
func New(cfg *config.Config, engine *runner.Engine) *Server {
	return &Server{
		cfg:    cfg,
		engine: engine,
		stopCh: make(chan struct{}),
	}
}

// Start launches the API server and (if MCPEnabled) the MCP SSE server.
// Blocks until the context is cancelled or Stop() is called.
func (s *Server) Start(ctx context.Context) error {
	addr := fmt.Sprintf("%s:%d", s.cfg.Server.Host, s.cfg.Server.Port)

	// ── API server (REST + WebSocket) ──────────────────────────────────────
	if s.cfg.Server.APIEnabled || s.cfg.Server.WSEnabled {
		s.apiSrv = api.New(s.cfg, s.engine)
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			if err := s.apiSrv.Start(ctx, addr); err != nil {
				if !errors.Is(err, http.ErrServerClosed) {
					log.Printf("API server error: %v", err)
				}
			}
		}()
	}

	// ── MCP SSE server ─────────────────────────────────────────────────────
	if s.cfg.Server.MCPEnabled {
		s.mcpSrv = mcp.New(s.cfg, s.engine)
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			if err := s.mcpSrv.StartSSE(ctx, addr); err != nil {
				if !errors.Is(err, http.ErrServerClosed) {
					log.Printf("MCP SSE server error: %v", err)
				}
			}
		}()
	}

	s.writePIDFile()

	// Block until stopped.
	select {
	case <-ctx.Done():
	case <-s.stopCh:
	}

	s.shutdown()
	s.wg.Wait()
	return nil
}

// Stop signals the server to shut down.
func (s *Server) Stop() {
	close(s.stopCh)
}

func (s *Server) shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if s.apiSrv != nil {
		s.apiSrv.Shutdown(ctx) //nolint:errcheck
	}
	if s.mcpSrv != nil {
		s.mcpSrv.Shutdown(ctx) //nolint:errcheck
	}
	s.removePIDFile()
}

func (s *Server) writePIDFile() {
	pidPath := filepath.Join(s.cfg.DataDir, "server.pid")
	os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0644) //nolint:errcheck
}

func (s *Server) removePIDFile() {
	os.Remove(filepath.Join(s.cfg.DataDir, "server.pid")) //nolint:errcheck
}
