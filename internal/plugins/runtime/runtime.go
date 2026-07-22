package runtime

import (
	"fmt"
	"runtime"

	"github.com/atheory-ai/context-engine/internal/core"
)

// Runtime manages the wazero + Extism environment for loading WASM plugins.
// One Runtime is created per Engine, shared across all plugin loads.
// The Runtime holds the wazero compilation cache and the static host functions.
type Runtime struct {
	cache                 *CompilationCache
	channels              *core.AppChannels
	allowDevStreamPlugins bool
	indexPoolSize         int
}

// SetAllowDevStreamPlugins permits the legacy Javy stdin/stdout convention.
// It is deliberately opt-in: production artifacts must declare and implement
// the Extism byte input/output ABI.
func (r *Runtime) SetAllowDevStreamPlugins(allow bool) {
	r.allowDevStreamPlugins = allow
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
		cache:         cache,
		channels:      channels,
		indexPoolSize: defaultIndexPoolSize(),
	}, nil
}

// SetIndexPoolSize limits independent Extism instances per plugin. It should
// match extraction concurrency; a larger pool only retains additional WASM
// high-water memory without enabling more work.
func (r *Runtime) SetIndexPoolSize(size int) {
	if size > 0 {
		r.indexPoolSize = size
	}
}

func defaultIndexPoolSize() int {
	size := runtime.NumCPU()
	if size > 8 {
		size = 8
	}
	if size < 1 {
		return 1
	}
	return size
}

// Close releases the wazero compilation cache resources.
func (r *Runtime) Close() error {
	return r.cache.Close()
}
