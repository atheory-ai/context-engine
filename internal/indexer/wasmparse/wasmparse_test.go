package wasmparse

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
)

func child(n *SyntaxNode, typ string) *SyntaxNode {
	for _, c := range n.Children {
		if c.Type == typ {
			return c
		}
	}
	return nil
}

func field(n *SyntaxNode, name string) *SyntaxNode {
	for _, c := range n.Children {
		if c.FieldName != nil && *c.FieldName == name {
			return c
		}
	}
	return nil
}

func TestParseGo(t *testing.T) {
	ctx := context.Background()
	p, err := New(ctx, "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer p.Close(ctx)

	src := []byte("package main\n\nfunc add(a, b int) int {\n\treturn a + b\n}\n")
	tree, err := p.Parse(ctx, "go", src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if tree == nil {
		t.Fatal("nil tree")
	}
	if tree.Language != "go" {
		t.Errorf("language = %q, want go", tree.Language)
	}
	if tree.Root.Type != "source_file" {
		t.Fatalf("root type = %q, want source_file", tree.Root.Type)
	}

	fn := child(tree.Root, "function_declaration")
	if fn == nil {
		t.Fatal("no function_declaration")
	}
	// field names must be resolved
	name := field(fn, "name")
	if name == nil || name.Type != "identifier" || name.Text != "add" {
		t.Fatalf("name field = %+v, want identifier 'add'", name)
	}
	if params := field(fn, "parameters"); params == nil || params.Type != "parameter_list" {
		t.Fatalf("parameters field = %+v", params)
	}
	if result := field(fn, "result"); result == nil || result.Type != "type_identifier" {
		t.Fatalf("result field = %+v", result)
	}
	body := field(fn, "body")
	if body == nil || body.Type != "block" {
		t.Fatalf("body field = %+v", body)
	}

	// positions: func is on line index 2 (third line)
	if fn.StartPosition.Row != 2 {
		t.Errorf("func start row = %d, want 2", fn.StartPosition.Row)
	}
	// text slices align with byte offsets
	if got := src[fn.StartByte:fn.EndByte]; string(got[:4]) != "func" {
		t.Errorf("func text starts with %q", string(got[:4]))
	}

	// named flag
	if !name.IsNamed {
		t.Error("identifier should be named")
	}

	// must marshal to JSON (the plugin boundary)
	if _, err := json.Marshal(tree); err != nil {
		t.Fatalf("marshal: %v", err)
	}
}

// TestRegisterGrammar registers a grammar at runtime under a novel extension
// and parses through it, exercising the plugin-provided-grammar path.
func TestRegisterGrammar(t *testing.T) {
	ctx := context.Background()
	p, err := New(ctx, "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer p.Close(ctx)

	// Register the Go grammar wasm under a made-up extension. The name must be
	// auto-detected from its tree_sitter_go export.
	name, err := p.RegisterGrammar([]string{".gox"}, goGrammarWASM)
	if err != nil {
		t.Fatalf("RegisterGrammar: %v", err)
	}
	if name != "go" {
		t.Fatalf("detected name = %q, want go", name)
	}
	// Route the novel extension through the runtime registry (not a builtin).
	if GrammarForExt(".gox") != "" {
		t.Fatal(".gox should not be a builtin extension")
	}
	tree, err := p.ParseFile(ctx, "f.gox", []byte("package p\nfunc f() {}\n"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if tree == nil {
		t.Fatal("nil tree from registered grammar")
	}
	var st SyntaxTree
	if err := json.Unmarshal(tree, &st); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if st.Root.Type != "source_file" {
		t.Errorf("root = %q, want source_file", st.Root.Type)
	}
	// A non-grammar wasm is rejected.
	if _, err := p.RegisterGrammar([]string{".bad"}, []byte("\x00asm\x01\x00\x00\x00")); err == nil {
		t.Error("expected error registering a non-grammar wasm")
	}
	// A truncated/malformed module must be rejected, not panic (untrusted input):
	// an import section claiming a 16-byte module name with only one byte present.
	truncated := []byte{0x00, 'a', 's', 'm', 1, 0, 0, 0, 0x02, 0x05, 0x01, 0x10, 0x61}
	if _, err := p.RegisterGrammar([]string{".bad2"}, truncated); err == nil {
		t.Error("expected error registering a truncated wasm")
	}
}

// TestParseConcurrent hammers one Parser from many goroutines to exercise the
// instance pool (run with -race). Each parse must return a correct tree.
func TestParseConcurrent(t *testing.T) {
	ctx := context.Background()
	p, err := New(ctx, "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer p.Close(ctx)

	langs := []struct{ name, src, root string }{
		{"go", "package p\nfunc f() {}\n", "source_file"},
		{"python", "def g():\n    pass\n", "module"},
		{"typescript", "export function h(): void {}\n", "program"},
	}
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tc := langs[i%len(langs)]
			tree, err := p.Parse(ctx, tc.name, []byte(tc.src))
			if err != nil {
				t.Errorf("%s: %v", tc.name, err)
				return
			}
			if tree == nil || tree.Root.Type != tc.root {
				t.Errorf("%s: bad tree %+v", tc.name, tree)
			}
		}(i)
	}
	wg.Wait()
}

// TestParseAllLanguages loads every bundled grammar in one Parser (exercising
// multi-grammar coexistence + scanner grammars that need GOT.mem/stack-pointer
// resolution) and checks each yields a sensible tree.
func TestParseAllLanguages(t *testing.T) {
	cases := []struct {
		lang, src, rootType, fnType string
	}{
		{"go", "package main\nfunc add(a int) int { return a }\n", "source_file", "function_declaration"},
		{"python", "def find(id):\n    return id\n", "module", "function_definition"},
		{"javascript", "function add(a, b) { return a + b }\n", "program", "function_declaration"},
		{"typescript", "function add(a: number): number { return a }\n", "program", "function_declaration"},
		{"tsx", "const x = <div className=\"a\">hi</div>\n", "program", ""},
	}
	ctx := context.Background()
	p, err := New(ctx, "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer p.Close(ctx)

	for _, tc := range cases {
		t.Run(tc.lang, func(t *testing.T) {
			tree, err := p.Parse(ctx, tc.lang, []byte(tc.src))
			if err != nil {
				t.Fatalf("Parse %s: %v", tc.lang, err)
			}
			if tree == nil {
				t.Fatalf("%s: nil tree", tc.lang)
			}
			if tree.Root.Type != tc.rootType {
				t.Errorf("%s root = %q, want %q", tc.lang, tree.Root.Type, tc.rootType)
			}
			// no ERROR nodes at top level (scanner working)
			for _, c := range tree.Root.Children {
				if c.Type == "ERROR" {
					t.Errorf("%s: parse produced ERROR node", tc.lang)
				}
			}
			if tc.fnType != "" && child(tree.Root, tc.fnType) == nil {
				t.Errorf("%s: no %s in tree", tc.lang, tc.fnType)
			}
		})
	}
}
