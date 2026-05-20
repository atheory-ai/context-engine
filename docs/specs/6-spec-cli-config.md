# Context Engine — Spec 6: CLI / Config
## Implementation Spec — Cobra Command Tree, Viper Bindings, ce.yaml Schema
### Version 1.0 | February 2026

---

> This is an implementation spec, not a PRD section.
> Hand this document to Claude Code alongside all prior specs.
> The command tree here is complete and authoritative.
> Companion: Context Engine PRD v0.5 Section 16. Decisions Log v1.0 Section 8.

---

## 1. Overview

The CLI is the primary user interface for Phase 1. It is thin — business logic
lives in `internal/`. The CLI's job is flag parsing, config loading, and
handing control to the engine or TUI.

Framework: **Cobra** for command structure, **Viper** for config.
Both are already in the Go ecosystem standard toolkit; no surprises.

---

## 2. Entry Point

```go
// cmd/ce/main.go
// This file stays under 50 lines. Always.

package main

import (
    "os"
    "github.com/atheory-ai/context-engine/cli"
)

func main() {
    if err := cli.Execute(); err != nil {
        os.Exit(1)
    }
}
```

```go
// cli/root.go

package cli

import (
    "github.com/spf13/cobra"
    "github.com/spf13/viper"
)

var rootCmd = &cobra.Command{
    Use:   "ce",
    Short: "Context Engine — codebase intelligence for large projects",
    Long: `Context Engine is an AI-powered coding assistant that builds a
persistent knowledge graph of your codebase and reasons over it.`,
    SilenceUsage:  true,  // don't print usage on every error
    SilenceErrors: true,  // we print errors ourselves
}

// Execute is called from main.go.
func Execute() error {
    return rootCmd.Execute()
}

func init() {
    cobra.OnInitialize(initConfig)

    // Persistent flags — available on every subcommand
    rootCmd.PersistentFlags().String("config", "",
        "config file (default: ./ce.yaml, then ~/.ce/config.yaml)")
    rootCmd.PersistentFlags().String("data-dir", "",
        "CE data directory (default: ~/.ce)")
    rootCmd.PersistentFlags().String("token", "",
        "API token for remote/CI access")
    rootCmd.PersistentFlags().Bool("debug", false,
        "enable debug output")
    rootCmd.PersistentFlags().Bool("show-cost", false,
        "show token cost after each query")
    rootCmd.PersistentFlags().Bool("no-color", false,
        "disable color output")

    viper.BindPFlag("data_dir", rootCmd.PersistentFlags().Lookup("data-dir"))
    viper.BindPFlag("token",    rootCmd.PersistentFlags().Lookup("token"))
    viper.BindPFlag("debug",    rootCmd.PersistentFlags().Lookup("debug"))
    viper.BindPFlag("show_cost", rootCmd.PersistentFlags().Lookup("show-cost"))
    viper.BindPFlag("no_color", rootCmd.PersistentFlags().Lookup("no-color"))

    // Register all subcommands
    rootCmd.AddCommand(
        newQueryCmd(),
        newIndexCmd(),
        newProjectCmd(),
        newTokenCmd(),
        newPluginCmd(),
        newConfigCmd(),
        newServerCmd(),
        newCacheCmd(),
        newVersionCmd(),
        newCompletionCmd(),
    )
}

func initConfig() {
    // Config file resolution order:
    // 1. --config flag
    // 2. ./ce.yaml (project config)
    // 3. ~/.ce/config.yaml (global config)
    // Viper merges all found files — project config overrides global.

    cfgFile, _ := rootCmd.PersistentFlags().GetString("config")
    if cfgFile != "" {
        viper.SetConfigFile(cfgFile)
    } else {
        viper.SetConfigName("ce")
        viper.SetConfigType("yaml")
        viper.AddConfigPath(".")               // project root first
        viper.AddConfigPath("$HOME/.ce")       // then global
    }

    viper.SetEnvPrefix("CE")
    viper.AutomaticEnv()  // CE_DATA_DIR → data_dir, etc.

    // Read config — ignore not-found error (first run has no config yet)
    viper.ReadInConfig()
}
```

---

## 3. Full Command Tree

```
ce
├── query [text]              Run a query against the active project
├── index [path]              Index a project (or reindex if already indexed)
├── project
│   ├── init [path]           Register and configure a project
│   ├── list                  List all registered projects
│   ├── status                Show active project index status
│   ├── set <git-url>         Set the active project for this directory
│   └── remove <git-url>      Unregister a project (keeps graph file)
├── token
│   ├── create                Create a new API token
│   ├── list                  List all tokens
│   └── revoke <token-id>     Revoke a token
├── plugin
│   ├── build [path]          Compile TypeScript/JS plugin to .wasm
│   ├── dev [path]            Live development loop with coverage analysis
│   ├── validate <file>       Validate a .wasm plugin file
│   ├── install <file|url>    Register a plugin in ce.yaml
│   ├── list                  List installed plugins
│   └── remove <name>         Unregister a plugin
├── config
│   ├── show                  Print resolved config (all sources merged)
│   ├── get <key>             Get a specific config value
│   └── set <key> <value>     Set a value in the project ce.yaml
├── server
│   ├── start                 Start the MCP/API/WebSocket server
│   ├── stop                  Stop the running server
│   └── status                Show server status
├── cache
│   ├── show                  Show cache size and contents
│   └── clear [--plugins]     Clear compilation cache
└── version                   Print version information
└── completion                Generate shell completion scripts
    ├── bash
    ├── zsh
    ├── fish
    └── powershell
```

---

## 4. Command Implementations

### 4.1 `ce query`

The primary command. Constructs the engine, runs the cognitive loop, renders output.

```go
// cli/query.go

func newQueryCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "query [text]",
        Short: "Run a query against the active project",
        Long: `Run an AI-powered investigation query against the indexed codebase.

If text is omitted, launches the interactive TUI.
If text is provided, runs in CLI mode and prints the answer.`,
        RunE: runQuery,
    }

    cmd.Flags().Bool("tui", false,
        "force TUI mode even when text is provided")
    cmd.Flags().String("role", "",
        "agent role to use (overrides project default)")
    cmd.Flags().Int("max-loops", 0,
        "maximum cognitive loop iterations (0 = use project default)")
    cmd.Flags().Int("k-limit", 0,
        "maximum nodes per activation query (0 = use project default)")
    cmd.Flags().String("model", "",
        "model tier to use: fast|standard|thinking")
    cmd.Flags().Bool("trace", false,
        "write verbatim LLM calls to execution.db (overrides config)")

    viper.BindPFlag("engine.max_loops", cmd.Flags().Lookup("max-loops"))
    viper.BindPFlag("engine.k_limit",   cmd.Flags().Lookup("k-limit"))

    return cmd
}

func runQuery(cmd *cobra.Command, args []string) error {
    cfg, err := config.Load()
    if err != nil {
        return err
    }

    // No text + no --tui flag = TUI mode
    // Text provided = CLI mode
    // --tui flag = TUI mode regardless
    forceTUI, _ := cmd.Flags().GetBool("tui")
    if len(args) == 0 || forceTUI {
        return runTUI(cfg)
    }

    query := strings.Join(args, " ")
    return runCLIQuery(cmd, cfg, query)
}

func runCLIQuery(cmd *cobra.Command, cfg *config.Config, query string) error {
    ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
    defer cancel()

    engine, err := runner.New(ctx, cfg)
    if err != nil {
        return fmt.Errorf("engine init: %w", err)
    }
    defer engine.Close(context.Background())

    ch := engine.Channels()

    // Start renderer goroutine — reads channels, writes to stdout
    renderer := newCLIRenderer(cmd, cfg, ch)
    go renderer.Run(ctx)

    // Run the query
    if err := engine.Query(ctx, query); err != nil {
        return err
    }

    // Wait for renderer to drain channels
    renderer.Wait()
    return nil
}
```

### CLI Renderer

The renderer pops from channels and writes to stdout. It respects the
`--debug`, `--show-cost`, and `--no-color` flags.

```go
// cli/renderer.go

type cliRenderer struct {
    ch      core.AppChannels
    debug   bool
    showCost bool
    noColor bool
    done    chan struct{}
    writer  io.Writer
}

func (r *cliRenderer) Run(ctx context.Context) {
    defer close(r.done)

    for {
        select {
        case e, ok := <-r.ch.Thinking:
            if !ok { return }
            if r.debug {
                r.print(dimStyle, "  ↳ "+e.Content)
            }
        case e, ok := <-r.ch.Action:
            if !ok { return }
            r.print(actionStyle, "  • "+e.Content)
        case e, ok := <-r.ch.Message:
            if !ok { return }
            // Final answer — render as markdown if terminal supports it
            r.printMarkdown(e.Content)
        case e, ok := <-r.ch.Warning:
            if !ok { return }
            r.print(warnStyle, "  ⚠ "+e.Content)
        case e, ok := <-r.ch.Error:
            if !ok { return }
            r.print(errStyle, "  ✗ "+e.Content)
        case e, ok := <-r.ch.Cost:
            if !ok { return }
            if r.showCost {
                r.print(dimStyle, "  $ "+e.Content)
            }
        case e, ok := <-r.ch.Debug:
            if !ok { return }
            if r.debug {
                r.print(dimStyle, "  [debug] "+e.Content)
            }
        case <-ctx.Done():
            return
        }
    }
}
```

### 4.2 `ce index`

```go
// cli/index.go

func newIndexCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "index [path]",
        Short: "Index a project (or reindex if already indexed)",
        Long: `Walk the project directory, extract AST nodes and edges via
language plugins, and populate the substrate graph.

If path is omitted, uses the current directory.
Runs an incremental reindex if the project was previously indexed.
Use --full to force a complete reindex.`,
        RunE: runIndex,
    }

    cmd.Flags().Bool("full", false,
        "force full reindex (ignore previous index state)")
    cmd.Flags().Bool("watch", false,
        "keep running and reindex on file changes")
    cmd.Flags().StringSlice("include", nil,
        "glob patterns to include (overrides ce.yaml)")
    cmd.Flags().StringSlice("exclude", nil,
        "glob patterns to exclude (overrides ce.yaml)")

    return cmd
}
```

### 4.3 `ce project init`

The project init command is interactive. It walks the user through the
minimum required configuration and writes a `ce.yaml` to the project root.

```go
// cli/project.go

func runProjectInit(cmd *cobra.Command, args []string) error {
    projectPath := "."
    if len(args) > 0 {
        projectPath = args[0]
    }

    // Resolve absolute path and check it's a git repo
    absPath, err := filepath.Abs(projectPath)
    if err != nil {
        return err
    }

    gitURL, err := resolveGitURL(absPath)
    if err != nil {
        return fmt.Errorf("not a git repository (or no remote configured): %w", err)
    }

    fmt.Printf("Initializing Context Engine for: %s\n\n", absPath)
    fmt.Printf("Git remote: %s\n\n", gitURL)

    // ── Interactive prompts ────────────────────────────────────────────────
    // These populate base_prompt and arch_prompt.
    // The user can edit ce.yaml afterward for more detail.

    fmt.Println("Enter a brief description of this project (1-3 sentences).")
    fmt.Println("This becomes the base context injected into every query.")
    fmt.Println("Example: 'A Go microservice that handles volunteer scheduling")
    fmt.Println("          and billing for a non-profit platform.'")
    fmt.Println()
    basePrompt := promptMultiline("Project description > ")

    fmt.Println()
    fmt.Println("Enter architectural notes (optional — press Enter twice to skip).")
    fmt.Println("Key packages, patterns, or constraints the AI should know.")
    fmt.Println("Example: 'Billing logic lives in internal/billing. The scheduler")
    fmt.Println("          uses a DAG in internal/graph. No ORM — raw sql + sqlx.'")
    fmt.Println()
    archPrompt := promptMultiline("Architecture notes > ")

    // ── LLM provider setup ─────────────────────────────────────────────────
    fmt.Println()
    fmt.Println("LLM provider:")
    fmt.Println("  1. Anthropic (Claude)")
    fmt.Println("  2. OpenAI")
    fmt.Println("  3. Local (Ollama)")
    fmt.Println()
    providerChoice := promptChoice("Provider [1] > ", []string{"1", "2", "3"}, "1")

    var provider, apiKeyEnvVar string
    switch providerChoice {
    case "1":
        provider = "anthropic"
        apiKeyEnvVar = "ANTHROPIC_API_KEY"
    case "2":
        provider = "openai"
        apiKeyEnvVar = "OPENAI_API_KEY"
    case "3":
        provider = "local"
        apiKeyEnvVar = ""
    }

    if apiKeyEnvVar != "" && os.Getenv(apiKeyEnvVar) == "" {
        fmt.Printf("\n⚠  %s is not set. Set it before running queries.\n", apiKeyEnvVar)
        fmt.Printf("   export %s=your-key-here\n", apiKeyEnvVar)
    }

    // ── Write ce.yaml ──────────────────────────────────────────────────────
    ceYAMLPath := filepath.Join(absPath, "ce.yaml")
    if _, err := os.Stat(ceYAMLPath); err == nil {
        fmt.Printf("\nce.yaml already exists at %s\n", ceYAMLPath)
        overwrite := promptYesNo("Overwrite? [y/N] > ", false)
        if !overwrite {
            fmt.Println("Aborted.")
            return nil
        }
    }

    cfg := buildInitialConfig(gitURL, basePrompt, archPrompt, provider)
    if err := writeCEYAML(ceYAMLPath, cfg); err != nil {
        return fmt.Errorf("write ce.yaml: %w", err)
    }

    // ── Register project in meta.db ────────────────────────────────────────
    if err := registerProject(absPath, gitURL, basePrompt, archPrompt); err != nil {
        return fmt.Errorf("register project: %w", err)
    }

    fmt.Printf("\n✓ Created %s\n", ceYAMLPath)
    fmt.Println()
    fmt.Println("Next steps:")
    fmt.Printf("  1. Run: ce index\n")
    fmt.Printf("  2. Run: ce query \"how does [something] work?\"\n")
    fmt.Println()
    fmt.Println("To add plugins:")
    fmt.Printf("  ce plugin install <path-to-plugin.wasm>\n")

    return nil
}
```

### 4.4 `ce token create`

```go
func runTokenCreate(cmd *cobra.Command, args []string) error {
    name, _ := cmd.Flags().GetString("name")
    scope, _ := cmd.Flags().GetString("scope")
    days, _ := cmd.Flags().GetInt("expires-days")

    if name == "" {
        name = promptString("Token name > ")
    }

    // Validate scope
    switch scope {
    case "read", "read-write", "admin":
        // valid
    default:
        return fmt.Errorf("invalid scope %q — must be: read, read-write, admin", scope)
    }

    token, err := createToken(name, scope, days)
    if err != nil {
        return err
    }

    fmt.Printf("\nToken created:\n\n")
    fmt.Printf("  ID:     %s\n", token.ID)
    fmt.Printf("  Name:   %s\n", token.Name)
    fmt.Printf("  Scope:  %s\n", token.Scope)
    if token.ExpiresAt != nil {
        fmt.Printf("  Expires: %s\n", time.UnixMilli(*token.ExpiresAt).Format("2006-01-02"))
    }
    fmt.Println()
    fmt.Println("⚠  This token will not be shown again. Store it securely.")
    fmt.Printf("   CE_TOKEN=%s\n", token.ID)

    return nil
}
```

### 4.5 `ce config show`

```go
func runConfigShow(cmd *cobra.Command, args []string) error {
    // Print the fully resolved config — all sources merged.
    // Masks sensitive values (API keys, tokens).
    settings := viper.AllSettings()
    maskSensitive(settings)

    enc := yaml.NewEncoder(os.Stdout)
    enc.SetIndent(2)
    return enc.Encode(settings)
}

func maskSensitive(m map[string]any) {
    sensitiveKeys := []string{"api_key", "token", "secret", "password"}
    for k, v := range m {
        for _, sensitive := range sensitiveKeys {
            if strings.Contains(strings.ToLower(k), sensitive) {
                if str, ok := v.(string); ok && len(str) > 0 {
                    m[k] = str[:4] + "****"
                }
            }
        }
        if nested, ok := v.(map[string]any); ok {
            maskSensitive(nested)
        }
    }
}
```

### 4.6 `ce cache clear`

```go
func runCacheClear(cmd *cobra.Command, args []string) error {
    pluginsOnly, _ := cmd.Flags().GetBool("plugins")

    dataDir := viper.GetString("data_dir")
    if dataDir == "" {
        dataDir = filepath.Join(os.Getenv("HOME"), ".ce")
    }

    if pluginsOnly {
        cacheDir := filepath.Join(dataDir, "cache", "plugins")
        entries, _ := os.ReadDir(cacheDir)
        fmt.Printf("Clearing %d plugin cache entries...\n", len(entries))
        return os.RemoveAll(cacheDir)
    }

    cacheDir := filepath.Join(dataDir, "cache")
    entries, _ := os.ReadDir(cacheDir)
    fmt.Printf("Clearing all cache (%d entries)...\n", len(entries))
    return os.RemoveAll(cacheDir)
}
```

---

## 5. `ce.yaml` — Full Schema

This is the authoritative `ce.yaml` schema with all fields, types, defaults,
and comments. The `config.Load()` function reads this via Viper.

```yaml
# ce.yaml — Context Engine project configuration
# Reference: https://docs.atheory.ai/ce/config

# ── Project ────────────────────────────────────────────────────────────────
project:
  # Git remote URL. Set automatically by `ce project init`.
  # Identifies this project regardless of filesystem path.
  git_url: ""

  # Brief description of this project (1-3 sentences).
  # Injected into every Strategizer prompt.
  base_prompt: |
    A Go microservice that handles volunteer scheduling and billing
    for a non-profit platform.

  # Architectural notes. Injected into Strategizer and deep reasoning nodes.
  # Optional but strongly recommended.
  arch_prompt: |
    Billing logic lives in internal/billing.
    The scheduler uses a property graph in internal/graph.
    No ORM — raw sql + sqlx throughout.
    Plugins are WASM loaded via wazero + Extism.

# ── LLM Provider ───────────────────────────────────────────────────────────
llm:
  # Provider: anthropic | openai | local
  provider: anthropic

  # Model selection per tier.
  # The router selects tier; these map tier to model ID.
  models:
    fast:     claude-haiku-4-5-20251001
    standard: claude-sonnet-4-6
    thinking: claude-opus-4-6

  # API key. Prefer environment variable: CE_LLM_API_KEY or provider-specific
  # ANTHROPIC_API_KEY / OPENAI_API_KEY. Do not commit this field.
  api_key: ""

  # Base URL override. For local/proxied deployments.
  # Leave empty to use provider defaults.
  base_url: ""

  # Request timeout in seconds.
  timeout_seconds: 120

  # Maximum retries on transient errors (rate limits, 5xx).
  max_retries: 3

# ── Engine ─────────────────────────────────────────────────────────────────
engine:
  # Default maximum cognitive loop iterations per query.
  # Can be overridden per-query by the Strategizer or --max-loops flag.
  max_loops: 8

  # Default maximum nodes returned per activation query.
  # Can be overridden per-query by the Strategizer or --k-limit flag.
  k_limit: 30

  # Context window safety margin.
  # Loop exits when context usage reaches this fraction of model limit.
  context_safety_margin: 0.85

  # Default agent role. Empty = built-in default role.
  default_role: ""

# ── Indexing ───────────────────────────────────────────────────────────────
indexer:
  # Glob patterns to include. Empty = include everything not excluded.
  include: []

  # Glob patterns to always exclude.
  exclude:
    - "vendor/**"
    - "node_modules/**"
    - ".git/**"
    - "**/*.pb.go"      # generated protobuf
    - "**/*_gen.go"     # generated code
    - "dist/**"
    - "build/**"

  # Maximum file size to index (bytes). Larger files are skipped.
  max_file_size_bytes: 524288  # 512KB

  # Whether to index test files.
  include_test_files: true

  # Debounce interval for file watcher (milliseconds).
  watch_debounce_ms: 500

# ── Plugins ────────────────────────────────────────────────────────────────
plugins:
  # List of .wasm plugin files to load at startup.
  # Paths are relative to this file or absolute.
  installed: []
  # - path: ~/.ce/plugins/go-language.wasm
  #   config:
  #     include_test_files: true

# ── Tracing ────────────────────────────────────────────────────────────────
tracing:
  # Write verbatim LLM calls to execution.db.
  # Always enabled in development (env = development).
  # In production, requires --trace flag or this setting.
  enabled: false

  # Retention: how many days to keep execution log entries.
  # 0 = keep forever.
  retention_days: 30

# ── Server ─────────────────────────────────────────────────────────────────
server:
  # Port for MCP/API/WebSocket server.
  port: 8765

  # Bind address.
  host: "127.0.0.1"

  # Enable specific server protocols.
  mcp_enabled: true
  api_enabled: true
  ws_enabled:  true

  # CORS allowed origins for API/WebSocket.
  cors_origins:
    - "http://localhost:*"
    - "http://127.0.0.1:*"

# ── Data ───────────────────────────────────────────────────────────────────
data:
  # Directory for CE databases, cache, and plugins.
  # Default: ~/.ce
  # Override with CE_DATA_DIR environment variable.
  dir: ""

# ── Display ────────────────────────────────────────────────────────────────
display:
  # Disable color output.
  no_color: false

  # Show token cost after each query.
  show_cost: false

  # Show thinking stream in CLI mode (non-TUI).
  show_thinking: false
```

---

## 6. Config Go Struct

```go
// internal/config/schema.go

package config

// Config is the fully resolved configuration for a CE session.
// Built by Load() from ce.yaml + global config + env vars + flags.
type Config struct {
    Project  ProjectConfig
    LLM      LLMConfig
    Engine   EngineConfig
    Indexer  IndexerConfig
    Plugins  []PluginEntry
    Tracing  TracingConfig
    Server   ServerConfig
    Data     DataConfig
    Display  DisplayConfig

    // Runtime fields — not from ce.yaml
    ReadOnly bool   // true for read-scoped token sessions
    DataDir  string // resolved absolute path to ~/.ce or override
}

type ProjectConfig struct {
    GitURL     string `mapstructure:"git_url"`
    BasePrompt string `mapstructure:"base_prompt"`
    ArchPrompt string `mapstructure:"arch_prompt"`
}

type LLMConfig struct {
    Provider       string            `mapstructure:"provider"`
    Models         map[string]string `mapstructure:"models"` // tier → model ID
    APIKey         string            `mapstructure:"api_key"`
    BaseURL        string            `mapstructure:"base_url"`
    TimeoutSeconds int               `mapstructure:"timeout_seconds"`
    MaxRetries     int               `mapstructure:"max_retries"`
}

type EngineConfig struct {
    MaxLoops             int     `mapstructure:"max_loops"`
    KLimit               int     `mapstructure:"k_limit"`
    ContextSafetyMargin  float64 `mapstructure:"context_safety_margin"`
    DefaultRole          string  `mapstructure:"default_role"`
}

type IndexerConfig struct {
    Include           []string `mapstructure:"include"`
    Exclude           []string `mapstructure:"exclude"`
    MaxFileSizeBytes  int      `mapstructure:"max_file_size_bytes"`
    IncludeTestFiles  bool     `mapstructure:"include_test_files"`
    WatchDebounceMS   int      `mapstructure:"watch_debounce_ms"`
}

type PluginEntry struct {
    Path   string         `mapstructure:"path"`
    Config map[string]any `mapstructure:"config"`
}

type TracingConfig struct {
    Enabled       bool `mapstructure:"enabled"`
    RetentionDays int  `mapstructure:"retention_days"`
}

type ServerConfig struct {
    Port        int      `mapstructure:"port"`
    Host        string   `mapstructure:"host"`
    MCPEnabled  bool     `mapstructure:"mcp_enabled"`
    APIEnabled  bool     `mapstructure:"api_enabled"`
    WSEnabled   bool     `mapstructure:"ws_enabled"`
    CORSOrigins []string `mapstructure:"cors_origins"`
}

type DataConfig struct {
    Dir string `mapstructure:"dir"`
}

type DisplayConfig struct {
    NoColor      bool `mapstructure:"no_color"`
    ShowCost     bool `mapstructure:"show_cost"`
    ShowThinking bool `mapstructure:"show_thinking"`
}
```

---

## 7. Config Loading

```go
// internal/config/config.go

package config

import (
    "fmt"
    "os"
    "path/filepath"

    "github.com/spf13/viper"
)

// Load reads the resolved Viper config into a Config struct.
// Called at the start of every command that needs the engine.
func Load() (*Config, error) {
    var cfg Config
    if err := viper.Unmarshal(&cfg); err != nil {
        return nil, fmt.Errorf("unmarshal config: %w", err)
    }

    // Resolve data directory
    cfg.DataDir = resolveDataDir(cfg.Data.Dir)

    // Apply defaults for zero values
    applyDefaults(&cfg)

    // Validate required fields for engine commands
    if err := validate(&cfg); err != nil {
        return nil, err
    }

    return &cfg, nil
}

func resolveDataDir(configured string) string {
    if configured != "" {
        abs, err := filepath.Abs(configured)
        if err == nil {
            return abs
        }
    }
    // CE_DATA_DIR env var (already handled by viper.AutomaticEnv,
    // but check directly as fallback)
    if env := os.Getenv("CE_DATA_DIR"); env != "" {
        return env
    }
    home, _ := os.UserHomeDir()
    return filepath.Join(home, ".ce")
}

func applyDefaults(cfg *Config) {
    if cfg.Engine.MaxLoops == 0 {
        cfg.Engine.MaxLoops = core.DefaultMaxLoops
    }
    if cfg.Engine.KLimit == 0 {
        cfg.Engine.KLimit = core.DefaultKLimit
    }
    if cfg.Engine.ContextSafetyMargin == 0 {
        cfg.Engine.ContextSafetyMargin = core.ContextWindowSafetyMargin
    }
    if cfg.LLM.TimeoutSeconds == 0 {
        cfg.LLM.TimeoutSeconds = 120
    }
    if cfg.LLM.MaxRetries == 0 {
        cfg.LLM.MaxRetries = 3
    }
    if cfg.Server.Port == 0 {
        cfg.Server.Port = 8765
    }
    if cfg.Server.Host == "" {
        cfg.Server.Host = "127.0.0.1"
    }
    if cfg.Indexer.MaxFileSizeBytes == 0 {
        cfg.Indexer.MaxFileSizeBytes = 524288
    }
    if cfg.Indexer.WatchDebounceMS == 0 {
        cfg.Indexer.WatchDebounceMS = 500
    }
    if len(cfg.LLM.Models) == 0 {
        cfg.LLM.Models = map[string]string{
            "fast":     "claude-haiku-4-5-20251001",
            "standard": "claude-sonnet-4-6",
            "thinking": "claude-opus-4-6",
        }
    }
    // Default indexer excludes
    if len(cfg.Indexer.Exclude) == 0 {
        cfg.Indexer.Exclude = []string{
            "vendor/**", "node_modules/**", ".git/**",
            "**/*.pb.go", "**/*_gen.go", "dist/**", "build/**",
        }
    }
}
```

---

## 8. Configuration Hierarchy

Values are resolved in this order. Later sources override earlier ones.

```
1. Hard-coded defaults (applyDefaults)
        ↓ overridden by
2. ~/.ce/config.yaml (global user config)
        ↓ overridden by
3. ./ce.yaml (project config — current directory)
        ↓ overridden by
4. Environment variables (CE_* prefix)
        ↓ overridden by
5. CLI flags (--flag or -f)
        ↓ overridden by
6. Session overrides (read-only from token scope — applied in pre-flight)
```

**Key environment variables:**

| Variable | Maps to | Notes |
|----------|---------|-------|
| `CE_DATA_DIR` | `data.dir` | Override data directory |
| `CE_TOKEN` | `token` | API token for remote access |
| `CE_DEBUG` | `debug` | Enable debug output |
| `CE_LLM_PROVIDER` | `llm.provider` | Override LLM provider |
| `CE_LLM_API_KEY` | `llm.api_key` | API key (prefer over ce.yaml) |
| `CE_LLM_BASE_URL` | `llm.base_url` | Provider base URL override |
| `ANTHROPIC_API_KEY` | checked by anthropic provider | Standard Anthropic env var |
| `OPENAI_API_KEY` | checked by openai provider | Standard OpenAI env var |

`ANTHROPIC_API_KEY` and `OPENAI_API_KEY` are checked directly by the LLM
provider implementations (not via Viper). This means developers who already
have these set in their environment don't need to configure anything.

---

## 9. First-Run Experience

When `ce` is invoked with no config file and no `~/.ce` directory, the engine
detects a first run and guides the user.

```go
// internal/config/validate.go

func validate(cfg *Config) error {
    // Check for first-run condition
    if _, err := os.Stat(cfg.DataDir); os.IsNotExist(err) {
        return &FirstRunError{DataDir: cfg.DataDir}
    }
    return nil
}

// FirstRunError signals that CE has not been initialized.
type FirstRunError struct {
    DataDir string
}

func (e *FirstRunError) Error() string {
    return fmt.Sprintf("CE data directory not found: %s", e.DataDir)
}
```

```go
// cli/root.go (error handler)

func Execute() error {
    err := rootCmd.Execute()
    if err == nil {
        return nil
    }

    var firstRun *config.FirstRunError
    if errors.As(err, &firstRun) {
        printFirstRunGuide()
        return err
    }

    // Standard error display
    fmt.Fprintf(os.Stderr, "Error: %v\n", err)
    return err
}

func printFirstRunGuide() {
    fmt.Println(`
Context Engine — First Run

CE hasn't been set up yet. To get started:

  1. Navigate to your project directory:
       cd /path/to/your/project

  2. Initialize Context Engine:
       ce project init

  3. Index your codebase:
       ce index

  4. Start querying:
       ce query "how does X work?"

For more information: https://docs.atheory.ai/ce/quickstart
`)
}
```

### First-run flow when `ce project init` is run in a non-git directory:

```
$ ce project init
Error: not a git repository (or no remote configured)

Context Engine uses git remote URL to identify projects.

To fix this:
  1. Initialize a git repo:    git init && git remote add origin <url>
  2. Or navigate to an existing git repository

CE works with any git repository that has a remote configured.
```

### First-run flow when API key is missing:

```
$ ce query "how does billing work?"
Error: LLM provider 'anthropic' requires an API key.

Set it via environment variable (recommended):
  export ANTHROPIC_API_KEY=your-key-here

Or via ce.yaml (not recommended for shared repos):
  llm:
    api_key: your-key-here

Get an Anthropic API key at: https://console.anthropic.com
```

---

## 10. Shell Completion

```go
// cli/completion.go

func newCompletionCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "completion",
        Short: "Generate shell completion scripts",
    }

    cmd.AddCommand(
        &cobra.Command{
            Use:   "bash",
            Short: "Generate bash completion script",
            RunE: func(cmd *cobra.Command, args []string) error {
                return rootCmd.GenBashCompletion(os.Stdout)
            },
        },
        &cobra.Command{
            Use:   "zsh",
            Short: "Generate zsh completion script",
            RunE: func(cmd *cobra.Command, args []string) error {
                return rootCmd.GenZshCompletion(os.Stdout)
            },
        },
        &cobra.Command{
            Use:   "fish",
            Short: "Generate fish completion script",
            RunE: func(cmd *cobra.Command, args []string) error {
                return rootCmd.GenFishCompletion(os.Stdout, true)
            },
        },
        &cobra.Command{
            Use:   "powershell",
            Short: "Generate PowerShell completion script",
            RunE: func(cmd *cobra.Command, args []string) error {
                return rootCmd.GenPowerShellCompletionWithDesc(os.Stdout)
            },
        },
    )

    return cmd
}
```

Installation instructions printed by `ce completion --help`:

```
To load completions:

  Bash:
    source <(ce completion bash)
    # To persist: ce completion bash > /etc/bash_completion.d/ce

  Zsh:
    echo "autoload -U compinit; compinit" >> ~/.zshrc
    ce completion zsh > "${fpath[1]}/_ce"

  Fish:
    ce completion fish | source
    # To persist: ce completion fish > ~/.config/fish/completions/ce.fish
```

---

## 11. CLI Package Layout

```
cli/
  root.go           — rootCmd, Execute(), initConfig(), error handler
  query.go          — ce query
  renderer.go       — CLI channel renderer
  index.go          — ce index
  project.go        — ce project {init|list|status|set|remove}
  token.go          — ce token {create|list|revoke}
  plugin.go         — ce plugin {build|dev|validate|install|list|remove}
  config.go         — ce config {show|get|set}
  server.go         — ce server {start|stop|status}
  cache.go          — ce cache {show|clear}
  version.go        — ce version
  completion.go     — ce completion {bash|zsh|fish|powershell}
  prompt_helpers.go — promptString(), promptMultiline(), promptChoice(),
                      promptYesNo() — interactive input utilities
  errors.go         — printFirstRunGuide(), error formatting
```

```
internal/config/
  config.go         — Load(), resolveDataDir(), applyDefaults()
  schema.go         — Config struct and all sub-structs
  validate.go       — validate(), FirstRunError
```

---

## 12. Key Decisions Captured in This Spec

| Decision | Value |
|----------|-------|
| CLI framework | Cobra |
| Config framework | Viper |
| Config filename | `ce.yaml` |
| Config search path | `./ce.yaml` then `~/.ce/config.yaml` |
| Config format | YAML (Viper also accepts JSON/TOML transparently) |
| Environment prefix | `CE_` |
| Provider API keys | Checked directly (`ANTHROPIC_API_KEY`) — not only via CE_ prefix |
| Data directory | `~/.ce` default, `CE_DATA_DIR` override |
| Config hierarchy | defaults → global → project → env → flags → session |
| First-run detection | `~/.ce` directory absence |
| TUI vs CLI mode | No text args = TUI; text args = CLI; `--tui` forces TUI |
| Sensitive value masking | `ce config show` masks api_key, token, secret, password fields |
| Error verbosity | `SilenceUsage: true` — no usage dump on every error |

---

*Spec 6: CLI / Config — v1.0 — February 2026*
*All six Phase 1 specs complete.*
*Companion: Context Engine PRD v0.5 Section 16 | Decisions Log v1.0 Section 8*
