package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func newCacheCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Manage the CE compilation cache",
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:   "show",
			Short: "Show cache size and contents",
			RunE:  runCacheShow,
		},
		newCacheClearCmd(),
	)

	return cmd
}

func newCacheClearCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clear",
		Short: "Clear the compilation cache",
		RunE:  runCacheClear,
	}
	cmd.Flags().Bool("plugins", false, "clear only the plugin compilation cache")
	return cmd
}

func runCacheShow(_ *cobra.Command, _ []string) error {
	dataDir := resolveDataDirFromViper()
	cacheDir := filepath.Join(dataDir, "cache")

	entries, err := os.ReadDir(cacheDir)
	if os.IsNotExist(err) {
		fmt.Println("Cache directory is empty or does not exist.")
		return nil
	}
	if err != nil {
		return fmt.Errorf("read cache dir: %w", err)
	}

	fmt.Printf("Cache directory: %s\n", cacheDir)
	fmt.Printf("Entries: %d\n", len(entries))
	return nil
}

func runCacheClear(cmd *cobra.Command, _ []string) error {
	pluginsOnly, _ := cmd.Flags().GetBool("plugins")
	dataDir := resolveDataDirFromViper()

	if pluginsOnly {
		cacheDir := filepath.Join(dataDir, "cache", "plugins")
		entries, _ := os.ReadDir(cacheDir)
		fmt.Printf("Clearing %d plugin cache entries...\n", len(entries))
		return os.RemoveAll(cacheDir)
	}

	cacheDir := filepath.Join(dataDir, "cache")
	entries, _ := os.ReadDir(cacheDir)
	fmt.Printf("Clearing all cache (%d entries)...\n", len(entries))
	return os.RemoveAll(cacheDir)
}

func resolveDataDirFromViper() string {
	dataDir := viper.GetString("data_dir")
	if dataDir == "" {
		home, _ := os.UserHomeDir()
		dataDir = filepath.Join(home, ".ce")
	}
	return dataDir
}
