package config

import (
	"fmt"
	"os"
)

// validate checks for required runtime conditions.
// Returns FirstRunError if the data directory has not been created yet.
func validate(cfg *Config) error {
	if _, err := os.Stat(cfg.DataDir); os.IsNotExist(err) {
		return &FirstRunError{DataDir: cfg.DataDir}
	}
	return nil
}

// FirstRunError signals that CE has not been initialized.
// The CLI catches this and prints a first-run guide.
type FirstRunError struct {
	DataDir string
}

func (e *FirstRunError) Error() string {
	return fmt.Sprintf("CE data directory not found: %s\nRun 'ce project init' to get started.", e.DataDir)
}
