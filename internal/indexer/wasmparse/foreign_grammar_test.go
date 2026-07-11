package wasmparse

import (
	"context"
	_ "embed"
	"encoding/json"
	"testing"
)

// rustGrammarWASM is a genuinely FOREIGN grammar side module — Rust is not among
// the engine's bundled grammars (go/python/javascript/typescript/tsx) and carries
// its own external scanner. Built from the pinned malivvan tree-sitter sources by
// testdata/build.sh; committed so the test needs no toolchain. See
// docs/specs/18-spec-wasm-grammar-loader.md.
//
//go:embed testdata/rust.wasm
var rustGrammarWASM []byte

// TestRegisterForeignGrammar proves the headline pluggable-grammar capability:
// loading a grammar the engine has NEVER seen — foreign node types, a foreign
// external scanner — at runtime, then parsing it to a correct CST. Unlike
// TestRegisterGrammar (which re-registers the already-bundled Go grammar under a
// fake extension), this exercises a real non-bundled language end-to-end.
func TestRegisterForeignGrammar(t *testing.T) {
	ctx := context.Background()
	p, err := New(ctx, "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer p.Close(ctx)

	name, err := p.RegisterGrammar([]string{".rs"}, rustGrammarWASM)
	if err != nil {
		t.Fatalf("RegisterGrammar(rust): %v", err)
	}
	if name != "rust" {
		t.Fatalf("detected name = %q, want rust (from tree_sitter_rust export)", name)
	}
	// The extension is not a builtin — it routes purely through the runtime registry.
	if GrammarForExt(".rs") != "" {
		t.Fatal(".rs should not be a builtin extension")
	}

	src := []byte("struct Point {\n    x: i32,\n    y: i32,\n}\n\nfn add(a: i32, b: i32) -> i32 {\n    a + b\n}\n")
	treeJSON, err := p.ParseFile(ctx, "lib.rs", src)
	if err != nil {
		t.Fatalf("ParseFile(lib.rs): %v", err)
	}
	if treeJSON == nil {
		t.Fatal("nil tree from foreign grammar")
	}
	var tree SyntaxTree
	if err := json.Unmarshal(treeJSON, &tree); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if tree.Root.Type != "source_file" {
		t.Fatalf("root = %q, want source_file", tree.Root.Type)
	}

	// Foreign node types the engine has no built-in knowledge of.
	st := child(tree.Root, "struct_item")
	if st == nil {
		t.Fatal("no struct_item — foreign grammar did not parse Rust struct")
	}
	if n := field(st, "name"); n == nil || n.Type != "type_identifier" || n.Text != "Point" {
		t.Fatalf("struct name field = %+v, want type_identifier 'Point'", n)
	}

	fn := child(tree.Root, "function_item")
	if fn == nil {
		t.Fatal("no function_item — foreign grammar did not parse Rust fn")
	}
	if n := field(fn, "name"); n == nil || n.Type != "identifier" || n.Text != "add" {
		t.Fatalf("fn name field = %+v, want identifier 'add'", n)
	}
	// The external scanner + grammar must resolve the return type field.
	if rt := field(fn, "return_type"); rt == nil {
		t.Fatal("no return_type field on function_item — external scanner path likely broken")
	}
}
