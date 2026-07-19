package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	extism "github.com/extism/go-sdk"

	"github.com/atheory-ai/context-engine/internal/core"
)

// Load reads a .wasm file, validates its exports, compiles it (using the
// wazero compilation cache for speed on subsequent loads), calls
// ce_plugin_manifest to read metadata, and returns a ready core.Plugin.
//
// pluginConfig is the per-plugin config block from ce.yaml, made available
// to the plugin via the ce.get_config host function.
func (r *Runtime) Load(ctx context.Context, wasmPath string, pluginConfig map[string]any) (core.Plugin, error) {
	// ── 1. Read file ─────────────────────────────────────────────────────────
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		return nil, fmt.Errorf("read wasm %s: %w", wasmPath, err)
	}

	wasmHash := WASMHash(wasmBytes)

	// ── 2. Validate exports + collect export set ─────────────────────────────
	if err := validateExports(wasmBytes); err != nil {
		return nil, fmt.Errorf("validate %s: %w", wasmPath, err)
	}
	exports, err := collectExports(wasmBytes)
	if err != nil {
		exports = map[string]bool{} // non-fatal — degrade gracefully
	}

	// ── 3. Touch cache metadata if already cached ────────────────────────────
	if r.cache.IsCached(wasmHash) {
		r.cache.TouchLastUsed(wasmHash)
	}

	// ── 4. Build per-plugin host functions with injected config ──────────────
	deps := HostDeps{
		Channels:     r.channels,
		Substrate:    nil, // provided per-query in future phases
		PluginConfig: pluginConfig,
	}
	hostFuncs := buildHostFunctions(deps)

	// ── 5. Create Extism plugin (wazero cache handles compilation artifacts) ─
	extismConfig := extism.PluginConfig{
		RuntimeConfig: r.cache.RuntimeConfig(),
		EnableWasi:    true,
	}
	extismPlugin, err := extism.NewPlugin(ctx, newExtismManifest(wasmBytes), extismConfig, hostFuncs)
	if err != nil {
		return nil, fmt.Errorf("create extism plugin %s: %w", wasmPath, err)
	}

	// ── 6. Call ce_plugin_manifest to read plugin metadata ───────────────────
	manifestExport := resolveExportName(exports, "ce_plugin_manifest")
	manifestJSON, err := callPluginManifest(ctx, extismPlugin, wasmBytes, extismConfig, hostFuncs, manifestExport)
	if err != nil {
		_ = extismPlugin.Close(ctx)
		return nil, fmt.Errorf("call ce_plugin_manifest on %s: %w", wasmPath, err)
	}

	var pmeta PluginManifest
	if err := json.Unmarshal(manifestJSON, &pmeta); err != nil {
		_ = extismPlugin.Close(ctx)
		return nil, fmt.Errorf("parse plugin manifest from %s: %w", wasmPath, err)
	}
	if err := validateManifestABI(pmeta.ABI); err != nil {
		_ = extismPlugin.Close(ctx)
		return nil, fmt.Errorf("unsupported plugin ABI in %s: %w", wasmPath, err)
	}

	// ── 7. Write cache metadata (first load only) ────────────────────────────
	if !r.cache.IsCached(wasmHash) {
		_ = r.cache.writeMeta(wasmHash, CacheMeta{ //nolint:errcheck // best-effort cache; missing meta only forces re-read on next load
			WASMHash:   wasmHash,
			PluginName: pmeta.Name,
			Version:    pmeta.Version,
			CachedAt:   time.Now().UnixMilli(),
			LastUsed:   time.Now().UnixMilli(),
		})
	}

	// ── 8. Wrap in Plugin instance ───────────────────────────────────────────
	instance := &pluginInstance{
		id:        core.PluginID(pmeta.ID),
		name:      pmeta.Name,
		version:   pmeta.Version,
		wasm:      extismPlugin,
		manifest:  pmeta,
		wasmDir:   filepath.Dir(wasmPath),
		wasmBytes: wasmBytes,
		hostFuncs: hostFuncs,
		config:    extismConfig,
		exports:   exports,
	}
	instance.indexPool = newPluginInstancePool(instance)
	return instance, nil
}

func validateManifestABI(abi *PluginABIInfo) error {
	if abi == nil {
		return nil
	}
	if abi.Name != "" && abi.Name != "ce-plugin" {
		return fmt.Errorf("name %q", abi.Name)
	}
	if abi.Version != 0 && abi.Version != 1 {
		return fmt.Errorf("version %d", abi.Version)
	}
	switch abi.CallConvention {
	case "", callConventionExtismInputOutput, callConventionJavyStreamIO:
		return nil
	default:
		return fmt.Errorf("call convention %q", abi.CallConvention)
	}
}

func newExtismManifest(wasmBytes []byte) extism.Manifest {
	return extism.Manifest{
		Wasm: []extism.Wasm{
			extism.WasmData{Data: wasmBytes},
		},
		// Sandbox: no filesystem, no network.
		AllowedHosts: []string{},
		AllowedPaths: map[string]string{},
	}
}
