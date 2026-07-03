// Package plugins manages the lifecycle of WASM plugins for the Context Engine.
// The Registry is the engine-facing facade; the runtime sub-package handles
// the low-level wazero + Extism integration.
package plugins

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/plugins/runtime"
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
	plugins   map[core.PluginID]core.Plugin
	loadOrder []core.PluginID  // registration order, for last-registered-wins semantics
	rt        *runtime.Runtime // nil until Initialize() is called
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		plugins: make(map[core.PluginID]core.Plugin),
	}
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

// Load loads a single plugin from the given path.
// If Initialize has not been called, returns nil (no-op).
// Duplicate plugin IDs are silently replaced (last-registered wins).
func (r *Registry) Load(ctx context.Context, path string, config map[string]any) error {
	if r.rt == nil {
		return nil
	}
	p, err := r.rt.Load(ctx, path, config)
	if err != nil {
		return fmt.Errorf("load plugin %s: %w", path, err)
	}
	// If this plugin ID was already registered, close the old one.
	if old, exists := r.plugins[p.ID()]; exists {
		_ = old.Close()
	} else {
		r.loadOrder = append(r.loadOrder, p.ID())
	}
	r.plugins[p.ID()] = p
	return nil
}

// LoadAll discovers and loads all plugins from the given entries.
// If Initialize has not been called, returns nil (Phase 1 no-op).
// Plugins are loaded in order; last-registered plugin for an extension wins.
func (r *Registry) LoadAll(ctx context.Context, entries []PluginEntry) error {
	for _, e := range entries {
		if err := r.Load(ctx, e.Path, e.Config); err != nil {
			return err
		}
	}
	return nil
}

// Loaded returns the currently loaded plugins in registration order.
func (r *Registry) Loaded() []core.Plugin {
	out := make([]core.Plugin, 0, len(r.loadOrder))
	for _, id := range r.loadOrder {
		if p, ok := r.plugins[id]; ok {
			out = append(out, p)
		}
	}
	return out
}

// PluginForFile returns the last plugin that handles the given file path.
// Returns nil if no plugin matches.
// Kept for callers that need override semantics.
func (r *Registry) PluginForFile(filePath string) core.Plugin {
	matches := r.PluginsForFile(filePath)
	if len(matches) == 0 {
		return nil
	}
	return matches[len(matches)-1]
}

// PluginsForFile returns every language plugin that handles the given file path.
// This supports additive convention plugins such as WordPress or WooCommerce
// running alongside the generic PHP language plugin.
func (r *Registry) PluginsForFile(filePath string) []core.Plugin {
	ext := strings.ToLower(filepath.Ext(filePath))

	var matches []core.Plugin
	for _, id := range r.loadOrder {
		p, ok := r.plugins[id]
		if !ok {
			continue
		}
		h := p.Language()
		if h == nil {
			continue
		}
		// Check extensions first (fast path).
		matched := false
		for _, handledExt := range h.Extensions() {
			if strings.ToLower(handledExt) == ext {
				matched = true
				break
			}
		}
		// Check custom match if declared (slow path).
		if h.HasCustomMatch() && h.Match(filePath) {
			matched = true
		}
		if matched {
			matches = append(matches, p)
		}
	}

	return matches
}

// iirRuleContributor is the optional interface a plugin implements to declare an
// IIR rule pack. Kept out of core.Plugin so contributing rules is opt-in and
// doesn't ripple through every plugin implementation.
type iirRuleContributor interface {
	IIRRulePackJSON() []byte
}

// IIRRulePackJSONs returns the raw IIR rule-pack JSON declared by loaded
// plugins, in load order. Plugins that contribute none are skipped. The host
// merges these over the built-in defaults (see iir.MergePluginRulePacks).
func (r *Registry) IIRRulePackJSONs() [][]byte {
	var out [][]byte
	for _, p := range r.Loaded() {
		c, ok := p.(iirRuleContributor)
		if !ok {
			continue
		}
		if raw := c.IIRRulePackJSON(); len(raw) > 0 {
			out = append(out, raw)
		}
	}
	return out
}

// ConceptSeeds aggregates all concept seeds contributed by loaded plugins.
func (r *Registry) ConceptSeeds() []core.ConceptSeed {
	var seeds []core.ConceptSeed
	for _, p := range r.Loaded() {
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
	r.plugins = make(map[core.PluginID]core.Plugin)
	r.loadOrder = nil
	if r.rt != nil {
		_ = r.rt.Close()
		r.rt = nil
	}
}
