package cli

import (
	"fmt"
	"os"
	"strings"

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
