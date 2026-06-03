package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/atheory-ai/context-engine/internal/config"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var rootCmd = &cobra.Command{
	Use:   "ce",
	Short: "Context Engine — codebase intelligence for large projects",
	Long: `Context Engine is an AI-powered coding assistant that builds a
persistent knowledge graph of your codebase and reasons over it.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute is called from main.go.
func Execute() error {
	err := rootCmd.Execute()
	if err == nil {
		return nil
	}

	var firstRun *config.FirstRunError
	if errors.As(err, &firstRun) {
		printFirstRunGuide()
		return err
	}

	fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	return err
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().String("config", "",
		"config file (default: ./ce.yaml, then ~/.ce/config.yaml)")
	rootCmd.PersistentFlags().String("data-dir", "",
		"CE data directory (default: ~/.ce)")
	rootCmd.PersistentFlags().String("token", "",
		"API token for remote/CI access")
	rootCmd.PersistentFlags().Bool("debug", false,
		"enable debug output")
	rootCmd.PersistentFlags().Bool("show-cost", false,
		"show token cost after each query")
	rootCmd.PersistentFlags().Bool("no-color", false,
		"disable color output")

	_ = viper.BindPFlag("data_dir", rootCmd.PersistentFlags().Lookup("data-dir"))
	_ = viper.BindPFlag("token", rootCmd.PersistentFlags().Lookup("token"))
	_ = viper.BindPFlag("debug", rootCmd.PersistentFlags().Lookup("debug"))
	_ = viper.BindPFlag("show_cost", rootCmd.PersistentFlags().Lookup("show-cost"))
	_ = viper.BindPFlag("no_color", rootCmd.PersistentFlags().Lookup("no-color"))

	rootCmd.AddCommand(
		newQueryCmd(),
		newIndexCmd(),
		newProjectCmd(),
		newTokenCmd(),
		newPluginCmd(),
		newConfigCmd(),
		newServerCmd(),
		newCacheCmd(),
		newVersionCmd(),
		newCompletionCmd(),
	)
}

func initConfig() {
	cfgFile, _ := rootCmd.PersistentFlags().GetString("config")
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		viper.SetConfigName("ce")
		viper.SetConfigType("yaml")
		viper.AddConfigPath(".")
		viper.AddConfigPath("$HOME/.ce")
	}

	viper.SetEnvPrefix("CE")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	// Ignore not-found errors — first run has no config yet.
	_ = viper.ReadInConfig()
}
