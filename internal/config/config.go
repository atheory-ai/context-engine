package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/spf13/viper"
)

// Load reads the resolved Viper config into a Config struct.
// Called at the start of every command that needs the engine.
func Load() (*Config, error) {
	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	cfg.DataDir = resolveDataDir(cfg.Data.Dir)
	applyDefaults(&cfg)

	if err := validate(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// LoadRaw reads the Viper config without validation.
// Used by commands that don't need the full engine (e.g. config show).
func LoadRaw() (*Config, error) {
	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	cfg.DataDir = resolveDataDir(cfg.Data.Dir)
	applyDefaults(&cfg)
	return &cfg, nil
}

func resolveDataDir(configured string) string {
	if configured != "" {
		abs, err := filepath.Abs(configured)
		if err == nil {
			return abs
		}
	}
	if env := os.Getenv("CE_DATA_DIR"); env != "" {
		return env
	}
	home, _ := os.UserHomeDir() //nolint:errcheck // empty home falls back to cwd-relative ".ce"
	return filepath.Join(home, ".ce")
}

func applyDefaults(cfg *Config) {
	if cfg.Engine.MaxLoops == 0 {
		cfg.Engine.MaxLoops = core.DefaultMaxLoops
	}
	if cfg.Engine.KLimit == 0 {
		cfg.Engine.KLimit = core.DefaultKLimit
	}
	if cfg.Engine.ContextSafetyMargin == 0 {
		cfg.Engine.ContextSafetyMargin = core.ContextWindowSafetyMargin
	}
	if cfg.LLM.TimeoutSeconds == 0 {
		cfg.LLM.TimeoutSeconds = 120
	}
	if cfg.LLM.MaxRetries == 0 {
		cfg.LLM.MaxRetries = 3
	}
	if cfg.LLM.Provider == "" {
		cfg.LLM.Provider = "anthropic"
	}
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8765
	}
	if cfg.Server.Host == "" {
		cfg.Server.Host = "127.0.0.1"
	}
	if cfg.Indexer.MaxFileSizeBytes == 0 {
		cfg.Indexer.MaxFileSizeBytes = 524288
	}
	if cfg.Indexer.WatchDebounceMS == 0 {
		cfg.Indexer.WatchDebounceMS = 500
	}
	if len(cfg.LLM.Models) == 0 {
		cfg.LLM.Models = map[string]string{
			core.TierFast:     "claude-haiku-4-5-20251001",
			core.TierStandard: "claude-sonnet-4-6",
			core.TierThinking: "claude-opus-4-6",
		}
	}
	if len(cfg.Indexer.Exclude) == 0 {
		cfg.Indexer.Exclude = []string{
			"vendor/**", "node_modules/**", ".git/**",
			"**/*.pb.go", "**/*_gen.go", "dist/**", "build/**",
		}
	}
}
