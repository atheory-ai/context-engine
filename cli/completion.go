package cli

import (
	"os"

	"github.com/spf13/cobra"
)

func newCompletionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion",
		Short: "Generate shell completion scripts",
		Long: `To load completions:

  Bash:
    source <(ce completion bash)
    # To persist: ce completion bash > /etc/bash_completion.d/ce

  Zsh:
    echo "autoload -U compinit; compinit" >> ~/.zshrc
    ce completion zsh > "${fpath[1]}/_ce"

  Fish:
    ce completion fish | source
    # To persist: ce completion fish > ~/.config/fish/completions/ce.fish
`,
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:   "bash",
			Short: "Generate bash completion script",
			RunE: func(_ *cobra.Command, _ []string) error {
				return rootCmd.GenBashCompletion(os.Stdout)
			},
		},
		&cobra.Command{
			Use:   "zsh",
			Short: "Generate zsh completion script",
			RunE: func(_ *cobra.Command, _ []string) error {
				return rootCmd.GenZshCompletion(os.Stdout)
			},
		},
		&cobra.Command{
			Use:   "fish",
			Short: "Generate fish completion script",
			RunE: func(_ *cobra.Command, _ []string) error {
				return rootCmd.GenFishCompletion(os.Stdout, true)
			},
		},
		&cobra.Command{
			Use:   "powershell",
			Short: "Generate PowerShell completion script",
			RunE: func(_ *cobra.Command, _ []string) error {
				return rootCmd.GenPowerShellCompletionWithDesc(os.Stdout)
			},
		},
	)

	return cmd
}
