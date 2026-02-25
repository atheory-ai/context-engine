// Package plugins manages the lifecycle of WASM plugins for the Context Engine.
// The Registry is the engine-facing facade; the runtime sub-package handles
// the low-level wazero + Extism integration.
package plugins

import (
	"context"
	"fmt"

	"github.com/atheory/context-engine/internal/core"
	"github.com/atheory/context-engine/internal/plugins/runtime"
)

// PluginEntry describes a plugin to load.
// The runner converts []config.PluginEntry → []PluginEntry before calling LoadAll.
type PluginEntry struct {
	Path   string
	Config map[string]any
}

// Registry manages loaded plugins for the engine lifetime.
// Thread-safe — the runner reads from it concurrently during fan-out.
type Registry struct {
	plugins []core.Plugin
	rt      *runtime.Runtime // nil until Initialize() is called
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{}
}

// Initialize wires the plugin runtime to this registry.
// Must be called before LoadAll. Creates the wazero compilation cache under
// ceDataDir and registers all ce.* host functions.
func (r *Registry) Initialize(ceDataDir string, ch *core.AppChannels) error {
	rt, err := runtime.New(ceDataDir, ch)
	if err != nil {
		return fmt.Errorf("initialize plugin runtime: %w", err)
	}
	r.rt = rt
	return nil
}

// LoadAll discovers and loads all plugins from the given entries.
// If Initialize has not been called, returns nil (Phase 1 no-op).
// Plugins are loaded in order; duplicate plugin IDs are rejected.
func (r *Registry) LoadAll(ctx context.Context, entries []PluginEntry) error {
	if r.rt == nil {
		return nil // runtime not initialized — no-op
	}
	for _, e := range entries {
		p, err := r.rt.Load(ctx, e.Path, e.Config)
		if err != nil {
			return fmt.Errorf("load plugin %s: %w", e.Path, err)
		}
		r.plugins = append(r.plugins, p)
	}
	return nil
}

// Loaded returns the currently loaded plugins.
func (r *Registry) Loaded() []core.Plugin {
	return r.plugins
}

// ConceptSeeds aggregates all concept seeds contributed by loaded plugins.
func (r *Registry) ConceptSeeds() []core.ConceptSeed {
	var seeds []core.ConceptSeed
	for _, p := range r.plugins {
		if lang := p.Language(); lang != nil {
			seeds = append(seeds, lang.Concepts()...)
		}
	}
	return seeds
}

// UnloadAll closes all loaded plugins and clears the registry.
func (r *Registry) UnloadAll() {
	for _, p := range r.plugins {
		_ = p.Close()
	}
	r.plugins = nil
	if r.rt != nil {
		_ = r.rt.Close()
		r.rt = nil
	}
}
