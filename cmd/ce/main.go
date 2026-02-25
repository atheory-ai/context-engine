package main

import (
	"os"

	"github.com/atheory/context-engine/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
