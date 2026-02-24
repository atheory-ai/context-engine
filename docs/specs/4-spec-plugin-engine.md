# Context Engine — Spec 4: Plugin System (Engine Side)
## Implementation Spec — wazero Runtime, Extism Host, Plugin Lifecycle
### Version 1.0 | February 2026

---

> This is an implementation spec, not a PRD section.
> Hand this document to Claude Code alongside spec-data-layer.md,
> spec-packages.md, and spec-engine-runner.md.
> Companion: Context Engine PRD v0.5 Section 12. Decisions Log v1.0 Section 5.

---

## 1. Overview

The plugin system has three concerns:

1. **Runtime** (`internal/plugins/runtime/`) — loading `.wasm` files via
   wazero + Extism, exposing host functions under the `ce` namespace,
   managing the compilation cache.

2. **Registry** (`internal/plugins/registry/`) — tracking loaded plugins,
   resolving which plugins handle which files, lifecycle management.

3. **CLI pipeline** (`cli/plugin.go`) — `ce plugin build`, `ce plugin dev`,
   `ce plugin validate`, `ce plugin install` commands that make plugin
   authoring invisible to the TypeScript developer.

This spec covers 1 and 2 in full, and the CLI pipeline build/validate path.
The plugin SDK (TypeScript side) is Spec 7.

---

## 2. Dependencies

```go
// go.mod additions
require (
    github.com/extism/go-sdk v1.x.x
    github.com/tetratelabs/wazero v1.x.x
)
```

No CGO. Both are pure Go. Cross-compilation preserved.

---

## 3. WASM Interface Contract

Every plugin `.wasm` file must export exactly these functions.
The names are the contract — they never change without a major version bump.

```
Exported function       Signature (WASM i32/i64 types)
─────────────────────   ──────────────────────────────
ce_plugin_manifest      () → i32   (ptr to JSON manifest in plugin memory)
ce_language_match       (i32, i32) → i32   (ptr, len → bool)
ce_language_extract     (i32, i32, i32, i32) → i32   (path_ptr, path_len, content_ptr, content_len → ptr to JSON result)
ce_language_concepts    () → i32   (ptr to JSON concept seed array)
ce_role_definition      () → i32   (ptr to JSON role, or 0 if none)
ce_analyzers_list       () → i32   (ptr to JSON analyzer array)
ce_tools_list           () → i32   (ptr to JSON tool descriptor array)
ce_tool_activate        (i32, i32, i32) → i32   (tool_name_ptr, tool_name_len, ir_ptr → bool)
ce_tool_execute         (i32, i32, i32, i32) → i32   (tool_name_ptr, tool_name_len, req_ptr, req_len → ptr to JSON result)
ce_analyzer_run         (i32, i32, i32, i32) → i32   (analyzer_name_ptr, analyzer_name_len, nodes_ptr, nodes_len → ptr to JSON result)
```

All JSON is UTF-8. Pointers are offsets into the plugin's linear memory.
The Extism PDK handles memory management — plugin authors never see raw pointers.

**Validation rule**: A `.wasm` file that does not export `ce_plugin_manifest`
is rejected immediately. All other exports are optional — a plugin that
exports only `ce_language_match` + `ce_language_extract` is a valid
language-only plugin.

---

## 4. Host Functions — `ce` Namespace

These are Go functions exposed to plugins. Plugins can call them during
extraction, analysis, or tool execution. They are registered on the wazero
runtime before any plugin is loaded.

```go
// internal/plugins/runtime/host.go

package runtime

// registerHostFunctions registers all ce.* host functions on the wazero runtime.
// Called once during runtime initialization.
func registerHostFunctions(r wazero.Runtime, deps HostDeps) error {
    builder := r.NewHostModuleBuilder("ce")

    // ce.log(level_ptr, level_len, msg_ptr, msg_len)
    // Plugin logging. Routes to ChanDebug.
    builder.NewFunctionBuilder().
        WithFunc(hostLog(deps.Channels)).
        Export("log")

    // ce.emit(channel_ptr, channel_len, content_ptr, content_len)
    // Plugin emits to an engine channel. Restricted to: thinking, action, debug.
    // Plugins cannot emit to message or system channels.
    builder.NewFunctionBuilder().
        WithFunc(hostEmit(deps.Channels, deps.RunContext)).
        Export("emit")

    // ce.substrate_query(query_json_ptr, query_json_len) → result_json_ptr
    // Plugin queries the substrate reader. Read-only.
    // query JSON: { "project_id": "...", "node_types": [...], "limit": N }
    builder.NewFunctionBuilder().
        WithFunc(hostSubstrateQuery(deps.Substrate)).
        Export("substrate_query")

    // ce.get_config(key_ptr, key_len) → value_json_ptr
    // Plugin reads its own config values from ce.yaml plugins section.
    builder.NewFunctionBuilder().
        WithFunc(hostGetConfig(deps.PluginConfig)).
        Export("get_config")

    // ce.node_id(project_id_ptr, project_id_len, type_ptr, type_len,
    //            canonical_ptr, canonical_len) → id_ptr
    // Deterministic node ID generation. Plugins use this to produce
    // consistent IDs without reimplementing the hash algorithm.
    builder.NewFunctionBuilder().
        WithFunc(hostNodeID()).
        Export("node_id")

    // ce.edge_id(source_ptr, source_len, type_ptr, type_len,
    //            target_ptr, target_len) → id_ptr
    builder.NewFunctionBuilder().
        WithFunc(hostEdgeID()).
        Export("edge_id")

    _, err := builder.Instantiate(context.Background())
    return err
}

// HostDeps are the engine dependencies injected into host functions.
// Constructed per-query for tool execution; constructed once for indexing.
type HostDeps struct {
    Channels    *core.AppChannels
    RunContext  *runner.RunContext // nil during indexing
    Substrate   core.SubstrateReader
    PluginConfig map[string]any
}
```

### Permitted channel writes from plugins

Plugins can only write to a restricted set of channels:

```go
var pluginPermittedChannels = map[core.ChannelType]bool{
    core.ChanThinking: true,
    core.ChanAction:   true,
    core.ChanDebug:    true,
    core.ChanWarning:  true,
}

func hostEmit(ch *core.AppChannels, rc *runner.RunContext) func(ctx context.Context,
    m api.Module, channelPtr, channelLen, contentPtr, contentLen uint32) {
    return func(ctx context.Context, m api.Module, channelPtr, channelLen, contentPtr, contentLen uint32) {
        channel := core.ChannelType(readString(m, channelPtr, channelLen))
        if !pluginPermittedChannels[channel] {
            return // silently drop — plugins cannot hijack message channel
        }
        content := readString(m, contentPtr, contentLen)
        ch.Emit(core.Emission{
            Channel: channel,
            Content: content,
        })
    }
}
```

---

## 5. Compilation Cache

Compiled WASM artifacts are cached to disk so subsequent loads are near-instant.

### Cache Layout

```
~/.ce/cache/
  plugins/
    <sha256-of-wasm-content>/
      native.bin        — wazero compiled artifact
      meta.json         — { wasm_hash, plugin_name, version, cached_at, last_used }
```

The cache key is the SHA-256 of the raw `.wasm` file content.
Filename and path are irrelevant — same bytes = same cache entry.

### Cache Implementation

```go
// internal/plugins/runtime/cache.go

package runtime

import (
    "crypto/sha256"
    "encoding/hex"
    "encoding/json"
    "os"
    "path/filepath"
    "time"
)

const cacheTTLDays = 30

type CompilationCache struct {
    baseDir string // ~/.ce/cache/plugins/
}

func NewCompilationCache(ceDataDir string) *CompilationCache {
    return &CompilationCache{
        baseDir: filepath.Join(ceDataDir, "cache", "plugins"),
    }
}

type CacheMeta struct {
    WASMHash   string `json:"wasm_hash"`
    PluginName string `json:"plugin_name"`
    Version    string `json:"version"`
    CachedAt   int64  `json:"cached_at"`
    LastUsed   int64  `json:"last_used"`
}

// WASMHash computes the cache key for a .wasm file.
func WASMHash(wasmBytes []byte) string {
    h := sha256.Sum256(wasmBytes)
    return hex.EncodeToString(h[:])
}

// CacheDir returns the directory for a given wasm hash.
func (c *CompilationCache) CacheDir(wasmHash string) string {
    return filepath.Join(c.baseDir, wasmHash)
}

// IsCached returns true if a compiled artifact exists for this hash.
func (c *CompilationCache) IsCached(wasmHash string) bool {
    _, err := os.Stat(filepath.Join(c.CacheDir(wasmHash), "native.bin"))
    return err == nil
}

// TouchLastUsed updates the last_used timestamp in meta.json.
// Called on every cache hit to enable TTL-based eviction.
func (c *CompilationCache) TouchLastUsed(wasmHash string) {
    metaPath := filepath.Join(c.CacheDir(wasmHash), "meta.json")
    data, err := os.ReadFile(metaPath)
    if err != nil {
        return
    }
    var meta CacheMeta
    if err := json.Unmarshal(data, &meta); err != nil {
        return
    }
    meta.LastUsed = time.Now().UnixMilli()
    updated, _ := json.Marshal(meta)
    _ = os.WriteFile(metaPath, updated, 0644)
}

// Evict removes cache entries not used within the TTL.
// Called at startup. Non-blocking — runs in a goroutine.
func (c *CompilationCache) Evict() {
    go func() {
        entries, err := os.ReadDir(c.baseDir)
        if err != nil {
            return
        }
        cutoff := time.Now().AddDate(0, 0, -cacheTTLDays).UnixMilli()
        for _, entry := range entries {
            if !entry.IsDir() {
                continue
            }
            metaPath := filepath.Join(c.baseDir, entry.Name(), "meta.json")
            data, err := os.ReadFile(metaPath)
            if err != nil {
                continue
            }
            var meta CacheMeta
            if err := json.Unmarshal(data, &meta); err != nil {
                continue
            }
            if meta.LastUsed < cutoff {
                _ = os.RemoveAll(filepath.Join(c.baseDir, entry.Name()))
            }
        }
    }()
}
```

### wazero Cache Integration

```go
// internal/plugins/runtime/runtime.go (cache wiring)

func (r *Runtime) initWazero(cache *CompilationCache) error {
    // wazero accepts a compilationcache.Cache to persist compiled modules.
    // We point it at our cache directory.
    wazeroCache, err := compilationcache.NewFileCache(cache.baseDir)
    if err != nil {
        return fmt.Errorf("wazero cache: %w", err)
    }

    r.wazero = wazero.NewRuntimeWithConfig(
        context.Background(),
        wazero.NewRuntimeConfig().WithCompilationCache(wazeroCache),
    )
    return nil
}
```

---

## 6. Plugin Loading Sequence

This is the full lifecycle from `.wasm` file on disk to a ready `core.Plugin`.

```go
// internal/plugins/runtime/load.go

package runtime

// Load reads a .wasm file, validates it, compiles it (or hits cache),
// and returns a ready Plugin instance.
func (r *Runtime) Load(ctx context.Context, wasmPath string) (core.Plugin, error) {

    // ── 1. Read file ───────────────────────────────────────────────────────
    wasmBytes, err := os.ReadFile(wasmPath)
    if err != nil {
        return nil, fmt.Errorf("read wasm %s: %w", wasmPath, err)
    }

    wasmHash := WASMHash(wasmBytes)

    // ── 2. Validate exports ────────────────────────────────────────────────
    if err := validateExports(wasmBytes); err != nil {
        return nil, fmt.Errorf("validate %s: %w", wasmPath, err)
    }

    // ── 3. Check compilation cache ─────────────────────────────────────────
    if r.cache.IsCached(wasmHash) {
        r.cache.TouchLastUsed(wasmHash)
        // wazero loads from cache transparently — no special path needed,
        // the compilationcache.FileCache handles it
    }

    // ── 4. Create Extism plugin instance ──────────────────────────────────
    manifest := extism.Manifest{
        Wasm: []extism.Wasm{
            extism.WasmData{Data: wasmBytes},
        },
        // Sandbox: no filesystem access, no network access
        AllowedHosts: []string{},
        AllowedPaths: map[string]string{},
    }

    extismPlugin, err := extism.NewPlugin(ctx, manifest, extism.PluginConfig{
        EnableWasi: false, // no WASI syscalls
    }, r.hostFunctions)
    if err != nil {
        return nil, fmt.Errorf("create extism plugin %s: %w", wasmPath, err)
    }

    // ── 5. Call ce_plugin_manifest to read plugin metadata ────────────────
    _, manifestJSON, err := extismPlugin.Call("ce_plugin_manifest", nil)
    if err != nil {
        extismPlugin.Close(ctx)
        return nil, fmt.Errorf("call ce_plugin_manifest: %w", err)
    }

    var pmeta PluginManifest
    if err := json.Unmarshal(manifestJSON, &pmeta); err != nil {
        extismPlugin.Close(ctx)
        return nil, fmt.Errorf("parse plugin manifest: %w", err)
    }

    // ── 6. Write cache meta (first load only) ─────────────────────────────
    if !r.cache.IsCached(wasmHash) {
        r.cache.writeMeta(wasmHash, CacheMeta{
            WASMHash:   wasmHash,
            PluginName: pmeta.Name,
            Version:    pmeta.Version,
            CachedAt:   time.Now().UnixMilli(),
            LastUsed:   time.Now().UnixMilli(),
        })
    }

    // ── 7. Wrap in Plugin instance ─────────────────────────────────────────
    return &pluginInstance{
        id:       core.PluginID(pmeta.ID),
        name:     pmeta.Name,
        version:  pmeta.Version,
        wasm:     extismPlugin,
        manifest: pmeta,
        runtime:  r,
    }, nil
}
```

### Plugin Manifest JSON Schema

`ce_plugin_manifest` returns this JSON:

```json
{
  "id":      "com.atheory.go-language",
  "name":    "Go Language Plugin",
  "version": "1.0.0",
  "capabilities": {
    "language":  true,
    "role":      false,
    "analyzers": ["interface-graph"],
    "tools":     ["go-test-runner"]
  }
}
```

`capabilities` tells the engine what to call — if `language` is false,
`ce_language_match` is never called. If `tools` is empty, `ce_tools_list`
is never called. This avoids unnecessary WASM round-trips.

---

## 7. Plugin Instance — core.Plugin Implementation

```go
// internal/plugins/runtime/instance.go

package runtime

// pluginInstance wraps an Extism plugin and implements core.Plugin.
type pluginInstance struct {
    id       core.PluginID
    name     string
    version  string
    wasm     *extism.Plugin
    manifest PluginManifest
    runtime  *Runtime
    mu       sync.Mutex // extism plugins are not goroutine-safe — serialize calls
}

func (p *pluginInstance) ID() core.PluginID { return p.id }
func (p *pluginInstance) Name() string      { return p.name }
func (p *pluginInstance) Version() string   { return p.version }

func (p *pluginInstance) Language() core.LanguageHandler {
    if !p.manifest.Capabilities.Language {
        return nil
    }
    return &wasmLanguageHandler{plugin: p}
}

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

func (p *pluginInstance) Close() error {
    return p.wasm.Close(context.Background())
}
```

### WASM Language Handler

```go
// wasmLanguageHandler implements core.LanguageHandler via WASM calls.
type wasmLanguageHandler struct {
    plugin *pluginInstance
}

func (h *wasmLanguageHandler) Match(filePath string) bool {
    h.plugin.mu.Lock()
    defer h.plugin.mu.Unlock()

    _, result, err := h.plugin.wasm.Call("ce_language_match", []byte(filePath))
    if err != nil {
        return false
    }
    return len(result) > 0 && result[0] == 1
}

func (h *wasmLanguageHandler) Extract(filePath string, content []byte) (core.ExtractionResult, error) {
    h.plugin.mu.Lock()
    defer h.plugin.mu.Unlock()

    // Pack filePath + content as JSON input
    input, _ := json.Marshal(map[string]any{
        "file_path": filePath,
        "content":   string(content),
    })

    _, result, err := h.plugin.wasm.Call("ce_language_extract", input)
    if err != nil {
        return core.ExtractionResult{}, fmt.Errorf("ce_language_extract: %w", err)
    }

    var out core.ExtractionResult
    if err := json.Unmarshal(result, &out); err != nil {
        return core.ExtractionResult{}, fmt.Errorf("parse extraction result: %w", err)
    }
    return out, nil
}

func (h *wasmLanguageHandler) Concepts() []core.ConceptSeed {
    h.plugin.mu.Lock()
    defer h.plugin.mu.Unlock()

    _, result, err := h.plugin.wasm.Call("ce_language_concepts", nil)
    if err != nil {
        return nil
    }
    var seeds []core.ConceptSeed
    json.Unmarshal(result, &seeds)
    return seeds
}
```

### WASM Tool

```go
// wasmTool implements core.Tool via WASM calls.
type wasmTool struct {
    plugin     *pluginInstance
    descriptor ToolDescriptor
}

type ToolDescriptor struct {
    Name        string `json:"name"`
    Description string `json:"description"` // max 100 chars
}

func (t *wasmTool) Name() string        { return t.descriptor.Name }
func (t *wasmTool) Description() string { return t.descriptor.Description }

func (t *wasmTool) Activate(ir core.IR) bool {
    t.plugin.mu.Lock()
    defer t.plugin.mu.Unlock()

    input, _ := json.Marshal(map[string]any{
        "tool_name": t.descriptor.Name,
        "ir":        ir,
    })

    _, result, err := t.plugin.wasm.Call("ce_tool_activate", input)
    if err != nil {
        return false
    }
    return len(result) > 0 && result[0] == 1
}

func (t *wasmTool) Execute(ctx context.Context, req core.ToolRequest) (core.ToolResult, error) {
    t.plugin.mu.Lock()
    defer t.plugin.mu.Unlock()

    input, _ := json.Marshal(map[string]any{
        "tool_name": t.descriptor.Name,
        "request":   req,
    })

    _, result, err := t.plugin.wasm.Call("ce_tool_execute", input)
    if err != nil {
        return core.ToolResult{}, fmt.Errorf("ce_tool_execute %s: %w", t.descriptor.Name, err)
    }

    var out core.ToolResult
    if err := json.Unmarshal(result, &out); err != nil {
        return core.ToolResult{}, fmt.Errorf("parse tool result: %w", err)
    }
    return out, nil
}
```

---

## 8. Plugin Registry

```go
// internal/plugins/registry/registry.go

package registry

// Registry manages all loaded plugins for the engine lifetime.
type Registry struct {
    mu      sync.RWMutex
    plugins map[core.PluginID]core.Plugin
    runtime *runtime.Runtime
}

func NewRegistry(rt *runtime.Runtime) *Registry {
    return &Registry{
        plugins: make(map[core.PluginID]core.Plugin),
        runtime: rt,
    }
}

// LoadAll loads all plugins specified in the config.
// Plugins are loaded in the order listed. Duplicate IDs are rejected.
func (r *Registry) LoadAll(ctx context.Context, pluginPaths []string) error {
    for _, path := range pluginPaths {
        if err := r.Load(ctx, path); err != nil {
            return fmt.Errorf("load plugin %s: %w", path, err)
        }
    }
    return nil
}

func (r *Registry) Load(ctx context.Context, wasmPath string) error {
    plugin, err := r.runtime.Load(ctx, wasmPath)
    if err != nil {
        return err
    }

    r.mu.Lock()
    defer r.mu.Unlock()

    if _, exists := r.plugins[plugin.ID()]; exists {
        plugin.Close()
        return fmt.Errorf("plugin %s already loaded", plugin.ID())
    }

    r.plugins[plugin.ID()] = plugin
    return nil
}

// Loaded returns all currently loaded plugins.
func (r *Registry) Loaded() []core.Plugin {
    r.mu.RLock()
    defer r.mu.RUnlock()
    out := make([]core.Plugin, 0, len(r.plugins))
    for _, p := range r.plugins {
        out = append(out, p)
    }
    return out
}

// LanguageHandlers returns all language handlers from all loaded plugins.
// Called by the indexer to route files to the correct parser.
func (r *Registry) LanguageHandlers() []core.LanguageHandler {
    r.mu.RLock()
    defer r.mu.RUnlock()
    var handlers []core.LanguageHandler
    for _, p := range r.plugins {
        if h := p.Language(); h != nil {
            handlers = append(handlers, h)
        }
    }
    return handlers
}

// UnloadAll closes all plugins and clears the registry.
func (r *Registry) UnloadAll() {
    r.mu.Lock()
    defer r.mu.Unlock()
    for id, p := range r.plugins {
        p.Close()
        delete(r.plugins, id)
    }
}
```

---

## 9. WASM Export Validation

Called during `Load()` and during `ce plugin validate`. Inspects the WASM
binary's export section without executing it.

```go
// internal/plugins/runtime/validate.go

package runtime

// validateExports checks that a .wasm file exports the required functions.
// Uses wazero's module inspection — does not execute the module.
func validateExports(wasmBytes []byte) error {
    // Compile to inspect exports (uses wazero's module compilation)
    rt := wazero.NewRuntime(context.Background())
    defer rt.Close(context.Background())

    compiled, err := rt.CompileModule(context.Background(), wasmBytes)
    if err != nil {
        return fmt.Errorf("invalid wasm: %w", err)
    }
    defer compiled.Close(context.Background())

    exports := compiled.ExportedFunctions()

    // ce_plugin_manifest is the only unconditionally required export
    if _, ok := exports["ce_plugin_manifest"]; !ok {
        return fmt.Errorf("missing required export: ce_plugin_manifest")
    }

    // Validate that any present exports have the correct signatures
    signatureChecks := map[string]expectedSig{
        "ce_plugin_manifest":  {params: []api.ValueType{}, results: []api.ValueType{api.ValueTypeI32}},
        "ce_language_match":   {params: []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, results: []api.ValueType{api.ValueTypeI32}},
        "ce_language_extract": {params: []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, results: []api.ValueType{api.ValueTypeI32}},
        "ce_tool_activate":    {params: []api.ValueType{api.ValueTypeI32}, results: []api.ValueType{api.ValueTypeI32}},
        "ce_tool_execute":     {params: []api.ValueType{api.ValueTypeI32}, results: []api.ValueType{api.ValueTypeI32}},
    }

    for name, expected := range signatureChecks {
        fn, ok := exports[name]
        if !ok {
            continue // optional export absent — fine
        }
        if err := checkSignature(fn, expected); err != nil {
            return fmt.Errorf("export %s: %w", name, err)
        }
    }

    return nil
}
```

---

## 10. CLI Pipeline — Build and Validate

### `ce plugin build`

Compiles a TypeScript/JavaScript plugin source to `.wasm`.
The developer never sees esbuild, Javy, or Extism.

```go
// cli/plugin.go (build subcommand handler)

func runPluginBuild(cmd *cobra.Command, args []string) error {
    sourcePath := "." // default: current directory
    if len(args) > 0 {
        sourcePath = args[0]
    }

    // ── 1. Detect source type ──────────────────────────────────────────────
    buildPlan, err := detectBuildPlan(sourcePath)
    if err != nil {
        return err
    }

    fmt.Fprintf(os.Stderr, "Building %s (v%s)...\n",
        buildPlan.PluginName, buildPlan.Version)

    // ── 2. Bundle TypeScript → single JS (esbuild, embedded) ──────────────
    bundledJS, err := bundleTypeScript(buildPlan)
    if err != nil {
        return fmt.Errorf("bundle: %w", err)
    }

    // ── 3. Compile JS → WASM (Javy, embedded) ─────────────────────────────
    wasmBytes, err := compileWithJavy(bundledJS)
    if err != nil {
        return fmt.Errorf("compile: %w", err)
    }

    // ── 4. Validate exports ────────────────────────────────────────────────
    if err := runtime.validateExports(wasmBytes); err != nil {
        return fmt.Errorf("validation failed: %w", err)
    }

    // ── 5. Write output ────────────────────────────────────────────────────
    outPath := buildPlan.OutputPath
    if err := os.WriteFile(outPath, wasmBytes, 0644); err != nil {
        return fmt.Errorf("write %s: %w", outPath, err)
    }

    fmt.Fprintf(os.Stderr, "✓ %s\n", outPath)
    return nil
}
```

### Build Plan Detection

```go
type BuildPlan struct {
    SourceType  string   // "single-file" | "project"
    EntryPoint  string   // path to entry .ts file
    OutputPath  string   // where to write .wasm
    PluginName  string
    Version     string
}

func detectBuildPlan(sourcePath string) (*BuildPlan, error) {
    info, err := os.Stat(sourcePath)
    if err != nil {
        return nil, err
    }

    // Single file: ce plugin build my-plugin.ts
    if !info.IsDir() && (strings.HasSuffix(sourcePath, ".ts") ||
                          strings.HasSuffix(sourcePath, ".js")) {
        name := strings.TrimSuffix(filepath.Base(sourcePath), filepath.Ext(sourcePath))
        return &BuildPlan{
            SourceType: "single-file",
            EntryPoint: sourcePath,
            OutputPath: name + ".wasm",
            PluginName: name,
            Version:    "0.0.0",
        }, nil
    }

    // Project: read package.json + ce-plugin.json
    if info.IsDir() {
        return detectProjectBuildPlan(sourcePath)
    }

    return nil, fmt.Errorf("unsupported source: %s", sourcePath)
}

func detectProjectBuildPlan(dir string) (*BuildPlan, error) {
    // Try ce-plugin.json first
    cePluginPath := filepath.Join(dir, "ce-plugin.json")
    if _, err := os.Stat(cePluginPath); err == nil {
        return parseCEPluginManifest(cePluginPath, dir)
    }

    // Fall back to package.json contextEngine field
    pkgPath := filepath.Join(dir, "package.json")
    if _, err := os.Stat(pkgPath); err == nil {
        return parsePackageJSON(pkgPath, dir)
    }

    return nil, fmt.Errorf("no ce-plugin.json or package.json found in %s", dir)
}
```

### `ce plugin validate`

```go
func runPluginValidate(cmd *cobra.Command, args []string) error {
    if len(args) == 0 {
        return fmt.Errorf("usage: ce plugin validate <file.wasm>")
    }

    wasmBytes, err := os.ReadFile(args[0])
    if err != nil {
        return err
    }

    // ── 1. Export validation ───────────────────────────────────────────────
    if err := runtime.validateExports(wasmBytes); err != nil {
        fmt.Fprintf(os.Stderr, "✗ Export validation failed: %v\n", err)
        os.Exit(1)
    }
    fmt.Fprintln(os.Stderr, "✓ Exports valid")

    // ── 2. Load and call manifest ──────────────────────────────────────────
    rt := runtime.New(cfg.DataDir)
    plugin, err := rt.Load(context.Background(), args[0])
    if err != nil {
        fmt.Fprintf(os.Stderr, "✗ Load failed: %v\n", err)
        os.Exit(1)
    }
    defer plugin.Close()

    fmt.Fprintf(os.Stderr, "✓ Loaded: %s v%s\n", plugin.Name(), plugin.Version())

    // ── 3. Capability smoke test ───────────────────────────────────────────
    if h := plugin.Language(); h != nil {
        fmt.Fprintln(os.Stderr, "✓ Language handler present")
        seeds := h.Concepts()
        fmt.Fprintf(os.Stderr, "  %d concept seeds\n", len(seeds))
    }
    if tools := plugin.Tools(); len(tools) > 0 {
        fmt.Fprintf(os.Stderr, "✓ %d tool(s): ", len(tools))
        names := make([]string, len(tools))
        for i, t := range tools {
            names[i] = t.Name()
        }
        fmt.Fprintln(os.Stderr, strings.Join(names, ", "))
    }
    if analyzers := plugin.Analyzers(); len(analyzers) > 0 {
        fmt.Fprintf(os.Stderr, "✓ %d analyzer(s)\n", len(analyzers))
    }

    fmt.Fprintln(os.Stderr, "✓ Validation passed")
    return nil
}
```

### `ce plugin dev` — Live Development Loop

```go
func runPluginDev(cmd *cobra.Command, args []string) error {
    sourcePath := "."
    if len(args) > 0 {
        sourcePath = args[0]
    }

    buildPlan, err := detectBuildPlan(sourcePath)
    if err != nil {
        return err
    }

    fmt.Fprintf(os.Stderr, "Watching %s for changes...\n", buildPlan.EntryPoint)

    watcher, err := fsnotify.NewWatcher()
    if err != nil {
        return err
    }
    defer watcher.Close()

    // Watch the source directory
    watchDir := sourcePath
    if !strings.HasSuffix(sourcePath, "/") {
        watchDir = filepath.Dir(sourcePath)
    }
    watcher.Add(watchDir)

    // Initial build
    runDevBuild(buildPlan)

    for {
        select {
        case event := <-watcher.Events:
            if event.Op&(fsnotify.Write|fsnotify.Create) != 0 {
                if isSourceFile(event.Name) {
                    runDevBuild(buildPlan)
                }
            }
        case err := <-watcher.Errors:
            fmt.Fprintf(os.Stderr, "watcher error: %v\n", err)
        }
    }
}

func runDevBuild(plan *BuildPlan) {
    start := time.Now()
    fmt.Fprintf(os.Stderr, "\n[%s] Rebuilding...\n",
        time.Now().Format("15:04:05"))

    wasmBytes, err := buildToWASM(plan)
    if err != nil {
        fmt.Fprintf(os.Stderr, "✗ Build failed: %v\n", err)
        return
    }

    if err := runtime.validateExports(wasmBytes); err != nil {
        fmt.Fprintf(os.Stderr, "✗ Validation failed: %v\n", err)
        return
    }

    // Write .wasm
    os.WriteFile(plan.OutputPath, wasmBytes, 0644)

    // Coverage analysis against fixtures
    report := analyzeCoverage(plan, wasmBytes)
    printCoverageReport(report)

    fmt.Fprintf(os.Stderr, "✓ Built in %dms\n", time.Since(start).Milliseconds())
}
```

### Sandbox Coverage Report JSON Schema

This is the format CE Studio will consume for the plugin development view.
The sandbox CLI writes this to stdout with `--json` flag; the TUI renders it inline.

```json
{
  "schema_version": 1,
  "plugin_name": "go-language",
  "plugin_version": "1.0.0",
  "wasm_hash": "abc123...",
  "built_at": 1709000000000,
  "fixture_results": [
    {
      "fixture_file": "testdata/main.go",
      "ast_symbols": 24,
      "extracted_nodes": 18,
      "coverage_pct": 75.0,
      "missing_symbols": ["init", "TestMain", "BenchmarkFoo"],
      "extra_nodes": []
    }
  ],
  "aggregate": {
    "total_ast_symbols": 48,
    "total_extracted_nodes": 36,
    "coverage_pct": 75.0
  },
  "validation": {
    "passed": true,
    "errors": [],
    "warnings": ["coverage below 80% threshold"]
  }
}
```

---

## 11. ce.yaml Plugin Configuration

Plugins are registered in `ce.yaml`. Path is relative to the project root
or absolute. The engine loads plugins in list order at startup.

```yaml
plugins:
  - path: ~/.ce/plugins/go-language.wasm
    config:
      # Plugin-specific config — available via ce.get_config() host function
      include_test_files: true
      max_file_size_kb: 512

  - path: ./plugins/my-custom-plugin.wasm
    config: {}
```

---

## 12. Runtime Package Layout

```
internal/plugins/
  runtime/
    runtime.go      — Runtime struct, initialization, wazero + Extism setup
    load.go         — Load(): file → validated Plugin instance
    validate.go     — validateExports(): WASM binary inspection
    host.go         — registerHostFunctions(): ce.* namespace
    cache.go        — CompilationCache: ~/.ce/cache/plugins/
    instance.go     — pluginInstance: core.Plugin implementation
    wasm_language.go — wasmLanguageHandler: core.LanguageHandler via WASM
    wasm_tool.go    — wasmTool: core.Tool via WASM
    wasm_analyzer.go — wasmAnalyzer: core.Analyzer via WASM
    memory.go       — readString(), writeString(): WASM memory helpers
  registry/
    registry.go     — Registry: load, lookup, lifecycle
```

---

## 13. Key Decisions Captured in This Spec

| Decision | Value |
|----------|-------|
| WASM runtime | wazero (pure Go, no CGO) |
| Plugin framework | Extism |
| Host function namespace | `ce` |
| Cache location | `~/.ce/cache/plugins/<wasm-hash>/` |
| Cache key | SHA-256 of `.wasm` file content |
| Cache invalidation | Content-hash based (filename irrelevant) |
| Cache TTL | 30 days since last use |
| Required WASM export | `ce_plugin_manifest` only |
| Sandbox isolation | No WASI, no filesystem, no network |
| Plugin-to-channel writes | Restricted: thinking, action, debug, warning only |
| Extism concurrency | Serialized per instance (mutex) |
| Build plan detection | `ce-plugin.json` first, `package.json` contextEngine fallback |
| Sandbox coverage output | JSON schema v1 — contract for future CE Studio plugin view |

---

*Spec 4: Plugin System (Engine Side) — v1.0 — February 2026*
*Next: Spec 5 — Strategizer Prompt*
*Companion: Context Engine PRD v0.5 Section 12 | Decisions Log v1.0 Section 5*
