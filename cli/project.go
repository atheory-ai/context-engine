package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

func newProjectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Manage CE projects",
	}

	initCmd := &cobra.Command{
		Use:   "init [path]",
		Short: "Register and configure a project",
		RunE:  runProjectInit,
	}

	cmd.AddCommand(
		initCmd,
		&cobra.Command{
			Use:   "list",
			Short: "List all registered projects",
			RunE:  projectStub,
		},
		&cobra.Command{
			Use:   "status",
			Short: "Show active project index status",
			RunE:  projectStub,
		},
		&cobra.Command{
			Use:   "set <git-url>",
			Short: "Set the active project for this directory",
			Args:  cobra.ExactArgs(1),
			RunE:  projectStub,
		},
		&cobra.Command{
			Use:   "remove <git-url>",
			Short: "Unregister a project (keeps graph file)",
			Args:  cobra.ExactArgs(1),
			RunE:  projectStub,
		},
	)

	return cmd
}

func projectStub(_ *cobra.Command, _ []string) error {
	fmt.Println("Project management not yet fully implemented in Phase 1.")
	fmt.Println("Use 'ce project init' to initialize a new project.")
	return nil
}

func runProjectInit(_ *cobra.Command, args []string) error {
	projectPath := "."
	if len(args) > 0 {
		projectPath = args[0]
	}

	absPath, err := filepath.Abs(projectPath)
	if err != nil {
		return err
	}

	gitURL, err := resolveGitURL(absPath)
	if err != nil {
		return fmt.Errorf("not a git repository (or no remote configured): %w\n\n"+
			"Context Engine uses git remote URL to identify projects.\n\n"+
			"To fix this:\n"+
			"  1. Initialize a git repo:    git init && git remote add origin <url>\n"+
			"  2. Or navigate to an existing git repository", err)
	}

	fmt.Printf("Initializing Context Engine for: %s\n\n", absPath)
	fmt.Printf("Git remote: %s\n\n", gitURL)

	fmt.Println("Enter a brief description of this project (1-3 sentences).")
	fmt.Println("This becomes the base context injected into every query.")
	fmt.Println("Example: 'A Go microservice that handles volunteer scheduling")
	fmt.Println("          and billing for a non-profit platform.'")
	fmt.Println("(Press Enter twice when done)")
	fmt.Println()
	basePrompt := promptMultiline("Project description > ")

	fmt.Println()
	fmt.Println("Enter architectural notes (optional — press Enter twice to skip).")
	fmt.Println("Key packages, patterns, or constraints the AI should know.")
	fmt.Println()
	archPrompt := promptMultiline("Architecture notes > ")

	fmt.Println()
	fmt.Println("LLM provider:")
	fmt.Println("  1. Anthropic (Claude)")
	fmt.Println("  2. OpenAI")
	fmt.Println("  3. Local (Ollama)")
	fmt.Println()
	providerChoice := promptChoice("Provider [1] > ", []string{"1", "2", "3"}, "1")

	var provider, apiKeyEnvVar string
	switch providerChoice {
	case "1":
		provider = "anthropic"
		apiKeyEnvVar = "ANTHROPIC_API_KEY"
	case "2":
		provider = "openai"
		apiKeyEnvVar = "OPENAI_API_KEY"
	case "3":
		provider = "local"
		apiKeyEnvVar = ""
	}

	if apiKeyEnvVar != "" && os.Getenv(apiKeyEnvVar) == "" {
		fmt.Printf("\n⚠  %s is not set. Set it before running queries.\n", apiKeyEnvVar)
		fmt.Printf("   export %s=your-key-here\n", apiKeyEnvVar)
	}

	// ── Create data directory ──────────────────────────────────────────────
	dataDir := viper.GetString("data_dir")
	if dataDir == "" {
		home, _ := os.UserHomeDir()
		dataDir = filepath.Join(home, ".ce")
	}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "graphs"), 0755); err != nil {
		return fmt.Errorf("create graphs directory: %w", err)
	}

	// ── Write ce.yaml ──────────────────────────────────────────────────────
	ceYAMLPath := filepath.Join(absPath, "ce.yaml")
	if _, err := os.Stat(ceYAMLPath); err == nil {
		fmt.Printf("\nce.yaml already exists at %s\n", ceYAMLPath)
		overwrite := promptYesNo("Overwrite? [y/N] > ", false)
		if !overwrite {
			fmt.Println("Aborted.")
			return nil
		}
	}

	cfgData := buildInitialConfig(gitURL, basePrompt, archPrompt, provider)
	if err := writeCEYAML(ceYAMLPath, cfgData); err != nil {
		return fmt.Errorf("write ce.yaml: %w", err)
	}

	// ── Register project in meta.db (Phase 1 stub) ────────────────────────
	if err := registerProject(absPath, gitURL, basePrompt, archPrompt); err != nil {
		return fmt.Errorf("register project: %w", err)
	}

	fmt.Printf("\n✓ Created %s\n", ceYAMLPath)
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Printf("  1. Run: ce index\n")
	fmt.Printf("  2. Run: ce query \"how does [something] work?\"\n")
	fmt.Println()
	fmt.Println("To add plugins:")
	fmt.Printf("  ce plugin install <path-to-plugin.wasm>\n")

	return nil
}

func resolveGitURL(absPath string) (string, error) {
	cmd := exec.Command("git", "-C", absPath, "remote", "get-url", "origin")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git remote get-url origin: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func buildInitialConfig(gitURL, basePrompt, archPrompt, provider string) map[string]any {
	return map[string]any{
		"project": map[string]any{
			"git_url":     gitURL,
			"base_prompt": basePrompt,
			"arch_prompt": archPrompt,
		},
		"llm": map[string]any{
			"provider": provider,
		},
	}
}

func writeCEYAML(path string, data map[string]any) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	_, _ = fmt.Fprintln(f, "# ce.yaml — Context Engine project configuration")
	_, _ = fmt.Fprintln(f, "# Reference: https://docs.atheory.ai/ce/config")
	_, _ = fmt.Fprintln(f)

	enc := yaml.NewEncoder(f)
	enc.SetIndent(2)
	return enc.Encode(data)
}

// registerProject writes a project record to meta.db.
// Phase 1 stub — project DB operations not yet implemented.
func registerProject(_, _, _, _ string) error {
	return nil
}
