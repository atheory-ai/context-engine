package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newServerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Manage the CE MCP/API/WebSocket server",
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:   "start",
			Short: "Start the MCP/API/WebSocket server",
			RunE: func(_ *cobra.Command, _ []string) error {
				fmt.Println("Server not yet implemented in Phase 1.")
				return nil
			},
		},
		&cobra.Command{
			Use:   "stop",
			Short: "Stop the running server",
			RunE: func(_ *cobra.Command, _ []string) error {
				fmt.Println("Server not yet implemented in Phase 1.")
				return nil
			},
		},
		&cobra.Command{
			Use:   "status",
			Short: "Show server status",
			RunE: func(_ *cobra.Command, _ []string) error {
				fmt.Println("Server not yet implemented in Phase 1.")
				return nil
			},
		},
	)

	cmd.Flags().Int("port", 0,
		"port to listen on (overrides ce.yaml server.port)")
	cmd.Flags().String("host", "",
		"address to bind (overrides ce.yaml server.host)")

	return cmd
}
