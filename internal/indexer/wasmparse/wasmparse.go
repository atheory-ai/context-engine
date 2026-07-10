// Package wasmparse parses source files with tree-sitter running as WASM on
// wazero (pure Go, no CGO). The tree-sitter core is compiled to WASM once and
// embedded; each grammar is a WASM side module loaded at runtime via a small
// dynamic linker (emscripten MAIN_MODULE=2 model: a non-PIC core sharing its
// memory + indirect function table with PIC grammar side modules).
//
// It produces the same SyntaxTree the CGO parser does, so plugin
// extract() calls are unaffected. See docs/specs/18-spec-wasm-grammar-loader.md.
package wasmparse

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

//go:embed wasm/tree-sitter-core.wasm
var coreWASM []byte

//go:embed wasm/go.wasm
var goGrammarWASM []byte

//go:embed wasm/python.wasm
var pythonGrammarWASM []byte

//go:embed wasm/javascript.wasm
var javascriptGrammarWASM []byte

//go:embed wasm/typescript.wasm
var typescriptGrammarWASM []byte

//go:embed wasm/tsx.wasm
var tsxGrammarWASM []byte

// instance is one independent tree-sitter engine: its own wazero runtime, core,
// table-grow helper, and loaded grammars. Each instance runs in a single shared
// linear memory, so it is used by one goroutine at a time (enforced by the pool).
type instance struct {
	rt    wazero.Runtime
	core  api.Module
	dl    api.Module
	langs map[string]uint32 // grammar name → TSLanguage* (loaded once per instance)
}

// Parser is a pool of engine instances so the indexer can parse concurrently.
type Parser struct {
	cache wazero.CompilationCache
	pool  chan *instance
	all   []*instance

	// Grammars registered at runtime (e.g. plugin-provided), overriding the
	// bundled defaults. Guarded by mu.
	mu       sync.RWMutex
	dynGramm map[string][]byte // grammar name → wasm
	dynExt   map[string]string // extension → grammar name
}

// RegisterGrammar registers a tree-sitter grammar WASM at runtime for the given
// file extensions, so plugins can add languages without an engine rebuild. The
// grammar's language name is read from its tree_sitter_<name> export. Safe to
// call concurrently; takes effect for subsequent parses.
func (p *Parser) RegisterGrammar(extensions []string, wasm []byte) (name string, err error) {
	// Untrusted plugin input: recover any panic from raw WASM byte parsing, and
	// validate everything loadGrammar will parse (entry, dylink, imports) up
	// front so a grammar that registers can't panic later at parse time.
	defer func() {
		if r := recover(); r != nil {
			name, err = "", fmt.Errorf("malformed grammar wasm: %v", r)
		}
	}()
	if name, err = grammarEntryName(wasm); err != nil {
		return "", err
	}
	if _, _, _, err = dylinkMemInfo(wasm); err != nil {
		return "", fmt.Errorf("grammar %q: %w", name, err)
	}
	if _, err = parseImports(wasm); err != nil {
		return "", fmt.Errorf("grammar %q imports: %w", name, err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.dynGramm == nil {
		p.dynGramm = map[string][]byte{}
		p.dynExt = map[string]string{}
	}
	p.dynGramm[name] = wasm
	for _, e := range extensions {
		p.dynExt[strings.ToLower(e)] = name
	}
	return name, nil
}

// grammarWASM resolves a grammar name to WASM: runtime-registered first, then
// bundled defaults.
func (p *Parser) grammarWASM(name string) ([]byte, bool) {
	p.mu.RLock()
	w, ok := p.dynGramm[name]
	p.mu.RUnlock()
	if ok {
		return w, true
	}
	return builtinGrammar(name)
}

// grammarForExt resolves an extension to a grammar name: runtime-registered
// first, then bundled defaults.
func (p *Parser) grammarForExt(ext string) string {
	p.mu.RLock()
	n, ok := p.dynExt[ext]
	p.mu.RUnlock()
	if ok {
		return n
	}
	return GrammarForExt(ext)
}

// New builds a Parser with a pool of engine instances sized to GOMAXPROCS
// (capped). A shared compilation cache compiles the embedded WASM once and
// reuses it across instances. reference-types (needed by the helper's
// table.grow) is on by default in wazero's core feature set.
func New(ctx context.Context) (*Parser, error) {
	n := runtime.GOMAXPROCS(0)
	if n > 8 {
		n = 8
	}
	if n < 1 {
		n = 1
	}
	p := &Parser{
		cache: wazero.NewCompilationCache(),
		pool:  make(chan *instance, n),
	}
	for i := 0; i < n; i++ {
		in, err := newInstance(ctx, p.cache)
		if err != nil {
			_ = p.Close(ctx)
			return nil, err
		}
		p.all = append(p.all, in)
		p.pool <- in
	}
	return p, nil
}

func newInstance(ctx context.Context, cache wazero.CompilationCache) (*instance, error) {
	rt := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().WithCompilationCache(cache))
	if _, err := wasi_snapshot_preview1.Instantiate(ctx, rt); err != nil {
		return nil, fmt.Errorf("wasi: %w", err)
	}
	core, err := rt.InstantiateWithConfig(ctx, coreWASM, wazero.NewModuleConfig().WithName("core"))
	if err != nil {
		return nil, fmt.Errorf("instantiate core: %w", err)
	}
	dl, err := rt.InstantiateWithConfig(ctx, dlModule(), wazero.NewModuleConfig().WithName("dl"))
	if err != nil {
		return nil, fmt.Errorf("instantiate dl helper: %w", err)
	}
	return &instance{rt: rt, core: core, dl: dl, langs: map[string]uint32{}}, nil
}

// Close releases every instance's runtime and the shared compilation cache.
func (p *Parser) Close(ctx context.Context) error {
	for _, in := range p.all {
		_ = in.rt.Close(ctx)
	}
	if p.cache != nil {
		_ = p.cache.Close(ctx)
	}
	return nil
}

// GrammarForExt maps a lowercase file extension (with dot) to a bundled grammar
// name, or "" if unsupported.
func GrammarForExt(ext string) string {
	switch ext {
	case ".go":
		return "go"
	case ".py", ".pyi":
		return "python"
	case ".js", ".jsx", ".mjs", ".cjs":
		return "javascript"
	case ".ts", ".mts", ".cts":
		return "typescript"
	case ".tsx":
		return "tsx"
	}
	return ""
}

// ParseFile parses a file by extension and returns the serialized SyntaxTree
// JSON for the plugin boundary, or nil if no bundled grammar handles it.
func (p *Parser) ParseFile(ctx context.Context, filePath string, content []byte) ([]byte, error) {
	name := p.grammarForExt(strings.ToLower(filepath.Ext(filePath)))
	if name == "" {
		return nil, nil
	}
	tree, err := p.Parse(ctx, name, content)
	if err != nil {
		return nil, err
	}
	if tree == nil {
		return nil, nil
	}
	return json.Marshal(tree)
}

// builtinGrammar returns embedded grammar WASM for a language name, if bundled.
func builtinGrammar(name string) ([]byte, bool) {
	switch name {
	case "go":
		return goGrammarWASM, true
	case "python":
		return pythonGrammarWASM, true
	case "javascript":
		return javascriptGrammarWASM, true
	case "typescript":
		return typescriptGrammarWASM, true
	case "tsx":
		return tsxGrammarWASM, true
	}
	return nil, false
}

// loadGrammar dynamically links a grammar side module into this instance's core
// and returns its TSLanguage pointer, caching by name. The raw WASM byte-parsing
// (dylink, imports) may run on untrusted plugin-provided grammars, so a panic on
// a malformed module is recovered into an error rather than crashing the indexer.
func (in *instance) loadGrammar(ctx context.Context, name string, wasmBytes []byte) (lang uint32, err error) {
	if l, ok := in.langs[name]; ok {
		return l, nil
	}
	defer func() {
		if r := recover(); r != nil {
			lang, err = 0, fmt.Errorf("grammar %s: malformed wasm: %v", name, r)
		}
	}()
	memSize, memAlign, tableSize, err := dylinkMemInfo(wasmBytes)
	if err != nil {
		return 0, fmt.Errorf("grammar %s dylink: %w", name, err)
	}

	// memory_base: malloc(mem_size + align), rounded up to mem_align.
	raw, err := in.call(ctx, in.core, "malloc", uint64(memSize+memAlign))
	if err != nil {
		return 0, err
	}
	memBase := (int32(raw) + (memAlign - 1)) &^ (memAlign - 1)

	// table_base: grow the shared indirect function table by table_size.
	base, err := in.call(ctx, in.dl, "dl_table_grow", uint64(tableSize))
	if err != nil {
		return 0, err
	}
	if int32(base) == -1 {
		return 0, fmt.Errorf("grammar %s: table grow failed", name)
	}
	tableBase := int32(base)

	// Resolve the grammar's imports against the core, instantiating per-grammar
	// glue modules, and patch its import module names to the unique glue names.
	patched, err := in.linkGrammar(ctx, name, wasmBytes, memBase, tableBase)
	if err != nil {
		return 0, err
	}
	gm, err := in.rt.InstantiateWithConfig(ctx, patched, wazero.NewModuleConfig().WithName(name+"_grammar"))
	if err != nil {
		return 0, fmt.Errorf("grammar %s instantiate: %w", name, err)
	}
	if _, err := in.call(ctx, gm, "__wasm_apply_data_relocs"); err != nil {
		return 0, fmt.Errorf("grammar %s relocs: %w", name, err)
	}
	// Run static initializers if the (scanner) grammar has any.
	if gm.ExportedFunction("__wasm_call_ctors") != nil {
		if _, err := in.call(ctx, gm, "__wasm_call_ctors"); err != nil {
			return 0, fmt.Errorf("grammar %s ctors: %w", name, err)
		}
	}
	entry, err := in.call(ctx, gm, "tree_sitter_"+name)
	if err != nil {
		return 0, fmt.Errorf("grammar %s entry: %w", name, err)
	}
	in.langs[name] = uint32(entry)
	return uint32(entry), nil
}

// Parse parses source with the named grammar and returns the SyntaxTree, or nil
// if the grammar is not bundled. Checks out a pooled engine instance so calls
// can run concurrently; blocks until one is free or ctx is cancelled.
func (p *Parser) Parse(ctx context.Context, grammarName string, source []byte) (*SyntaxTree, error) {
	wasmBytes, ok := p.grammarWASM(grammarName)
	if !ok {
		return nil, nil
	}
	select {
	case in := <-p.pool:
		defer func() { p.pool <- in }()
		return in.parse(ctx, grammarName, wasmBytes, source)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// parse runs the full dynamic-link + parse on this instance. Not goroutine-safe;
// the pool guarantees a single caller.
func (in *instance) parse(ctx context.Context, grammarName string, wasmBytes, source []byte) (*SyntaxTree, error) {
	lang, err := in.loadGrammar(ctx, grammarName, wasmBytes)
	if err != nil {
		return nil, err
	}

	ps, err := in.call(ctx, in.core, "ts_parser_new")
	if err != nil {
		return nil, err
	}
	defer in.call(ctx, in.core, "ts_parser_delete", ps) //nolint:errcheck
	if _, err := in.call(ctx, in.core, "ts_parser_set_language", ps, uint64(lang)); err != nil {
		return nil, err
	}

	srcPtr, err := in.call(ctx, in.core, "malloc", uint64(len(source)+1))
	if err != nil {
		return nil, err
	}
	defer in.call(ctx, in.core, "free", srcPtr) //nolint:errcheck
	in.core.Memory().Write(uint32(srcPtr), source)
	in.core.Memory().WriteByte(uint32(srcPtr)+uint32(len(source)), 0)

	tree, err := in.call(ctx, in.core, "ts_parser_parse_string", ps, 0, srcPtr, uint64(len(source)))
	if err != nil {
		return nil, err
	}
	if tree == 0 {
		return nil, fmt.Errorf("parse produced no tree")
	}
	defer in.call(ctx, in.core, "ts_tree_delete", tree) //nolint:errcheck

	rootBuf, err := in.call(ctx, in.core, "malloc", nodeSize)
	if err != nil {
		return nil, err
	}
	defer in.call(ctx, in.core, "free", rootBuf) //nolint:errcheck
	if _, err := in.call(ctx, in.core, "ts_tree_root_node", rootBuf, tree); err != nil {
		return nil, err
	}
	root, err := in.serializeNode(ctx, uint32(rootBuf), source, "")
	if err != nil {
		return nil, err
	}
	return &SyntaxTree{Root: root, Source: string(source), Language: grammarName}, nil
}

const (
	nodeSize  = 24 // sizeof(TSNode) on wasm32
	pointSize = 8  // sizeof(TSPoint) = 2×u32
)

// serializeNode mirrors parser.serializeNode but walks the WASM tree via the
// core's exported ts_node_* functions. nodePtr points to a 24-byte TSNode.
func (in *instance) serializeNode(ctx context.Context, nodePtr uint32, source []byte, fieldName string) (*SyntaxNode, error) {
	typ, err := in.readCStr(ctx, in.core, "ts_node_type", uint64(nodePtr))
	if err != nil {
		return nil, err
	}
	named, err := in.call(ctx, in.core, "ts_node_is_named", uint64(nodePtr))
	if err != nil {
		return nil, err
	}
	sb, err := in.call(ctx, in.core, "ts_node_start_byte", uint64(nodePtr))
	if err != nil {
		return nil, err
	}
	eb, err := in.call(ctx, in.core, "ts_node_end_byte", uint64(nodePtr))
	if err != nil {
		return nil, err
	}
	sp, err := in.readPoint(ctx, "ts_node_start_point", uint64(nodePtr))
	if err != nil {
		return nil, err
	}
	ep, err := in.readPoint(ctx, "ts_node_end_point", uint64(nodePtr))
	if err != nil {
		return nil, err
	}

	sn := &SyntaxNode{
		Type:          typ,
		IsNamed:       named != 0,
		Text:          string(source[sb:eb]),
		StartByte:     uint32(sb),
		EndByte:       uint32(eb),
		StartPosition: sp,
		EndPosition:   ep,
	}
	if fieldName != "" {
		sn.FieldName = &fieldName
	}

	count, err := in.call(ctx, in.core, "ts_node_child_count", uint64(nodePtr))
	if err != nil {
		return nil, err
	}
	if count > 0 {
		sn.Children = make([]*SyntaxNode, 0, count)
		for i := uint64(0); i < count; i++ {
			childBuf, err := in.call(ctx, in.core, "malloc", nodeSize)
			if err != nil {
				return nil, err
			}
			if _, err := in.call(ctx, in.core, "ts_node_child", childBuf, uint64(nodePtr), i); err != nil {
				return nil, err
			}
			field, err := in.readCStr(ctx, in.core, "ts_node_field_name_for_child", uint64(nodePtr), i)
			if err != nil {
				return nil, err
			}
			child, err := in.serializeNode(ctx, uint32(childBuf), source, field)
			if err != nil {
				return nil, err
			}
			in.call(ctx, in.core, "free", childBuf) //nolint:errcheck
			sn.Children = append(sn.Children, child)
		}
	}
	return sn, nil
}

func (in *instance) readPoint(ctx context.Context, fn string, nodePtr uint64) (Position, error) {
	buf, err := in.call(ctx, in.core, "malloc", pointSize)
	if err != nil {
		return Position{}, err
	}
	defer in.call(ctx, in.core, "free", buf) //nolint:errcheck
	if _, err := in.call(ctx, in.core, fn, buf, nodePtr); err != nil {
		return Position{}, err
	}
	row, ok1 := in.core.Memory().ReadUint32Le(uint32(buf))
	col, ok2 := in.core.Memory().ReadUint32Le(uint32(buf) + 4)
	if !ok1 || !ok2 {
		return Position{}, fmt.Errorf("read point out of range")
	}
	return Position{Row: row, Column: col}, nil
}

// readCStr calls fn (returning char*) and reads a NUL-terminated string.
// A null (0) result yields "".
func (in *instance) readCStr(ctx context.Context, m api.Module, fn string, args ...uint64) (string, error) {
	ptr, err := in.call(ctx, m, fn, args...)
	if err != nil {
		return "", err
	}
	if ptr == 0 {
		return "", nil
	}
	mem := in.core.Memory()
	var out []byte
	for i := uint32(ptr); ; i++ {
		b, ok := mem.ReadByte(i)
		if !ok {
			return "", fmt.Errorf("read string out of range")
		}
		if b == 0 {
			break
		}
		out = append(out, b)
	}
	return string(out), nil
}

func (in *instance) call(ctx context.Context, m api.Module, fn string, args ...uint64) (uint64, error) {
	f := m.ExportedFunction(fn)
	if f == nil {
		return 0, fmt.Errorf("missing export %q", fn)
	}
	out, err := f.Call(ctx, args...)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", fn, err)
	}
	if len(out) == 0 {
		return 0, nil
	}
	return out[0], nil
}
