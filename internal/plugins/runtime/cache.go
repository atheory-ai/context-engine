package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/tetratelabs/wazero"
)

const cacheTTLDays = 30

// CompilationCache manages WASM compilation artifacts and plugin metadata.
//
// Layout under ceDataDir:
//
//	cache/plugins/<sha256>/meta.json   — plugin metadata + last-used timestamp
//	cache/wazero/                      — wazero compiled artifacts (managed by wazero)
type CompilationCache struct {
	metaDir     string // ~/.ce/cache/plugins/
	wazeroCache wazero.CompilationCache
}

// CacheMeta is the JSON structure written to meta.json.
type CacheMeta struct {
	WASMHash   string `json:"wasm_hash"`
	PluginName string `json:"plugin_name"`
	Version    string `json:"version"`
	CachedAt   int64  `json:"cached_at"`
	LastUsed   int64  `json:"last_used"`
}

// NewCompilationCache initializes the cache, creating directories as needed.
// The wazero compilation cache is opened for the wazero/ subdirectory.
func NewCompilationCache(ceDataDir string) (*CompilationCache, error) {
	metaDir := filepath.Join(ceDataDir, "cache", "plugins")
	wazeroDir := filepath.Join(ceDataDir, "cache", "wazero")

	for _, d := range []string{metaDir, wazeroDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			return nil, fmt.Errorf("create cache dir %s: %w", d, err)
		}
	}

	wc, err := wazero.NewCompilationCacheWithDir(wazeroDir)
	if err != nil {
		return nil, fmt.Errorf("wazero compilation cache: %w", err)
	}

	return &CompilationCache{
		metaDir:     metaDir,
		wazeroCache: wc,
	}, nil
}

// RuntimeConfig returns a wazero.RuntimeConfig wired to this cache.
// Pass this to extism.PluginConfig.RuntimeConfig so compiled WASM modules
// are persisted across process restarts.
func (c *CompilationCache) RuntimeConfig() wazero.RuntimeConfig {
	return wazero.NewRuntimeConfig().WithCompilationCache(c.wazeroCache)
}

// WASMHash computes the cache key (SHA-256) for a .wasm file's raw bytes.
// Same bytes → same key regardless of filename or path.
func WASMHash(wasmBytes []byte) string {
	h := sha256.Sum256(wasmBytes)
	return hex.EncodeToString(h[:])
}

func (c *CompilationCache) hashDir(wasmHash string) string {
	return filepath.Join(c.metaDir, wasmHash)
}

// IsCached returns true if a meta.json exists for this wasm hash.
// A cache miss means either first load or eviction.
func (c *CompilationCache) IsCached(wasmHash string) bool {
	_, err := os.Stat(filepath.Join(c.hashDir(wasmHash), "meta.json"))
	return err == nil
}

// TouchLastUsed updates the last_used timestamp in meta.json.
// Called on every cache hit to reset the TTL.
func (c *CompilationCache) TouchLastUsed(wasmHash string) {
	metaPath := filepath.Join(c.hashDir(wasmHash), "meta.json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return
	}
	var meta CacheMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return
	}
	meta.LastUsed = time.Now().UnixMilli()
	updated, _ := json.Marshal(meta)           //nolint:errcheck // marshaling a struct with primitive fields never errors
	_ = os.WriteFile(metaPath, updated, 0o644) //nolint:errcheck,gosec // best-effort LRU touch; G306: plugin-cache metadata read by other CE invocations
}

// writeMeta writes a new meta.json for the given hash.
// Creates the hash directory if it doesn't exist.
func (c *CompilationCache) writeMeta(wasmHash string, meta CacheMeta) error {
	dir := c.hashDir(wasmHash)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create cache entry dir: %w", err)
	}
	data, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal cache meta: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, "meta.json"), data, 0o644) //nolint:gosec // G306: plugin-cache metadata read by other CE invocations
}

// Close closes the underlying wazero compilation cache.
func (c *CompilationCache) Close() error {
	return c.wazeroCache.Close(context.Background())
}

// Evict removes cache entries that have not been used within cacheTTLDays.
// Runs in a background goroutine — non-blocking, best-effort.
func (c *CompilationCache) Evict() {
	go func() {
		entries, err := os.ReadDir(c.metaDir)
		if err != nil {
			return
		}
		cutoff := time.Now().AddDate(0, 0, -cacheTTLDays).UnixMilli()
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			metaPath := filepath.Join(c.metaDir, entry.Name(), "meta.json")
			data, err := os.ReadFile(metaPath)
			if err != nil {
				continue
			}
			var meta CacheMeta
			if err := json.Unmarshal(data, &meta); err != nil {
				continue
			}
			if meta.LastUsed < cutoff {
				_ = os.RemoveAll(filepath.Join(c.metaDir, entry.Name()))
			}
		}
	}()
}
