package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/atheory/context-engine/internal/config"
	"github.com/atheory/context-engine/internal/core"
	"github.com/atheory/context-engine/internal/orggraph"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage CE configuration",
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:   "show",
			Short: "Print resolved config (all sources merged)",
			RunE:  runConfigShow,
		},
		&cobra.Command{
			Use:   "get <key>",
			Short: "Get a specific config value",
			Args:  cobra.ExactArgs(1),
			RunE:  runConfigGet,
		},
		&cobra.Command{
			Use:   "set <key> <value>",
			Short: "Set a value in the project ce.yaml",
			Args:  cobra.ExactArgs(2),
			RunE:  runConfigSet,
		},
		newOrgConceptsCmd(),
	)

	return cmd
}

func runConfigShow(_ *cobra.Command, _ []string) error {
	settings := viper.AllSettings()
	maskSensitive(settings)

	enc := yaml.NewEncoder(os.Stdout)
	enc.SetIndent(2)
	return enc.Encode(settings)
}

func runConfigGet(_ *cobra.Command, args []string) error {
	key := args[0]
	val := viper.Get(key)
	if val == nil {
		fmt.Println("<not set>")
		return nil
	}
	fmt.Println(val)
	return nil
}

func runConfigSet(_ *cobra.Command, args []string) error {
	key := args[0]
	value := args[1]
	viper.Set(key, value)
	if err := viper.WriteConfig(); err != nil {
		return fmt.Errorf("write config: %w\nRun 'ce project init' first to create a ce.yaml", err)
	}
	fmt.Printf("Set %s = %s\n", key, value)
	return nil
}

// newOrgConceptsCmd returns the `ce config org-concepts` command with
// list/add/remove subcommands for managing org-level concept seeds.
func newOrgConceptsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "org-concepts",
		Short: "Manage org-level concept seeds",
	}

	// ── list ────────────────────────────────────────────────────────────────
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List all org-level concept seeds",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.LoadRaw()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			org, err := orggraph.Open(cfg.DataDir)
			if err != nil {
				return fmt.Errorf("open org graph: %w", err)
			}
			defer org.Close()

			seeds, err := org.GetOrgConceptSeeds(context.Background())
			if err != nil {
				return fmt.Errorf("list org concepts: %w", err)
			}
			if len(seeds) == 0 {
				fmt.Println("No org-level concept seeds defined.")
				fmt.Println("Add one with: ce config org-concepts add --term <term> --definition <def>")
				return nil
			}
			fmt.Printf("%-30s %s\n", "TERM", "DEFINITION")
			fmt.Println(strings.Repeat("-", 70))
			for _, s := range seeds {
				def := s.Definition
				if len(def) > 38 {
					def = def[:35] + "..."
				}
				fmt.Printf("%-30s %s\n", s.Term, def)
			}
			return nil
		},
	})

	// ── add ─────────────────────────────────────────────────────────────────
	addCmd := &cobra.Command{
		Use:   "add",
		Short: "Add or update an org-level concept seed",
		RunE: func(cmd *cobra.Command, _ []string) error {
			term, _ := cmd.Flags().GetString("term")
			if term == "" {
				return fmt.Errorf("--term is required")
			}
			definition, _ := cmd.Flags().GetString("definition")
			relatedStr, _ := cmd.Flags().GetString("related")
			synonymsStr, _ := cmd.Flags().GetString("synonyms")

			var related, synonyms []string
			if relatedStr != "" {
				for _, r := range strings.Split(relatedStr, ",") {
					if t := strings.TrimSpace(r); t != "" {
						related = append(related, t)
					}
				}
			}
			if synonymsStr != "" {
				for _, s := range strings.Split(synonymsStr, ",") {
					if t := strings.TrimSpace(s); t != "" {
						synonyms = append(synonyms, t)
					}
				}
			}

			cfg, err := config.LoadRaw()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			org, err := orggraph.Open(cfg.DataDir)
			if err != nil {
				return fmt.Errorf("open org graph: %w", err)
			}
			defer org.Close()

			seed := core.ConceptSeed{
				Term:       term,
				Definition: definition,
				Related:    related,
				Synonyms:   synonyms,
			}
			if err := org.AddOrgConceptSeed(context.Background(), seed); err != nil {
				return fmt.Errorf("add org concept seed: %w", err)
			}
			fmt.Printf("Added org concept seed: %s\n", term)
			return nil
		},
	}
	addCmd.Flags().String("term", "", "Concept term (required)")
	addCmd.Flags().String("definition", "", "Human-readable definition")
	addCmd.Flags().String("related", "", "Comma-separated related terms")
	addCmd.Flags().String("synonyms", "", "Comma-separated synonyms")
	cmd.AddCommand(addCmd)

	// ── remove ──────────────────────────────────────────────────────────────
	removeCmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove an org-level concept seed",
		RunE: func(cmd *cobra.Command, _ []string) error {
			term, _ := cmd.Flags().GetString("term")
			if term == "" {
				return fmt.Errorf("--term is required")
			}

			cfg, err := config.LoadRaw()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			org, err := orggraph.Open(cfg.DataDir)
			if err != nil {
				return fmt.Errorf("open org graph: %w", err)
			}
			defer org.Close()

			if err := org.RemoveOrgConceptSeed(context.Background(), term); err != nil {
				return fmt.Errorf("remove org concept seed: %w", err)
			}
			fmt.Printf("Removed org concept seed: %s\n", term)
			return nil
		},
	}
	removeCmd.Flags().String("term", "", "Concept term to remove (required)")
	cmd.AddCommand(removeCmd)

	return cmd
}

func maskSensitive(m map[string]any) {
	sensitiveKeys := []string{"api_key", "token", "secret", "password"}
	for k, v := range m {
		for _, sensitive := range sensitiveKeys {
			if strings.Contains(strings.ToLower(k), sensitive) {
				if str, ok := v.(string); ok && len(str) > 4 {
					m[k] = str[:4] + "****"
				}
			}
		}
		if nested, ok := v.(map[string]any); ok {
			maskSensitive(nested)
		}
	}
}
