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

// Parser is a WASM-backed tree-sitter parser. Not goroutine-safe: the core runs
// in one shared linear memory, so Parse serializes access with a mutex.
type Parser struct {
	rt    wazero.Runtime
	core  api.Module
	dl    api.Module
	mu    sync.Mutex
	langs map[string]uint32 // grammar name → TSLanguage* (loaded once)
}

// New builds a Parser: instantiates the tree-sitter core and the table-grow
// helper. reference-types (needed by the helper's table.grow) is on by default
// in wazero's core feature set.
func New(ctx context.Context) (*Parser, error) {
	rt := wazero.NewRuntime(ctx)
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
	p := &Parser{
		rt:    rt,
		core:  core,
		dl:    dl,
		langs: map[string]uint32{},
	}
	return p, nil
}

// Close releases the wazero runtime.
func (p *Parser) Close(ctx context.Context) error { return p.rt.Close(ctx) }

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
	name := GrammarForExt(strings.ToLower(filepath.Ext(filePath)))
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

// loadGrammar dynamically links a grammar side module into the core and returns
// its TSLanguage pointer, caching by name. Caller holds p.mu.
func (p *Parser) loadGrammar(ctx context.Context, name string, wasmBytes []byte) (uint32, error) {
	if lang, ok := p.langs[name]; ok {
		return lang, nil
	}
	memSize, memAlign, tableSize, err := dylinkMemInfo(wasmBytes)
	if err != nil {
		return 0, fmt.Errorf("grammar %s dylink: %w", name, err)
	}

	// memory_base: malloc(mem_size + align), rounded up to mem_align.
	raw, err := p.call(ctx, p.core, "malloc", uint64(memSize+memAlign))
	if err != nil {
		return 0, err
	}
	memBase := (int32(raw) + (memAlign - 1)) &^ (memAlign - 1)

	// table_base: grow the shared indirect function table by table_size.
	base, err := p.call(ctx, p.dl, "dl_table_grow", uint64(tableSize))
	if err != nil {
		return 0, err
	}
	if int32(base) == -1 {
		return 0, fmt.Errorf("grammar %s: table grow failed", name)
	}
	tableBase := int32(base)

	// Resolve the grammar's imports against the core, instantiating per-grammar
	// glue modules, and patch its import module names to the unique glue names.
	patched, err := p.linkGrammar(ctx, name, wasmBytes, memBase, tableBase)
	if err != nil {
		return 0, err
	}
	gm, err := p.rt.InstantiateWithConfig(ctx, patched, wazero.NewModuleConfig().WithName(name+"_grammar"))
	if err != nil {
		return 0, fmt.Errorf("grammar %s instantiate: %w", name, err)
	}
	if _, err := p.call(ctx, gm, "__wasm_apply_data_relocs"); err != nil {
		return 0, fmt.Errorf("grammar %s relocs: %w", name, err)
	}
	// Run static initializers if the (scanner) grammar has any.
	if gm.ExportedFunction("__wasm_call_ctors") != nil {
		if _, err := p.call(ctx, gm, "__wasm_call_ctors"); err != nil {
			return 0, fmt.Errorf("grammar %s ctors: %w", name, err)
		}
	}
	lang, err := p.call(ctx, gm, "tree_sitter_"+name)
	if err != nil {
		return 0, fmt.Errorf("grammar %s entry: %w", name, err)
	}
	p.langs[name] = uint32(lang)
	return uint32(lang), nil
}

// Parse parses source with the named grammar and returns the SyntaxTree, or nil
// if the grammar is not bundled.
func (p *Parser) Parse(ctx context.Context, grammarName string, source []byte) (*SyntaxTree, error) {
	wasmBytes, ok := builtinGrammar(grammarName)
	if !ok {
		return nil, nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	lang, err := p.loadGrammar(ctx, grammarName, wasmBytes)
	if err != nil {
		return nil, err
	}

	ps, err := p.call(ctx, p.core, "ts_parser_new")
	if err != nil {
		return nil, err
	}
	defer p.call(ctx, p.core, "ts_parser_delete", ps) //nolint:errcheck
	if _, err := p.call(ctx, p.core, "ts_parser_set_language", ps, uint64(lang)); err != nil {
		return nil, err
	}

	srcPtr, err := p.call(ctx, p.core, "malloc", uint64(len(source)+1))
	if err != nil {
		return nil, err
	}
	defer p.call(ctx, p.core, "free", srcPtr) //nolint:errcheck
	p.core.Memory().Write(uint32(srcPtr), source)
	p.core.Memory().WriteByte(uint32(srcPtr)+uint32(len(source)), 0)

	tree, err := p.call(ctx, p.core, "ts_parser_parse_string", ps, 0, srcPtr, uint64(len(source)))
	if err != nil {
		return nil, err
	}
	if tree == 0 {
		return nil, fmt.Errorf("parse produced no tree")
	}
	defer p.call(ctx, p.core, "ts_tree_delete", tree) //nolint:errcheck

	rootBuf, err := p.call(ctx, p.core, "malloc", nodeSize)
	if err != nil {
		return nil, err
	}
	defer p.call(ctx, p.core, "free", rootBuf) //nolint:errcheck
	if _, err := p.call(ctx, p.core, "ts_tree_root_node", rootBuf, tree); err != nil {
		return nil, err
	}
	root, err := p.serializeNode(ctx, uint32(rootBuf), source, "")
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
func (p *Parser) serializeNode(ctx context.Context, nodePtr uint32, source []byte, fieldName string) (*SyntaxNode, error) {
	typ, err := p.readCStr(ctx, p.core, "ts_node_type", uint64(nodePtr))
	if err != nil {
		return nil, err
	}
	named, err := p.call(ctx, p.core, "ts_node_is_named", uint64(nodePtr))
	if err != nil {
		return nil, err
	}
	sb, err := p.call(ctx, p.core, "ts_node_start_byte", uint64(nodePtr))
	if err != nil {
		return nil, err
	}
	eb, err := p.call(ctx, p.core, "ts_node_end_byte", uint64(nodePtr))
	if err != nil {
		return nil, err
	}
	sp, err := p.readPoint(ctx, "ts_node_start_point", uint64(nodePtr))
	if err != nil {
		return nil, err
	}
	ep, err := p.readPoint(ctx, "ts_node_end_point", uint64(nodePtr))
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

	count, err := p.call(ctx, p.core, "ts_node_child_count", uint64(nodePtr))
	if err != nil {
		return nil, err
	}
	if count > 0 {
		sn.Children = make([]*SyntaxNode, 0, count)
		for i := uint64(0); i < count; i++ {
			childBuf, err := p.call(ctx, p.core, "malloc", nodeSize)
			if err != nil {
				return nil, err
			}
			if _, err := p.call(ctx, p.core, "ts_node_child", childBuf, uint64(nodePtr), i); err != nil {
				return nil, err
			}
			field, err := p.readCStr(ctx, p.core, "ts_node_field_name_for_child", uint64(nodePtr), i)
			if err != nil {
				return nil, err
			}
			child, err := p.serializeNode(ctx, uint32(childBuf), source, field)
			if err != nil {
				return nil, err
			}
			p.call(ctx, p.core, "free", childBuf) //nolint:errcheck
			sn.Children = append(sn.Children, child)
		}
	}
	return sn, nil
}

func (p *Parser) readPoint(ctx context.Context, fn string, nodePtr uint64) (Position, error) {
	buf, err := p.call(ctx, p.core, "malloc", pointSize)
	if err != nil {
		return Position{}, err
	}
	defer p.call(ctx, p.core, "free", buf) //nolint:errcheck
	if _, err := p.call(ctx, p.core, fn, buf, nodePtr); err != nil {
		return Position{}, err
	}
	row, ok1 := p.core.Memory().ReadUint32Le(uint32(buf))
	col, ok2 := p.core.Memory().ReadUint32Le(uint32(buf) + 4)
	if !ok1 || !ok2 {
		return Position{}, fmt.Errorf("read point out of range")
	}
	return Position{Row: row, Column: col}, nil
}

// readCStr calls fn (returning char*) and reads a NUL-terminated string.
// A null (0) result yields "".
func (p *Parser) readCStr(ctx context.Context, m api.Module, fn string, args ...uint64) (string, error) {
	ptr, err := p.call(ctx, m, fn, args...)
	if err != nil {
		return "", err
	}
	if ptr == 0 {
		return "", nil
	}
	mem := p.core.Memory()
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

func (p *Parser) call(ctx context.Context, m api.Module, fn string, args ...uint64) (uint64, error) {
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
