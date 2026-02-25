package cli

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

// Version is the CE binary version. Set at build time via -ldflags.
var Version = "0.1.0-dev"

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		RunE: func(_ *cobra.Command, _ []string) error {
			fmt.Printf("ce version %s\n", Version)
			fmt.Printf("go %s %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
			return nil
		},
	}
}
