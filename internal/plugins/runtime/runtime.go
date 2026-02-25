package runtime

import (
	"fmt"

	"github.com/atheory/context-engine/internal/core"
)

// Runtime manages the wazero + Extism environment for loading WASM plugins.
// One Runtime is created per Engine, shared across all plugin loads.
// The Runtime holds the wazero compilation cache and the static host functions.
type Runtime struct {
	cache    *CompilationCache
	channels *core.AppChannels
}

// New creates a Runtime for the given CE data directory.
// Creates cache directories, opens the wazero compilation cache,
// registers ce.* host functions, and starts background TTL eviction.
func New(ceDataDir string, channels *core.AppChannels) (*Runtime, error) {
	cache, err := NewCompilationCache(ceDataDir)
	if err != nil {
		return nil, fmt.Errorf("plugin compilation cache: %w", err)
	}

	// Start background eviction of stale cache entries.
	cache.Evict()

	return &Runtime{
		cache:    cache,
		channels: channels,
	}, nil
}

// Close releases the wazero compilation cache resources.
func (r *Runtime) Close() error {
	return r.cache.Close()
}
