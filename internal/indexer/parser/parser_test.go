package parser

import (
	"bytes"
	"context"
	"testing"
)

func TestParseTree_AndSerializeMatchParse(t *testing.T) {
	p := NewParser(NewGrammarRegistry())
	content := []byte("export function f(x: number): number { return x; }")

	// Parse once (the shared path) and serialize.
	tree, grammar, err := p.ParseTree(context.Background(), "f.ts", content)
	if err != nil {
		t.Fatalf("ParseTree: %v", err)
	}
	if tree == nil {
		t.Fatal("expected a tree for a .ts file")
	}
	defer tree.Close()
	if tree.RootNode().Type() != "program" {
		t.Errorf("root type = %q, want program", tree.RootNode().Type())
	}
	shared, err := SerializeTree(tree, content, grammar.Name)
	if err != nil {
		t.Fatalf("SerializeTree: %v", err)
	}

	// Parse (the serialized convenience path) must produce identical JSON.
	viaParse, err := p.Parse(context.Background(), "f.ts", content)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !bytes.Equal(shared, viaParse) {
		t.Error("SerializeTree(ParseTree) diverged from Parse")
	}
}

func TestParseTree_NoGrammar(t *testing.T) {
	p := NewParser(NewGrammarRegistry())
	tree, grammar, err := p.ParseTree(context.Background(), "notes.txt", []byte("hello"))
	if err != nil {
		t.Fatalf("ParseTree: %v", err)
	}
	if tree != nil || grammar != nil {
		t.Error("expected nil tree/grammar for an unsupported extension")
	}
}
