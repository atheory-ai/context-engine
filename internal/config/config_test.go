package config

import (
	"path/filepath"
	"testing"
)

func TestResolveDataDir(t *testing.T) {
	// A configured path is made absolute and wins over the env/default.
	got := resolveDataDir("some/rel/dir")
	if !filepath.IsAbs(got) {
		t.Errorf("resolveDataDir(rel) = %q, want absolute", got)
	}

	// With no configured path, CE_DATA_DIR is used.
	t.Setenv("CE_DATA_DIR", "/env/data/dir")
	if got := resolveDataDir(""); got != "/env/data/dir" {
		t.Errorf("resolveDataDir with env = %q, want /env/data/dir", got)
	}
}

func TestApplyDefaults(t *testing.T) {
	var cfg Config
	applyDefaults(&cfg)

	// Zero-valued engine tunables get sensible defaults.
	if cfg.Engine.MaxLoops <= 0 {
		t.Errorf("MaxLoops default not applied: %d", cfg.Engine.MaxLoops)
	}
	if cfg.Engine.KLimit <= 0 {
		t.Errorf("KLimit default not applied: %d", cfg.Engine.KLimit)
	}

	// Explicit values are preserved (defaults never clobber a set value).
	custom := Config{}
	custom.Engine.MaxLoops = 99
	applyDefaults(&custom)
	if custom.Engine.MaxLoops != 99 {
		t.Errorf("applyDefaults clobbered an explicit MaxLoops: %d", custom.Engine.MaxLoops)
	}
}
