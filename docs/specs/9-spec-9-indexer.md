# Context Engine — Spec 9: Indexer
## Implementation Spec — File Walker, Tree-sitter, Grammar Registration, Plugin Extraction
### Version 1.0 | February 2026

---

> **Historical/superseded parsing design.** The CGO clauses below are retained
> for decision history only. [Spec 18](18-spec-wasm-grammar-loader.md) is the
> current authority: Context Engine is pure Go (`CGO_ENABLED=0`) and tree-sitter
> runs as WASM on wazero.

> This spec amends prior specs where noted. Amendments are authoritative
> over the original spec text. Read the amendments section first.
> Companion: Context Engine PRD v0.5 Section 13. Decisions Log v1.0.
> Hand to Claude Code alongside spec-2-packages.md and spec-1-data-layer.md.

---

## Amendments to Prior Specs

These decisions supersede or extend prior specs. Claude Code must treat
these as authoritative over the original spec text where they conflict.

### Amends Spec 4 (Plugin Engine) — Plugin Manifest

`ce_plugin_manifest` now returns an optional `grammar` field:

```json
{
  "id":      "com.atheory.go-language",
  "name":    "Go Language Plugin",
  "version": "1.2.0",
  "capabilities": {
    "language":  true,
    "role":      false,
    "analyzers": [],
    "tools":     []
  },
  "language": {
    "extensions":   [".go"],
    "grammar":      "./go-grammar.wasm"
  }
}
```

`language.extensions` — file extensions this plugin handles.
`language.grammar` — path to tree-sitter grammar WASM file, relative to
the plugin `.wasm` file. Optional. If absent, engine provides built-in
grammar or passes `tree: null` to extract().

### Amends Spec 4 (Plugin Engine) — Loading Sequence

Step 6 in the loading sequence (after manifest is read) adds:

```
6b. If manifest.language.grammar is set:
    - Resolve grammar path relative to plugin .wasm location
    - Load grammar.wasm via tree-sitter WASM runtime
    - Register grammar against plugin's declared extensions
    - Store in grammar registry keyed by extension
```

### Amends Spec 7 (Plugin SDK) — LanguageDefinition

```typescript
// REPLACES the existing LanguageDefinition interface

export interface LanguageDefinition {
  /**
   * File extensions this plugin handles.
   * Example: [".go"], [".ts", ".tsx", ".js", ".jsx"]
   */
  extensions: string[]

  /**
   * Path to tree-sitter grammar WASM file.
   * Relative to the compiled plugin .wasm file.
   * Optional — if absent, engine uses built-in grammar or passes tree: null.
   */
  grammar?: string

  /**
   * Returns true if this plugin should process the given file path.
   * Called for every file during indexing.
   * Default implementation checks extensions — override for custom logic.
   */
  match?: (filePath: string) => boolean

  /**
   * Extract nodes and edges from a file.
   * tree is null if no grammar is available for this file type.
   */
  extract: (filePath: string, content: string, tree: SyntaxTree | null) => ExtractionResult

  /**
   * Domain concept seeds contributed by this plugin.
   */
  concepts?: ConceptSeed[]
}
```

### Amends Spec 7 (Plugin SDK) — SyntaxTree Type

Add to `types.ts`:

```typescript
/**
 * SyntaxTree is a serialized tree-sitter CST passed to extract().
 * The engine parses the file and serializes the tree to JSON before
 * passing it across the WASM boundary.
 */
export interface SyntaxTree {
  /** Root node of the syntax tree */
  root: SyntaxNode

  /** Source text (same as content parameter) */
  source: string

  /** Language the tree was parsed with */
  language: string
}

export interface SyntaxNode {
  /** tree-sitter node type (e.g., "function_declaration", "identifier") */
  type: string

  /** Whether this is a named node (vs anonymous punctuation/keyword) */
  isNamed: boolean

  /** Field name in parent (e.g., "name", "body", "parameters") */
  fieldName: string | null

  /** Source text of this node */
  text: string

  /** Byte offsets in source */
  startByte: number
  endByte:   number

  /** Row/column positions (0-indexed) */
  startPosition: { row: number; column: number }
  endPosition:   { row: number; column: number }

  /** Child nodes */
  children: SyntaxNode[]
}
```

### New Architectural Decision — No Built-in Language Handlers

There are no native Go language handlers in the engine. All language
support is delivered via plugins. Default plugins (Go, TypeScript, Python)
are compiled to WASM and embedded in the engine binary via `go:embed`.
They are extracted to `~/.ce/plugins/defaults/` on first run.

Plugin loading is one path for all plugins — no special cases for defaults.
Users can replace default plugins by installing a custom plugin that handles
the same extensions. The last-registered plugin for an extension wins.

### Historical Constraint — CGO Tree-sitter (superseded)

The original implementation plan allowed CGO for tree-sitter runtime and
grammar bindings. That is no longer permitted: all current parsing is WASM on
wazero and cross-compiles as pure Go. The remaining text documents the prior
design only.

---

## 1. Overview

The indexer is the component that populates the substrate graph. It:

1. Walks the project directory tree
2. Finds language plugins that match each file
3. Parses files using the appropriate tree-sitter grammar
4. Calls plugin `extract()` with the serialized CST
5. Sends resulting nodes and edges to the write buffer
6. Tracks file hashes for incremental reindex
7. Watches for file changes and reindexes incrementally

The indexer runs in two modes:
- **Full index** — walk all files, process everything
- **Incremental** — only process files whose content hash changed

---

## 2. Package Structure

```
internal/indexer/
  indexer.go          — Indexer struct, Run(), Watch()
  walker/
    walker.go         — directory walker, gitignore support
    ignore.go         — ignore pattern matching
  parser/
    parser.go         — routes files to grammars, produces SyntaxTree
    grammar.go        — grammar registry, built-in + dynamic loading
    serialize.go      — tree-sitter CST → SyntaxTree JSON
  watcher/
    watcher.go        — fsnotify wrapper
    debounce.go       — debounce rapid file changes
  progress/
    progress.go       — indexing progress tracking, channel emissions
```

---

## 3. Default Plugin Embedding

Default plugins are embedded in the engine binary and extracted on first run.

```go
// internal/indexer/defaults.go

package indexer

import "embed"

// Default plugin .wasm files are embedded at build time.
// Built from the plugin SDK repo as part of the release pipeline.
// Grammar .wasm files are embedded alongside plugin .wasm files.
//go:embed defaults/go-language.wasm
//go:embed defaults/go-grammar.wasm
//go:embed defaults/typescript.wasm
//go:embed defaults/typescript-grammar.wasm
//go:embed defaults/python.wasm
//go:embed defaults/python-grammar.wasm
var defaultPluginFiles embed.FS

// ExtractDefaults extracts embedded default plugins to the data directory
// if they are not already present or if the embedded version is newer.
// Called at engine startup before plugin loading.
func ExtractDefaults(dataDir string) error {
    defaultsDir := filepath.Join(dataDir, "plugins", "defaults")
    if err := os.MkdirAll(defaultsDir, 0755); err != nil {
        return fmt.Errorf("create defaults dir: %w", err)
    }

    files := []string{
        "go-language.wasm",
        "go-grammar.wasm",
        "typescript.wasm",
        "typescript-grammar.wasm",
        "python.wasm",
        "python-grammar.wasm",
    }

    for _, name := range files {
        destPath := filepath.Join(defaultsDir, name)
        data, err := defaultPluginFiles.ReadFile("defaults/" + name)
        if err != nil {
            return fmt.Errorf("read embedded %s: %w", name, err)
        }

        // Only write if absent or content changed
        if shouldWrite(destPath, data) {
            if err := os.WriteFile(destPath, data, 0644); err != nil {
                return fmt.Errorf("write %s: %w", destPath, err)
            }
        }
    }

    return nil
}

func shouldWrite(path string, newContent []byte) bool {
    existing, err := os.ReadFile(path)
    if err != nil {
        return true // absent — write it
    }
    existingHash := sha256.Sum256(existing)
    newHash := sha256.Sum256(newContent)
    return existingHash != newHash // write if content changed
}
```

### Default Plugin Registration

Default plugins are registered automatically in `ce.yaml` as a separate
tier — they appear in `ce plugin list` but are not in the user's plugin
list and cannot be accidentally removed via `ce plugin remove`.

```go
// internal/runner/runner.go (amended)

func New(ctx context.Context, cfg *config.Config) (*Engine, error) {
    // Extract default plugins before loading any plugins
    if err := indexer.ExtractDefaults(cfg.DataDir); err != nil {
        return nil, fmt.Errorf("extract default plugins: %w", err)
    }

    // Load default plugins first (lowest priority)
    defaultsDir := filepath.Join(cfg.DataDir, "plugins", "defaults")
    defaultPlugins := []string{
        filepath.Join(defaultsDir, "go-language.wasm"),
        filepath.Join(defaultsDir, "typescript.wasm"),
        filepath.Join(defaultsDir, "python.wasm"),
    }
    for _, path := range defaultPlugins {
        if err := e.plugins.Load(ctx, path); err != nil {
            // Non-fatal — log warning, continue without this default
            log.Printf("warning: failed to load default plugin %s: %v", path, err)
        }
    }

    // Load user plugins (higher priority — can override defaults)
    for _, entry := range cfg.Plugins.Installed {
        if err := e.plugins.Load(ctx, entry.Path); err != nil {
            return nil, fmt.Errorf("load plugin %s: %w", entry.Path, err)
        }
    }

    // ...
}
```

---

## 4. Grammar Registry

The grammar registry maps file extensions to loaded tree-sitter grammars.
Both built-in grammars (compiled with CGO) and dynamic grammars (loaded
from plugin-supplied .wasm files) are registered here.

```go
// internal/indexer/parser/grammar.go

package parser

import (
    sitter "github.com/smacker/go-tree-sitter"
    "github.com/smacker/go-tree-sitter/golang"
    "github.com/smacker/go-tree-sitter/javascript"
    "github.com/smacker/go-tree-sitter/python"
    "github.com/smacker/go-tree-sitter/typescript/tsx"
    "github.com/smacker/go-tree-sitter/typescript/typescript"
)

// GrammarSource identifies where a grammar came from.
type GrammarSource string

const (
    GrammarBuiltIn GrammarSource = "builtin"
    GrammarPlugin  GrammarSource = "plugin"
)

// Grammar is a loaded tree-sitter language with metadata.
type Grammar struct {
    Language   *sitter.Language
    Name       string
    Source     GrammarSource
    PluginID   string // set if Source == GrammarPlugin
}

// GrammarRegistry maps file extensions to grammars.
// Plugin-registered grammars override built-in grammars for the same extension.
type GrammarRegistry struct {
    mu       sync.RWMutex
    grammars map[string]*Grammar // extension → Grammar
}

func NewGrammarRegistry() *GrammarRegistry {
    r := &GrammarRegistry{
        grammars: make(map[string]*Grammar),
    }
    r.registerBuiltIns()
    return r
}

// registerBuiltIns loads the CGO-compiled built-in grammars.
// These are the fallback grammars when no plugin provides one.
// Note: built-in grammars are registered here but built-in LANGUAGE SUPPORT
// is delivered via the default plugins (go-language.wasm, etc.).
// The grammar registry only handles parsing — extraction is always via plugins.
func (r *GrammarRegistry) registerBuiltIns() {
    builtIns := map[string]*Grammar{
        ".go": {
            Language: golang.GetLanguage(),
            Name:     "go",
            Source:   GrammarBuiltIn,
        },
        ".ts": {
            Language: typescript.GetLanguage(),
            Name:     "typescript",
            Source:   GrammarBuiltIn,
        },
        ".tsx": {
            Language: tsx.GetLanguage(),
            Name:     "tsx",
            Source:   GrammarBuiltIn,
        },
        ".js": {
            Language: javascript.GetLanguage(),
            Name:     "javascript",
            Source:   GrammarBuiltIn,
        },
        ".jsx": {
            Language: javascript.GetLanguage(),
            Name:     "javascript",
            Source:   GrammarBuiltIn,
        },
        ".py": {
            Language: python.GetLanguage(),
            Name:     "python",
            Source:   GrammarBuiltIn,
        },
    }

    for ext, g := range builtIns {
        r.grammars[ext] = g
    }
}

// RegisterPluginGrammar loads a tree-sitter grammar from a WASM file
// and registers it for the given extensions.
// Plugin grammars override built-in grammars.
func (r *GrammarRegistry) RegisterPluginGrammar(
    grammarWASMPath string,
    extensions []string,
    pluginID string,
) error {
    lang, err := loadWASMGrammar(grammarWASMPath)
    if err != nil {
        return fmt.Errorf("load grammar wasm %s: %w", grammarWASMPath, err)
    }

    grammar := &Grammar{
        Language: lang,
        Name:     pluginID,
        Source:   GrammarPlugin,
        PluginID: pluginID,
    }

    r.mu.Lock()
    defer r.mu.Unlock()

    for _, ext := range extensions {
        r.grammars[ext] = grammar
    }

    return nil
}

// ForExtension returns the grammar for a file extension, or nil if none.
func (r *GrammarRegistry) ForExtension(ext string) *Grammar {
    r.mu.RLock()
    defer r.mu.RUnlock()
    return r.grammars[ext]
}
```

### Loading a WASM Grammar

Tree-sitter supports loading grammars compiled to WASM via its own WASM
runtime. The Go binding (`smacker/go-tree-sitter`) exposes this:

```go
// internal/indexer/parser/grammar.go

func loadWASMGrammar(wasmPath string) (*sitter.Language, error) {
    wasmBytes, err := os.ReadFile(wasmPath)
    if err != nil {
        return nil, fmt.Errorf("read grammar wasm: %w", err)
    }

    // tree-sitter's WASM grammar loading.
    // This uses the tree-sitter WASM runtime embedded in the Go binding,
    // separate from our wazero runtime used for plugin execution.
    lang, err := sitter.NewLanguageFromWASM(wasmBytes)
    if err != nil {
        return nil, fmt.Errorf("load grammar from wasm: %w", err)
    }

    return lang, nil
}
```

---

## 5. File Parser — CST Production

The parser takes a file path and content, finds the appropriate grammar,
parses the file, and returns a serialized SyntaxTree.

```go
// internal/indexer/parser/parser.go

package parser

import (
    sitter "github.com/smacker/go-tree-sitter"
    "context"
)

// Parser routes files to grammars and produces serialized syntax trees.
type Parser struct {
    grammars *GrammarRegistry
    pool     *parserPool // pool of sitter.Parser instances (expensive to create)
}

func NewParser(grammars *GrammarRegistry) *Parser {
    return &Parser{
        grammars: grammars,
        pool:     newParserPool(runtime.NumCPU()),
    }
}

// Parse parses a file and returns a serialized SyntaxTree.
// Returns nil if no grammar is registered for this file extension.
// The serialized tree is JSON — passed directly to plugin extract() via WASM.
func (p *Parser) Parse(ctx context.Context, filePath string, content []byte) ([]byte, error) {
    ext := strings.ToLower(filepath.Ext(filePath))
    grammar := p.grammars.ForExtension(ext)
    if grammar == nil {
        return nil, nil // no grammar — plugin will receive tree: null
    }

    // Get a parser from the pool
    parser := p.pool.Get()
    defer p.pool.Put(parser)

    parser.SetLanguage(grammar.Language)

    tree, err := parser.ParseCtx(ctx, nil, content)
    if err != nil {
        return nil, fmt.Errorf("parse %s: %w", filePath, err)
    }
    defer tree.Close()

    // Serialize to JSON for WASM boundary
    syntaxTree := serializeTree(tree, content, grammar.Name)
    return json.Marshal(syntaxTree)
}

// parserPool manages a pool of tree-sitter parsers.
// sitter.Parser is not goroutine-safe; pool one per goroutine.
type parserPool struct {
    pool chan *sitter.Parser
}

func newParserPool(size int) *parserPool {
    p := &parserPool{pool: make(chan *sitter.Parser, size)}
    for i := 0; i < size; i++ {
        p.pool <- sitter.NewParser()
    }
    return p
}

func (p *parserPool) Get() *sitter.Parser  { return <-p.pool }
func (p *parserPool) Put(parser *sitter.Parser) { p.pool <- parser }
```

---

## 6. CST Serialization

Converts a tree-sitter tree to the `SyntaxTree` JSON format defined in
the plugin SDK types. This is the data structure plugin `extract()` receives.

```go
// internal/indexer/parser/serialize.go

package parser

import "github.com/smacker/go-tree-sitter"

// SyntaxTree mirrors the TypeScript SyntaxTree type in @ce/plugin-sdk.
// Must stay in sync with spec-7-plugin-sdk.md type definitions.
type SyntaxTree struct {
    Root     *SyntaxNode `json:"root"`
    Source   string      `json:"source"`
    Language string      `json:"language"`
}

type SyntaxNode struct {
    Type          string       `json:"type"`
    IsNamed       bool         `json:"isNamed"`
    FieldName     *string      `json:"fieldName"`
    Text          string       `json:"text"`
    StartByte     uint32       `json:"startByte"`
    EndByte       uint32       `json:"endByte"`
    StartPosition Position     `json:"startPosition"`
    EndPosition   Position     `json:"endPosition"`
    Children      []*SyntaxNode `json:"children"`
}

type Position struct {
    Row    uint32 `json:"row"`
    Column uint32 `json:"column"`
}

func serializeTree(tree *sitter.Tree, source []byte, language string) *SyntaxTree {
    return &SyntaxTree{
        Root:     serializeNode(tree.RootNode(), source, ""),
        Source:   string(source),
        Language: language,
    }
}

func serializeNode(node *sitter.Node, source []byte, fieldName string) *SyntaxNode {
    if node == nil {
        return nil
    }

    sn := &SyntaxNode{
        Type:    node.Type(),
        IsNamed: node.IsNamed(),
        Text:    string(source[node.StartByte():node.EndByte()]),
        StartByte: node.StartByte(),
        EndByte:   node.EndByte(),
        StartPosition: Position{
            Row:    node.StartPoint().Row,
            Column: node.StartPoint().Column,
        },
        EndPosition: Position{
            Row:    node.EndPoint().Row,
            Column: node.EndPoint().Column,
        },
    }

    if fieldName != "" {
        sn.FieldName = &fieldName
    }

    // Serialize children
    childCount := int(node.ChildCount())
    if childCount > 0 {
        sn.Children = make([]*SyntaxNode, 0, childCount)
        for i := 0; i < childCount; i++ {
            child := node.Child(i)
            // Get field name for this child position
            field := node.FieldNameForChild(i)
            sn.Children = append(sn.Children, serializeNode(child, source, field))
        }
    }

    return sn
}
```

### Serialization performance note

For large files, serializing the full CST to JSON and passing it through
the WASM boundary is the performance bottleneck. Mitigation strategies
(not implemented in Phase 2, noted for future):

- Lazy serialization — only serialize nodes within a depth limit, expand on demand
- Filtered serialization — only serialize named nodes (skip punctuation/keywords)
- Binary format — MessagePack instead of JSON

For Phase 2, full JSON serialization is correct and sufficient. Profile
before optimizing.

---

## 7. Directory Walker

```go
// internal/indexer/walker/walker.go

package walker

import (
    "io/fs"
    "os"
    "path/filepath"
)

// WalkResult is sent on the results channel for each file found.
type WalkResult struct {
    Path    string // absolute path
    RelPath string // relative to project root
    Info    fs.FileInfo
}

// Walker walks a directory tree respecting ignore patterns.
type Walker struct {
    root    string
    ignore  *IgnoreMatcher
    maxSize int64
}

func New(root string, cfg WalkerConfig) (*Walker, error) {
    ignore, err := newIgnoreMatcher(root, cfg.ExcludePatterns)
    if err != nil {
        return nil, err
    }
    return &Walker{
        root:    root,
        ignore:  ignore,
        maxSize: int64(cfg.MaxFileSizeBytes),
    }, nil
}

// Walk sends all non-ignored files to the results channel.
// Runs in its own goroutine — caller reads from results.
// Closes results channel when complete.
func (w *Walker) Walk(ctx context.Context, results chan<- WalkResult) error {
    defer close(results)

    return filepath.WalkDir(w.root, func(path string, d fs.DirEntry, err error) error {
        if err != nil {
            return nil // skip unreadable entries
        }

        // Check context cancellation
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
        }

        relPath, _ := filepath.Rel(w.root, path)

        // Skip ignored directories (prune the walk)
        if d.IsDir() {
            if w.ignore.MatchDir(relPath) {
                return filepath.SkipDir
            }
            return nil
        }

        // Skip ignored files
        if w.ignore.MatchFile(relPath) {
            return nil
        }

        // Skip files over size limit
        info, err := d.Info()
        if err != nil || info.Size() > w.maxSize {
            return nil
        }

        results <- WalkResult{
            Path:    path,
            RelPath: relPath,
            Info:    info,
        }

        return nil
    })
}
```

### Ignore Pattern Matching

```go
// internal/indexer/walker/ignore.go

// IgnoreMatcher combines ce.yaml exclude patterns with .gitignore rules.
type IgnoreMatcher struct {
    patterns []glob.Glob
}

func newIgnoreMatcher(root string, configPatterns []string) (*IgnoreMatcher, error) {
    var patterns []glob.Glob

    // ce.yaml exclude patterns
    for _, p := range configPatterns {
        g, err := glob.Compile(p, '/')
        if err != nil {
            return nil, fmt.Errorf("invalid exclude pattern %q: %w", p, err)
        }
        patterns = append(patterns, g)
    }

    // Read .gitignore if present
    gitignorePath := filepath.Join(root, ".gitignore")
    if data, err := os.ReadFile(gitignorePath); err == nil {
        for _, line := range strings.Split(string(data), "\n") {
            line = strings.TrimSpace(line)
            if line == "" || strings.HasPrefix(line, "#") {
                continue
            }
            // Convert gitignore pattern to glob
            pattern := gitignoreToGlob(line)
            if g, err := glob.Compile(pattern, '/'); err == nil {
                patterns = append(patterns, g)
            }
        }
    }

    return &IgnoreMatcher{patterns: patterns}, nil
}

func (m *IgnoreMatcher) MatchFile(relPath string) bool {
    for _, p := range m.patterns {
        if p.Match(relPath) {
            return true
        }
    }
    return false
}

func (m *IgnoreMatcher) MatchDir(relPath string) bool {
    // For directories, check if the dir itself or all its contents would be ignored
    return m.MatchFile(relPath) || m.MatchFile(relPath+"/")
}
```

---

## 8. The Indexer

```go
// internal/indexer/indexer.go

package indexer

// Indexer orchestrates full and incremental indexing.
type Indexer struct {
    cfg       *config.Config
    plugins   *plugins.Registry
    parser    *parser.Parser
    grammars  *parser.GrammarRegistry
    substrate core.SubstrateWriter
    queries   *queries.IndexQueries
    channels  *core.AppChannels
}

func New(
    cfg      *config.Config,
    plugins  *plugins.Registry,
    substrate core.SubstrateWriter,
    queries  *queries.IndexQueries,
    channels *core.AppChannels,
) *Indexer {
    grammars := parser.NewGrammarRegistry()

    // Register grammars from loaded plugins
    for _, plugin := range plugins.Loaded() {
        if h := plugin.Language(); h != nil {
            if grammarPath := h.GrammarPath(); grammarPath != "" {
                if err := grammars.RegisterPluginGrammar(
                    grammarPath,
                    h.Extensions(),
                    string(plugin.ID()),
                ); err != nil {
                    // Non-fatal — log warning, use built-in grammar as fallback
                    channels.Emit(core.Emission{
                        Channel: core.ChanWarning,
                        Content: fmt.Sprintf("plugin %s grammar load failed: %v", plugin.ID(), err),
                    })
                }
            }
        }
    }

    return &Indexer{
        cfg:      cfg,
        plugins:  plugins,
        parser:   parser.NewParser(grammars),
        grammars: grammars,
        substrate: substrate,
        queries:  queries,
        channels: channels,
    }
}

// Run performs a full or incremental index of the project.
// Emits progress to channels throughout.
func (idx *Indexer) Run(ctx context.Context, projectID core.ProjectID, full bool) error {
    runID := uuid.New().String()

    // Open index run record
    run, err := idx.queries.OpenIndexRun(ctx, runID, string(projectID))
    if err != nil {
        return fmt.Errorf("open index run: %w", err)
    }

    idx.channels.Emit(core.Emission{
        Channel: core.ChanSystem,
        Content: fmt.Sprintf("indexing %s (mode: %s)",
            projectID, map[bool]string{true: "full", false: "incremental"}[full]),
    })

    // Load existing file hashes for incremental mode
    var existingHashes map[string]string // relPath → hash
    if !full {
        existingHashes, err = idx.queries.GetFileHashes(ctx, string(projectID))
        if err != nil {
            return fmt.Errorf("load file hashes: %w", err)
        }
    }

    // Walk the project directory
    walker, err := walker.New(idx.cfg.Project.Path, walker.WalkerConfig{
        ExcludePatterns: idx.cfg.Indexer.Exclude,
        MaxFileSizeBytes: idx.cfg.Indexer.MaxFileSizeBytes,
    })
    if err != nil {
        return fmt.Errorf("create walker: %w", err)
    }

    fileResults := make(chan walker.WalkResult, 64)
    walkErr := make(chan error, 1)

    go func() {
        walkErr <- walker.Walk(ctx, fileResults)
    }()

    // Process files concurrently
    // Worker count: number of CPUs, capped at 8
    workerCount := min(runtime.NumCPU(), 8)
    var wg sync.WaitGroup
    stats := &indexStats{}

    for i := 0; i < workerCount; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for result := range fileResults {
                if err := idx.processFile(ctx, projectID, result,
                    existingHashes, stats); err != nil {
                    idx.channels.Emit(core.Emission{
                        Channel: core.ChanWarning,
                        Content: fmt.Sprintf("index %s: %v", result.RelPath, err),
                    })
                }
            }
        }()
    }

    wg.Wait()

    // Check walker error
    if err := <-walkErr; err != nil && err != context.Canceled {
        return fmt.Errorf("walk: %w", err)
    }

    // Flush write buffer before closing run
    if err := idx.substrate.Flush(ctx); err != nil {
        idx.channels.Emit(core.Emission{
            Channel: core.ChanWarning,
            Content: fmt.Sprintf("flush write buffer: %v", err),
        })
    }

    // Close index run record
    idx.queries.CloseIndexRun(ctx, runID, run.ToComplete(stats))

    idx.channels.Emit(core.Emission{
        Channel: core.ChanSystem,
        Content: fmt.Sprintf("index complete: %d files, %d nodes, %d edges",
            stats.filesProcessed, stats.nodesCreated, stats.edgesCreated),
    })

    return nil
}
```

### Processing a Single File

```go
func (idx *Indexer) processFile(
    ctx context.Context,
    projectID core.ProjectID,
    result walker.WalkResult,
    existingHashes map[string]string,
    stats *indexStats,
) error {
    // ── Incremental check ──────────────────────────────────────────────────
    content, err := os.ReadFile(result.Path)
    if err != nil {
        return fmt.Errorf("read: %w", err)
    }

    hash := fileHash(content)

    if existingHash, exists := existingHashes[result.RelPath]; exists {
        if existingHash == hash {
            return nil // unchanged — skip
        }
    }

    // ── Find plugin for this file ──────────────────────────────────────────
    plugin := idx.plugins.PluginForFile(result.RelPath)
    if plugin == nil {
        return nil // no plugin handles this file type
    }

    langHandler := plugin.Language()
    if langHandler == nil {
        return nil
    }

    // ── Parse file ─────────────────────────────────────────────────────────
    treeJSON, err := idx.parser.Parse(ctx, result.RelPath, content)
    if err != nil {
        return fmt.Errorf("parse: %w", err)
    }
    // treeJSON is nil if no grammar available — plugin receives tree: null

    // ── Call plugin extract() ──────────────────────────────────────────────
    extraction, err := langHandler.Extract(result.RelPath, content, treeJSON)
    if err != nil {
        return fmt.Errorf("extract: %w", err)
    }

    // ── Send to write buffer ───────────────────────────────────────────────
    for _, node := range extraction.Nodes {
        node.ProjectID = projectID
        if err := idx.substrate.UpsertNode(ctx, node); err != nil {
            return fmt.Errorf("upsert node: %w", err)
        }
    }

    for _, edge := range extraction.Edges {
        edge.ProjectID = projectID
        if err := idx.substrate.UpsertEdge(ctx, edge); err != nil {
            return fmt.Errorf("upsert edge: %w", err)
        }
    }

    // Run analyzers if plugin has them
    if analyzers := plugin.Analyzers(); len(analyzers) > 0 {
        for _, analyzer := range analyzers {
            additionalEdges, err := analyzer.Analyze(extraction.Nodes)
            if err != nil {
                idx.channels.Emit(core.Emission{
                    Channel: core.ChanWarning,
                    Content: fmt.Sprintf("analyzer %s on %s: %v",
                        analyzer.Name(), result.RelPath, err),
                })
                continue
            }
            for _, edge := range additionalEdges {
                edge.ProjectID = projectID
                idx.substrate.UpsertEdge(ctx, edge)
            }
        }
    }

    // Update file hash
    idx.queries.UpsertFileHash(ctx, string(projectID), result.RelPath, hash)

    atomic.AddInt64(&stats.filesProcessed, 1)
    atomic.AddInt64(&stats.nodesCreated, int64(len(extraction.Nodes)))
    atomic.AddInt64(&stats.edgesCreated, int64(len(extraction.Edges)))

    return nil
}

// fileHash returns the SHA-256 hash of file content as a hex string.
func fileHash(content []byte) string {
    h := sha256.Sum256(content)
    return hex.EncodeToString(h[:])
}
```

---

## 9. Plugin Registry Amendment — PluginForFile

The plugin registry needs a method to find the right plugin for a file.
Last-registered plugin for an extension wins (user plugins override defaults).

```go
// internal/plugins/registry/registry.go (amendment)

// PluginForFile returns the plugin that handles the given file path.
// Returns nil if no plugin matches.
// If multiple plugins match, the last-registered wins
// (user plugins are registered after defaults).
func (r *Registry) PluginForFile(filePath string) core.Plugin {
    ext := strings.ToLower(filepath.Ext(filePath))

    r.mu.RLock()
    defer r.mu.RUnlock()

    // Iterate in reverse registration order — last registered wins
    var match core.Plugin
    for _, plugin := range r.loadOrder {
        p := r.plugins[plugin]
        if h := p.Language(); h != nil {
            for _, handledExt := range h.Extensions() {
                if handledExt == ext {
                    match = p
                    break
                }
            }
        }
        // Also check match() function if defined
        if h := p.Language(); h != nil && h.HasCustomMatch() {
            if h.Match(filePath) {
                match = p
            }
        }
    }

    return match
}
```

---

## 10. Plugin Language Handler Amendment

The `core.LanguageHandler` interface is amended to support the new fields:

```go
// internal/core/interfaces.go (amended LanguageHandler)

// LanguageHandler teaches the indexer about a language or framework.
type LanguageHandler interface {
    // Extensions returns file extensions this handler processes.
    // Example: []string{".go"} or []string{".ts", ".tsx", ".js", ".jsx"}
    Extensions() []string

    // GrammarPath returns the path to a tree-sitter grammar WASM file.
    // Returns empty string if using built-in grammar or no grammar needed.
    GrammarPath() string

    // Match returns true if this handler should process the given file path.
    // Default implementation checks Extensions() — override for custom logic.
    Match(filePath string) bool

    // HasCustomMatch returns true if Match() has been overridden.
    // If false, the registry uses extension matching only (faster).
    HasCustomMatch() bool

    // Extract parses a file and returns nodes and edges.
    // treeJSON is the serialized SyntaxTree, or nil if no grammar available.
    Extract(filePath string, content []byte, treeJSON []byte) (ExtractionResult, error)

    // Concepts returns concept seeds contributed by this handler.
    Concepts() []ConceptSeed
}
```

---

## 11. File Hash Storage

File hashes are stored in the project's graph database to support
incremental reindex. Add this table to the graph schema (amends Spec 1):

```sql
-- Amendment to spec-1-data-layer.md graph schema
-- Add to graph migration 000002_file_hashes.up.sql

CREATE TABLE IF NOT EXISTS file_hashes (
    project_id  TEXT NOT NULL,
    rel_path    TEXT NOT NULL,
    hash        TEXT NOT NULL,    -- SHA-256 hex
    indexed_at  INTEGER NOT NULL,
    PRIMARY KEY (project_id, rel_path)
);

CREATE INDEX IF NOT EXISTS idx_file_hashes_project
    ON file_hashes(project_id);
```

---

## 12. File Watcher

```go
// internal/indexer/watcher/watcher.go

package watcher

import (
    "github.com/fsnotify/fsnotify"
)

// Watcher watches for file changes and triggers incremental reindex.
type Watcher struct {
    root    string
    watcher *fsnotify.Watcher
    debounce *Debouncer
    onChange func(paths []string)  // called with changed file paths
}

func New(root string, debounceMS int, onChange func(paths []string)) (*Watcher, error) {
    fw, err := fsnotify.NewWatcher()
    if err != nil {
        return nil, err
    }

    w := &Watcher{
        root:     root,
        watcher:  fw,
        debounce: NewDebouncer(time.Duration(debounceMS) * time.Millisecond),
        onChange: onChange,
    }

    // Watch the project root recursively
    if err := w.addRecursive(root); err != nil {
        fw.Close()
        return nil, err
    }

    return w, nil
}

func (w *Watcher) Run(ctx context.Context) {
    for {
        select {
        case event, ok := <-w.watcher.Events:
            if !ok {
                return
            }
            if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
                w.debounce.Add(event.Name)
            }
            // Handle new directory — add to watcher
            if event.Has(fsnotify.Create) {
                if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
                    w.addRecursive(event.Name)
                }
            }

        case <-w.debounce.Ready():
            paths := w.debounce.Flush()
            if len(paths) > 0 {
                w.onChange(paths)
            }

        case <-ctx.Done():
            w.watcher.Close()
            return
        }
    }
}
```

```go
// internal/indexer/watcher/debounce.go

// Debouncer accumulates file change events and fires after
// a quiet period. Prevents reindexing on every keystroke.
type Debouncer struct {
    mu       sync.Mutex
    paths    map[string]struct{}
    timer    *time.Timer
    interval time.Duration
    ready    chan struct{}
}

func NewDebouncer(interval time.Duration) *Debouncer {
    return &Debouncer{
        paths:    make(map[string]struct{}),
        interval: interval,
        ready:    make(chan struct{}, 1),
    }
}

func (d *Debouncer) Add(path string) {
    d.mu.Lock()
    defer d.mu.Unlock()

    d.paths[path] = struct{}{}

    if d.timer != nil {
        d.timer.Reset(d.interval)
    } else {
        d.timer = time.AfterFunc(d.interval, func() {
            select {
            case d.ready <- struct{}{}:
            default:
            }
        })
    }
}

func (d *Debouncer) Ready() <-chan struct{} { return d.ready }

func (d *Debouncer) Flush() []string {
    d.mu.Lock()
    defer d.mu.Unlock()

    paths := make([]string, 0, len(d.paths))
    for p := range d.paths {
        paths = append(paths, p)
    }
    d.paths = make(map[string]struct{})
    d.timer = nil
    return paths
}
```

---

## 13. Progress Emissions

The indexer emits structured progress so the TUI can render a progress bar.

```go
// internal/indexer/progress/progress.go

// IndexProgress is emitted periodically during indexing.
// Sent to ChanProgress channel.
type IndexProgress struct {
    FilesProcessed int
    FilesTotal     int     // -1 if unknown (streaming walk)
    NodesCreated   int
    EdgesCreated   int
    CurrentFile    string
    ElapsedMS      int64
}

func (p *IndexProgress) Render() string {
    if p.FilesTotal > 0 {
        pct := float64(p.FilesProcessed) / float64(p.FilesTotal) * 100
        return fmt.Sprintf("indexing: %.0f%% (%d/%d files, %d nodes)",
            pct, p.FilesProcessed, p.FilesTotal, p.NodesCreated)
    }
    return fmt.Sprintf("indexing: %d files, %d nodes, %d edges",
        p.FilesProcessed, p.NodesCreated, p.EdgesCreated)
}
```

---

## 14. ce plugin list — Default Plugin Display

Amendment to Spec 6 (CLI). The `ce plugin list` command shows default
plugins separately from user-installed plugins:

```go
// cli/plugin.go (amended)

func runPluginList(cmd *cobra.Command, args []string) error {
    // Default plugins (embedded)
    fmt.Println("Default plugins:")
    for _, p := range engine.DefaultPlugins() {
        fmt.Printf("  %-20s v%-10s %s\n",
            p.Name(), p.Version(), strings.Join(p.Extensions(), " "))
    }

    // User-installed plugins
    fmt.Println("\nInstalled plugins:")
    installed := cfg.Plugins.Installed
    if len(installed) == 0 {
        fmt.Println("  (none)")
        return nil
    }
    for _, p := range engine.UserPlugins() {
        fmt.Printf("  %-20s v%-10s %s\n",
            p.Name(), p.Version(), strings.Join(p.Extensions(), " "))
    }
    return nil
}
```

---

## 15. Goreleaser Configuration

```yaml
# .goreleaser.yaml — at engine repo root

project_name: ce

builds:
  - id: ce
    main: ./cmd/ce
    binary: ce
    env:
      - CGO_ENABLED=1
    goos:
      - linux
      - darwin
      - windows
    goarch:
      - amd64
      - arm64
    overrides:
      - goos: linux
        goarch: amd64
        env:
          - CC=zig cc -target x86_64-linux-gnu
      - goos: linux
        goarch: arm64
        env:
          - CC=zig cc -target aarch64-linux-gnu
      - goos: darwin
        goarch: amd64
        env:
          - CC=zig cc -target x86_64-macos
      - goos: darwin
        goarch: arm64
        env:
          - CC=zig cc -target aarch64-macos
      - goos: windows
        goarch: amd64
        env:
          - CC=zig cc -target x86_64-windows-gnu
      - goos: windows
        goarch: arm64
        env:
          - CC=zig cc -target aarch64-windows-gnu

archives:
  - format: tar.gz
    format_overrides:
      - goos: windows
        format: zip
    name_template: "ce_{{ .Version }}_{{ .Os }}_{{ .Arch }}"

brews:
  - repository:
      owner: atheory-ai
      name: homebrew-tap
    homepage: https://atheory.ai
    description: "Context Engine — AI-powered codebase intelligence"
    install: |
      bin.install "ce"

checksum:
  name_template: "checksums.txt"

release:
  github:
    owner: atheory-ai
    name: context-engine
```

---

## 16. Package Layout Summary

```
internal/indexer/
  indexer.go            — Indexer struct, Run(), processFile()
  defaults.go           — ExtractDefaults(), shouldWrite()
  defaults/             — embedded default plugin .wasm files (build artifact)
  walker/
    walker.go           — Walker struct, Walk()
    ignore.go           — IgnoreMatcher, gitignore support
  parser/
    parser.go           — Parser struct, Parse()
    grammar.go          — GrammarRegistry, built-in + WASM grammar loading
    serialize.go        — serializeTree(), SyntaxTree, SyntaxNode Go types
  watcher/
    watcher.go          — Watcher, fsnotify wrapper
    debounce.go         — Debouncer
  progress/
    progress.go         — IndexProgress emission
```

---

## 17. Key Decisions Captured in This Spec

| Decision | Value |
|----------|-------|
| CGO | Historical plan allowed it for tree-sitter; current engine forbids it |
| Language support delivery | All via plugins — no native handlers in engine |
| Default plugins | Embedded via go:embed, extracted on first run |
| Default plugin override | Last-registered plugin for extension wins |
| Grammar loading | Built-in CGO grammars + dynamic WASM grammars from plugins |
| Plugin grammar registration | Declared in manifest, loaded at plugin load time |
| CST serialization | Full JSON tree (SyntaxTree type) across WASM boundary |
| Incremental reindex | File content hash (SHA-256), stored in graph DB |
| Walker | Respects both ce.yaml exclude patterns and .gitignore |
| Concurrency | min(NumCPU, 8) worker goroutines for file processing |
| File watcher debounce | Configurable, default 500ms (from Spec 6 ce.yaml) |
| Distribution | Goreleaser + GitHub Actions, Homebrew tap |
| go install | Not supported (CGO requirement) |
| Built-in languages | Go, TypeScript/TSX, JavaScript/JSX, Python |

---

*Spec 9: Indexer — v1.0 — February 2026*
*Next: Spec 10 — Activation & Graph*
*Companion: Context Engine PRD v0.5 Section 13 | Decisions Log v1.0*
