package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sync"

	extism "github.com/extism/go-sdk"
	"github.com/tetratelabs/wazero"

	"github.com/atheory-ai/context-engine/internal/core"
)

// pluginInstance wraps an Extism plugin and implements core.Plugin.
// All WASM calls are serialized through mu — Extism plugins are not goroutine-safe.
type pluginInstance struct {
	id        core.PluginID
	name      string
	version   string
	wasm      *extism.Plugin
	manifest  PluginManifest
	wasmDir   string // directory containing the plugin .wasm file
	wasmBytes []byte
	hostFuncs []extism.HostFunction
	config    extism.PluginConfig
	exports   map[string]bool // set of exported function names
	mu        sync.Mutex
}

const (
	callConventionExtismInputOutput = "extism-input-output"
	callConventionJavyStreamIO      = "javy-stream-io"
)

func callPlugin(ctx context.Context, wasm *extism.Plugin, wasmBytes []byte, config extism.PluginConfig, hostFuncs []extism.HostFunction, name string, input []byte) ([]byte, error) {
	_, output, err := wasm.CallWithContext(ctx, name, input)
	if err != nil {
		return nil, err
	}
	return output, nil
}

func callPluginManifest(ctx context.Context, wasm *extism.Plugin, wasmBytes []byte, config extism.PluginConfig, hostFuncs []extism.HostFunction, name string) ([]byte, error) {
	output, err := callPlugin(ctx, wasm, wasmBytes, config, hostFuncs, name, nil)
	if err != nil || len(output) > 0 {
		return output, err
	}
	return callPluginStreamIO(ctx, wasmBytes, config, hostFuncs, name, nil)
}

func callPluginStreamIO(ctx context.Context, wasmBytes []byte, config extism.PluginConfig, hostFuncs []extism.HostFunction, name string, input []byte) ([]byte, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	fallbackConfig := config
	fallbackConfig.EnableWasi = true
	fallbackConfig.ModuleConfig = wazero.NewModuleConfig().
		WithStdin(bytes.NewReader(input)).
		WithStdout(&stdout).
		WithStderr(&stderr)

	fallback, err := extism.NewPlugin(ctx, newExtismManifest(wasmBytes), fallbackConfig, hostFuncs)
	if err != nil {
		return nil, fmt.Errorf("create stream fallback plugin: %w", err)
	}
	defer fallback.Close(ctx) //nolint:errcheck

	_, fallbackOutput, err := fallback.CallWithContext(ctx, name, nil)
	if err != nil {
		return nil, err
	}
	if len(fallbackOutput) > 0 {
		return fallbackOutput, nil
	}
	if stdout.Len() > 0 {
		return stdout.Bytes(), nil
	}
	return fallbackOutput, nil
}

func (p *pluginInstance) call(name string, input []byte) ([]byte, error) {
	exportName := p.exportName(name)
	if p.callConvention() == callConventionJavyStreamIO {
		return callPluginStreamIO(context.Background(), p.wasmBytes, p.config, p.hostFuncs, exportName, input)
	}
	return callPlugin(context.Background(), p.wasm, p.wasmBytes, p.config, p.hostFuncs, exportName, input)
}

// hasExport returns true if the plugin exports the given function name.
func (p *pluginInstance) hasExport(name string) bool {
	return p.exports[p.exportName(name)]
}

func (p *pluginInstance) exportName(name string) string {
	if p.exports[name] {
		return name
	}
	if alias, ok := exportAliases[name]; ok && p.exports[alias] {
		return alias
	}
	return name
}

func (p *pluginInstance) callConvention() string {
	if p.manifest.ABI == nil || p.manifest.ABI.CallConvention == "" {
		return callConventionExtismInputOutput
	}
	return p.manifest.ABI.CallConvention
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

	data, err := p.call("ce_role_definition", nil)
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

	data, err := p.call("ce_tools_list", nil)
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

	data, err := p.call("ce_analyzers_list", nil)
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
