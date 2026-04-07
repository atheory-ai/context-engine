package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/atheory/context-engine/internal/config"
	"github.com/atheory/context-engine/internal/runner"
	"github.com/atheory/context-engine/tui"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func newQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "query [text]",
		Short: "Run a query against the active project",
		Long: `Run an AI-powered investigation query against the indexed codebase.

If text is omitted, launches the interactive TUI.
If text is provided, runs in CLI mode and prints the answer.`,
		RunE: runQuery,
	}

	cmd.Flags().Bool("tui", false,
		"force TUI mode even when text is provided")
	cmd.Flags().String("role", "",
		"agent role to use (overrides project default)")
	cmd.Flags().Int("max-loops", 0,
		"maximum cognitive loop iterations (0 = use project default)")
	cmd.Flags().Int("k-limit", 0,
		"maximum nodes per activation query (0 = use project default)")
	cmd.Flags().String("model", "",
		"model tier to use: fast|standard|thinking")
	cmd.Flags().Bool("trace", false,
		"write verbatim LLM calls to execution.db (overrides config)")

	_ = viper.BindPFlag("engine.max_loops", cmd.Flags().Lookup("max-loops"))
	_ = viper.BindPFlag("engine.k_limit", cmd.Flags().Lookup("k-limit"))

	return cmd
}

func runQuery(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	forceTUI, _ := cmd.Flags().GetBool("tui")

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	engine, err := runner.New(ctx, cfg)
	if err != nil {
		return fmt.Errorf("engine init: %w", err)
	}
	defer engine.Close(context.Background())

	switch {
	case len(args) == 0:
		return tui.Run(engine, cfg)
	case forceTUI:
		return tui.RunWithQuery(engine, cfg, strings.Join(args, " "))
	default:
		return runCLIQuery(cmd, cfg, engine, strings.Join(args, " "))
	}
}

func runCLIQuery(_ *cobra.Command, cfg *config.Config, engine *runner.Engine, query string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	ch := engine.Channels()

	debug := viper.GetBool("debug")
	showCost := viper.GetBool("show_cost") || cfg.Display.ShowCost
	noColor := viper.GetBool("no_color") || cfg.Display.NoColor
	showThink := cfg.Display.ShowThinking

	renderer := newCLIRenderer(ch, debug, showCost, noColor, showThink)
	go renderer.Run(ctx)

	queryErr := engine.Query(ctx, query)
	renderer.Stop()
	renderer.Wait()

	return queryErr
}
