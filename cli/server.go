package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/atheory-ai/context-engine/internal/config"
	"github.com/atheory-ai/context-engine/internal/runner"
	"github.com/atheory-ai/context-engine/internal/server"
	"github.com/atheory-ai/context-engine/internal/server/mcp"
	"github.com/spf13/cobra"
)

func newServerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Manage the CE MCP/API/WebSocket server",
	}

	startCmd := &cobra.Command{
		Use:   "start",
		Short: "Start the MCP/API/WebSocket server",
		RunE:  runServerStart,
	}
	startCmd.Flags().Int("port", 0,
		"port to listen on (overrides ce.yaml server.port)")
	startCmd.Flags().String("host", "",
		"address to bind (overrides ce.yaml server.host)")

	cmd.AddCommand(
		startCmd,
		&cobra.Command{
			Use:   "stop",
			Short: "Stop the running server",
			RunE:  runServerStop,
		},
		&cobra.Command{
			Use:   "status",
			Short: "Show server status",
			RunE:  runServerStatus,
		},
	)

	// ce mcp-stdio — hidden subcommand used by IDE MCP integrations.
	// Claude Desktop, Cursor, and Claude Code call this directly.
	mcpStdioCmd := &cobra.Command{
		Use:    "mcp-stdio",
		Short:  "Run MCP server over stdio (for IDE integration)",
		Hidden: true,
		RunE:   runMCPStdio,
	}
	rootCmd.AddCommand(mcpStdioCmd)

	return cmd
}

func runServerStart(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Apply flag overrides.
	if port, _ := cmd.Flags().GetInt("port"); port > 0 {
		cfg.Server.Port = port
	}
	if host, _ := cmd.Flags().GetString("host"); host != "" {
		cfg.Server.Host = host
	}

	ctx, cancel := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM)
	defer cancel()

	engine, err := runner.New(ctx, cfg)
	if err != nil {
		return fmt.Errorf("engine init: %w", err)
	}
	defer engine.Close(context.Background())

	srv := server.New(cfg, engine)

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	fmt.Printf("CE server running at http://%s\n", addr)
	if cfg.Server.MCPEnabled {
		fmt.Printf("MCP SSE endpoint:  http://%s/mcp/sse\n", addr)
	}
	fmt.Printf("API endpoint:      http://%s/api/v1\n", addr)
	fmt.Println()
	fmt.Println("ctrl+c to stop")

	return srv.Start(ctx)
}

func runServerStop(_ *cobra.Command, _ []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	pidPath := filepath.Join(cfg.DataDir, "server.pid")
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return fmt.Errorf("server is not running (no PID file)")
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return fmt.Errorf("invalid PID file: %w", err)
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("process not found: %w", err)
	}

	if err := proc.Signal(os.Interrupt); err != nil {
		return fmt.Errorf("send stop signal: %w", err)
	}

	fmt.Printf("Sent stop signal to CE server (PID %d)\n", pid)
	return nil
}

func runServerStatus(_ *cobra.Command, _ []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	pidPath := filepath.Join(cfg.DataDir, "server.pid")
	data, err := os.ReadFile(pidPath)
	if err != nil {
		fmt.Println("CE server: not running")
		return nil
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		fmt.Println("CE server: not running (stale PID file)")
		os.Remove(pidPath)
		return nil
	}

	proc, err := os.FindProcess(pid)
	if err != nil || proc.Signal(syscall.Signal(0)) != nil {
		fmt.Println("CE server: not running (stale PID file)")
		os.Remove(pidPath)
		return nil
	}

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	fmt.Printf("CE server: running (PID %d)\n", pid)
	fmt.Printf("  Address: http://%s\n", addr)
	if cfg.Server.MCPEnabled {
		fmt.Printf("  MCP SSE: http://%s/mcp/sse\n", addr)
	}
	return nil
}

func runMCPStdio(_ *cobra.Command, _ []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	engine, err := runner.New(ctx, cfg)
	if err != nil {
		return fmt.Errorf("engine init: %w", err)
	}
	defer engine.Close(context.Background())

	mcpServer := mcp.New(cfg, engine)
	return mcpServer.RunStdio(ctx)
}
