package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/atheory-ai/context-engine/internal/config"
	"github.com/atheory-ai/context-engine/internal/indexer"
	"github.com/atheory-ai/context-engine/internal/indexer/watcher"
	"github.com/atheory-ai/context-engine/internal/runner"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
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
	cmd.Flags().String("profile-dir", "",
		"write index CPU/heap/mutex/block profiles and a phase summary to this directory")
	cmd.Flags().Bool("profile-trace", false,
		"also write a detailed Go execution trace (large; intended for short runs)")
	cmd.Flags().Int("parse-workers", 0,
		"parse worker count (0 uses configured/default count)")
	cmd.Flags().Int("extract-workers", 0,
		"extract worker count (0 uses configured/default count)")
	_ = viper.BindPFlag("indexer.profile_dir", cmd.Flags().Lookup("profile-dir"))
	_ = viper.BindPFlag("indexer.profile_trace", cmd.Flags().Lookup("profile-trace"))
	_ = viper.BindPFlag("indexer.parse_workers", cmd.Flags().Lookup("parse-workers"))
	_ = viper.BindPFlag("indexer.extract_workers", cmd.Flags().Lookup("extract-workers"))

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
	watch, _ := cmd.Flags().GetBool("watch")
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

	if indexErr != nil && ctx.Err() == nil {
		renderer.Stop()
		renderer.Wait()
		return indexErr
	}

	printIndexSummary("Index complete", stats)
	if !watch || ctx.Err() != nil {
		renderer.Stop()
		renderer.Wait()
		return nil
	}

	w, err := watcher.New(absRoot, cfg.Indexer.WatchDebounceMS, func(paths []string) {
		fmt.Printf("\nDetected %d changed path(s); reindexing...\n", len(paths))
		changedStats, err := engine.IndexPaths(ctx, absRoot, paths)
		if err != nil {
			if ctx.Err() == nil {
				fmt.Fprintf(os.Stderr, "targeted reindex failed: %v\n", err)
			}
			return
		}
		printIndexSummary("Reindex complete", changedStats)
	})
	if err != nil {
		renderer.Stop()
		renderer.Wait()
		return fmt.Errorf("watch project: %w", err)
	}
	defer w.Close()

	fmt.Printf("\nWatching %s for changes (debounce %dms; Ctrl-C to stop)\n", absRoot, cfg.Indexer.WatchDebounceMS)
	w.Run(ctx)
	renderer.Stop()
	renderer.Wait()
	return nil
}

func printIndexSummary(prefix string, stats indexer.Stats) {
	fmt.Printf("\n%s: %d files indexed, %d nodes, %d edges (%s)\n", prefix,
		stats.FilesIndexed, stats.NodesWritten, stats.EdgesWritten,
		stats.Duration.Round(1000000), // round to milliseconds
	)
	if stats.SourceBytesProcessed > 0 {
		fmt.Printf("  processed: %s source, %s serialized CST, %s estimated plugin input\n",
			formatByteCount(stats.SourceBytesProcessed),
			formatByteCount(stats.CSTBytesProcessed),
			formatByteCount(stats.PluginPayloadBytesEstimated),
		)
	}
	if stats.FilesSkipped > 0 {
		fmt.Printf("  %d files skipped (no matching language handler)\n", stats.FilesSkipped)
	}
	if stats.FilesErrored > 0 {
		fmt.Printf("  %d files had extraction errors (check warnings above)\n", stats.FilesErrored)
	}
}

func formatByteCount(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit && exp < 5; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
