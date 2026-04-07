package runtime

import (
	"context"
	"encoding/json"
	"sync"

	extism "github.com/extism/go-sdk"

	"github.com/atheory/context-engine/internal/core"
)

// pluginInstance wraps an Extism plugin and implements core.Plugin.
// All WASM calls are serialized through mu — Extism plugins are not goroutine-safe.
type pluginInstance struct {
	id       core.PluginID
	name     string
	version  string
	wasm     *extism.Plugin
	manifest PluginManifest
	wasmDir  string          // directory containing the plugin .wasm file
	exports  map[string]bool // set of exported function names
	mu       sync.Mutex
}

// hasExport returns true if the plugin exports the given function name.
func (p *pluginInstance) hasExport(name string) bool {
	return p.exports[name]
}

func (p *pluginInstance) ID() core.PluginID { return p.id }
func (p *pluginInstance) Name() string      { return p.name }
func (p *pluginInstance) Version() string   { return p.version }

// Language returns the language handler if this plugin declares language capability.
func (p *pluginInstance) Language() core.LanguageHandler {
	if !p.manifest.Capabilities.Language {
		return nil
	}
	return &wasmLanguageHandler{plugin: p}
}

// Roles returns the agent roles this plugin defines.
// Returns nil if the plugin does not declare role capability.
func (p *pluginInstance) Roles() []core.RoleDefinition {
	if !p.manifest.Capabilities.Role {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	_, data, err := p.wasm.Call("ce_role_definition", nil)
	if err != nil {
		return nil
	}
	var role core.RoleDefinition
	if err := json.Unmarshal(data, &role); err != nil {
		return nil
	}
	return []core.RoleDefinition{role}
}

// Tools returns the tools this plugin defines.
// Returns nil if the plugin declares no tools.
func (p *pluginInstance) Tools() []core.Tool {
	if len(p.manifest.Capabilities.Tools) == 0 {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	_, data, err := p.wasm.Call("ce_tools_list", nil)
	if err != nil {
		return nil
	}
	var descriptors []ToolDescriptor
	if err := json.Unmarshal(data, &descriptors); err != nil {
		return nil
	}

	tools := make([]core.Tool, len(descriptors))
	for i, d := range descriptors {
		tools[i] = &wasmTool{plugin: p, descriptor: d}
	}
	return tools
}

// Analyzers returns the analysis passes this plugin defines.
// Returns nil if the plugin declares no analyzers.
func (p *pluginInstance) Analyzers() []core.Analyzer {
	if len(p.manifest.Capabilities.Analyzers) == 0 {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	_, data, err := p.wasm.Call("ce_analyzers_list", nil)
	if err != nil {
		return nil
	}
	var descriptors []AnalyzerDescriptor
	if err := json.Unmarshal(data, &descriptors); err != nil {
		return nil
	}

	analyzers := make([]core.Analyzer, len(descriptors))
	for i, d := range descriptors {
		analyzers[i] = &wasmAnalyzer{plugin: p, descriptor: d}
	}
	return analyzers
}

// Close unloads the WASM plugin and frees wazero resources.
func (p *pluginInstance) Close() error {
	return p.wasm.Close(context.Background())
}
