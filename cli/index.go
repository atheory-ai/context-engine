package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/atheory-ai/context-engine/internal/config"
	"github.com/atheory-ai/context-engine/internal/runner"
	"github.com/spf13/cobra"
)

func newIndexCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "index [path]",
		Short: "Index a project (or reindex if already indexed)",
		Long: `Walk the project directory, extract AST nodes and edges via
language plugins, and populate the substrate graph.

If path is omitted, uses the current directory.
Runs an incremental reindex if the project was previously indexed.
Use --full to force a complete reindex.`,
		RunE: runIndex,
	}

	cmd.Flags().Bool("full", false,
		"force full reindex (ignore previous index state)")
	cmd.Flags().Bool("watch", false,
		"keep running and reindex on file changes")
	cmd.Flags().StringSlice("include", nil,
		"glob patterns to include (overrides ce.yaml)")
	cmd.Flags().StringSlice("exclude", nil,
		"glob patterns to exclude (overrides ce.yaml)")

	return cmd
}

func runIndex(cmd *cobra.Command, args []string) error {
	// Resolve root directory.
	rootDir := "."
	if len(args) > 0 {
		rootDir = args[0]
	}
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}
	if _, err := os.Stat(absRoot); os.IsNotExist(err) {
		return fmt.Errorf("path does not exist: %s", absRoot)
	}

	full, _ := cmd.Flags().GetBool("full")
	include, _ := cmd.Flags().GetStringSlice("include")
	exclude, _ := cmd.Flags().GetStringSlice("exclude")

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// CLI flags override ce.yaml include/exclude.
	if len(include) > 0 {
		cfg.Indexer.Include = include
	}
	if len(exclude) > 0 {
		cfg.Indexer.Exclude = exclude
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	engine, err := runner.New(ctx, cfg)
	if err != nil {
		return fmt.Errorf("engine init: %w", err)
	}
	defer engine.Close(context.Background())

	ch := engine.Channels()
	renderer := newCLIRenderer(ch, false, false, false, false)
	go renderer.Run(ctx)

	stats, indexErr := engine.Index(ctx, absRoot, full)

	renderer.Stop()
	renderer.Wait()

	if indexErr != nil && ctx.Err() == nil {
		return indexErr
	}

	// Print summary.
	fmt.Printf("\nIndex complete: %d files indexed, %d nodes, %d edges (%s)\n",
		stats.FilesIndexed, stats.NodesWritten, stats.EdgesWritten,
		stats.Duration.Round(1000000), // round to milliseconds
	)
	if stats.FilesSkipped > 0 {
		fmt.Printf("  %d files skipped (no matching language handler)\n", stats.FilesSkipped)
	}
	if stats.FilesErrored > 0 {
		fmt.Printf("  %d files had extraction errors (check warnings above)\n", stats.FilesErrored)
	}
	return nil
}
