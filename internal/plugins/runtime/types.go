// Package runtime implements the wazero + Extism plugin engine.
// It loads, validates, and manages WASM plugins for the Context Engine.
package runtime

// PluginManifest is the JSON structure returned by ce_plugin_manifest.
// Every valid CE plugin returns this when called with no input.
type PluginManifest struct {
	ID           string              `json:"id"`
	Name         string              `json:"name"`
	Version      string              `json:"version"`
	Capabilities PluginCapabilities  `json:"capabilities"`
	Language     *PluginLanguageInfo `json:"language,omitempty"`
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
