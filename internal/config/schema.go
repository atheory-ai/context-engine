// Package config provides the Config struct and loading utilities
// for the Context Engine. It wraps ce.yaml values into typed structs.
package config

// Config is the fully resolved configuration for a CE session.
// Built by Load() from ce.yaml + global config + env vars + flags.
type Config struct {
	Project  ProjectConfig
	LLM      LLMConfig
	Engine   EngineConfig
	Indexer  IndexerConfig
	IIR      IIRConfig
	Plugins  []PluginEntry
	Tracing  TracingConfig
	Server   ServerConfig
	Data     DataConfig
	Display  DisplayConfig
	Features FeatureConfig

	// Runtime fields — not from ce.yaml
	ReadOnly bool   // true for read-scoped token sessions
	DataDir  string // resolved absolute path to ~/.ce or override
}

// ProjectConfig identifies the active project and its base prompts.
type ProjectConfig struct {
	GitURL     string `mapstructure:"git_url"`
	BasePrompt string `mapstructure:"base_prompt"`
	ArchPrompt string `mapstructure:"arch_prompt"`
}

// LLMConfig holds provider selection and API configuration.
type LLMConfig struct {
	Provider       string            `mapstructure:"provider"`
	Models         map[string]string `mapstructure:"models"` // tier → model ID
	APIKey         string            `mapstructure:"api_key"`
	BaseURL        string            `mapstructure:"base_url"`
	TimeoutSeconds int               `mapstructure:"timeout_seconds"`
	MaxRetries     int               `mapstructure:"max_retries"`
}

// EngineConfig controls cognitive loop behaviour.
type EngineConfig struct {
	MaxLoops            int     `mapstructure:"max_loops"`
	KLimit              int     `mapstructure:"k_limit"`
	ContextSafetyMargin float64 `mapstructure:"context_safety_margin"`
	DefaultRole         string  `mapstructure:"default_role"`
}

// IIRConfig controls Intermediate Intent Representation extraction at index time.
type IIRConfig struct {
	// Enabled turns on per-function IIR extraction during indexing. Because the
	// indexer skips unchanged files (by content hash), enabling this on an
	// already-indexed project only extracts IIR as files change — run
	// `ce index --full` to backfill existing files.
	Enabled bool `mapstructure:"enabled"`
}

// IndexerConfig controls file discovery and indexing behaviour.
type IndexerConfig struct {
	Include          []string `mapstructure:"include"`
	Exclude          []string `mapstructure:"exclude"`
	MaxFileSizeBytes int      `mapstructure:"max_file_size_bytes"`
	IncludeTestFiles bool     `mapstructure:"include_test_files"`
	WatchDebounceMS  int      `mapstructure:"watch_debounce_ms"`
}

// PluginEntry describes a single installed plugin.
type PluginEntry struct {
	Path   string         `mapstructure:"path"`
	Config map[string]any `mapstructure:"config"`
}

// TracingConfig controls execution log writing.
type TracingConfig struct {
	Enabled       bool `mapstructure:"enabled"`
	RetentionDays int  `mapstructure:"retention_days"`
}

// ServerConfig controls the MCP/API/WebSocket server.
type ServerConfig struct {
	Port        int      `mapstructure:"port"`
	Host        string   `mapstructure:"host"`
	MCPEnabled  bool     `mapstructure:"mcp_enabled"`
	APIEnabled  bool     `mapstructure:"api_enabled"`
	WSEnabled   bool     `mapstructure:"ws_enabled"`
	CORSOrigins []string `mapstructure:"cors_origins"`
}

// DataConfig controls the CE data directory.
type DataConfig struct {
	Dir string `mapstructure:"dir"`
}

// DisplayConfig controls output rendering.
type DisplayConfig struct {
	NoColor      bool `mapstructure:"no_color"`
	ShowCost     bool `mapstructure:"show_cost"`
	ShowThinking bool `mapstructure:"show_thinking"`
}

// FeatureConfig controls experimental or pre-release features.
type FeatureConfig struct {
	CEQuery bool `mapstructure:"ce_query"`
}
