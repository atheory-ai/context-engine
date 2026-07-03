// Package runtime implements the wazero + Extism plugin engine.
// It loads, validates, and manages WASM plugins for the Context Engine.
package runtime

import "encoding/json"

// PluginManifest is the JSON structure returned by ce_plugin_manifest.
// Every valid CE plugin returns this when called with no input.
type PluginManifest struct {
	ID           string              `json:"id"`
	Name         string              `json:"name"`
	Version      string              `json:"version"`
	ABI          *PluginABIInfo      `json:"abi,omitempty"`
	Capabilities PluginCapabilities  `json:"capabilities"`
	Language     *PluginLanguageInfo `json:"language,omitempty"`

	// IIRRules is an optional IIR rule pack (YAML or JSON) this plugin
	// contributes — its "flavour" of code expectations. Declarative data: the
	// host merges it over the built-in defaults with no WASM call. Opaque here;
	// internal/iir owns the schema.
	IIRRules json.RawMessage `json:"iirRules,omitempty"`
}

// PluginABIInfo declares the SDK/runtime ABI contract a plugin was built for.
type PluginABIInfo struct {
	Name           string `json:"name"`
	Version        int    `json:"version"`
	CallConvention string `json:"callConvention,omitempty"`
}

func (a *PluginABIInfo) UnmarshalJSON(data []byte) error {
	type alias PluginABIInfo
	var raw struct {
		alias
		SnakeCallConvention string `json:"call_convention"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*a = PluginABIInfo(raw.alias)
	if a.CallConvention == "" {
		a.CallConvention = raw.SnakeCallConvention
	}
	return nil
}

// PluginCapabilities declares what this plugin can do.
// The engine uses this to decide which WASM functions to call.
// If Language is false, ce_language_match is never called.
// If Tools is empty, ce_tools_list is never called.
type PluginCapabilities struct {
	Language  bool     `json:"language"`
	Role      bool     `json:"role"`
	Analyzers []string `json:"analyzers"` // names of provided analyzers
	Tools     []string `json:"tools"`     // names of provided tools
}

// PluginLanguageInfo is the optional language metadata in the manifest.
// Declared by plugins that handle file parsing.
type PluginLanguageInfo struct {
	// Extensions are the file extensions this plugin handles.
	Extensions []string `json:"extensions"`
	// Grammar is the path to a tree-sitter grammar WASM file,
	// relative to the plugin .wasm file. Optional.
	Grammar string `json:"grammar,omitempty"`
}

// ToolDescriptor is returned by ce_tools_list as a JSON array element.
type ToolDescriptor struct {
	Name        string `json:"name"`
	Description string `json:"description"` // max 100 chars — shown in Strategizer tool list
}

// AnalyzerDescriptor is returned by ce_analyzers_list as a JSON array element.
type AnalyzerDescriptor struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}
