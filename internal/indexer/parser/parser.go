// Package parser routes files to tree-sitter grammars and produces serialized
// syntax trees for plugin extract() calls.
package parser

import (
	"context"
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

// Parser routes files to grammars and produces serialized SyntaxTree JSON.
// The tree is passed to plugin extract() across the WASM boundary.
type Parser struct {
	grammars *GrammarRegistry
	pool     *parserPool
}

// NewParser creates a Parser backed by the given GrammarRegistry.
func NewParser(grammars *GrammarRegistry) *Parser {
	return &Parser{
		grammars: grammars,
		pool:     newParserPool(runtime.NumCPU()),
	}
}

// ParseTree parses a file and returns the native tree-sitter tree plus the
// grammar used. The caller MUST Close() the returned tree. Returns
// (nil, nil, nil) if no grammar is registered for the extension.
//
// This is the single parse the indexer shares: it serializes the tree for the
// plugin boundary (SerializeTree) and hands the same tree to in-process
// consumers (e.g. IIR extraction) rather than re-parsing.
func (p *Parser) ParseTree(ctx context.Context, filePath string, content []byte) (*sitter.Tree, *Grammar, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	grammar := p.grammars.ForExtension(ext)
	if grammar == nil {
		return nil, nil, nil // no grammar — plugin receives tree: null
	}

	parser := p.pool.Get()
	defer p.pool.Put(parser)

	parser.SetLanguage(grammar.Language)

	tree, err := parser.ParseCtx(ctx, nil, content)
	if err != nil {
		return nil, nil, err
	}
	return tree, grammar, nil
}

// SerializeTree serializes a parsed tree to SyntaxTree JSON for the plugin
// boundary.
func SerializeTree(tree *sitter.Tree, content []byte, grammarName string) ([]byte, error) {
	return json.Marshal(serializeTree(tree, content, grammarName))
}

// Parse parses a file and returns the serialized SyntaxTree as JSON bytes.
// Returns nil if no grammar is registered for this file extension —
// the plugin will receive tree: null in that case.
func (p *Parser) Parse(ctx context.Context, filePath string, content []byte) ([]byte, error) {
	tree, grammar, err := p.ParseTree(ctx, filePath, content)
	if err != nil {
		return nil, err
	}
	if tree == nil {
		return nil, nil
	}
	defer tree.Close()

	return SerializeTree(tree, content, grammar.Name)
}

// parserPool manages a pool of tree-sitter parsers.
// sitter.Parser is not goroutine-safe — pool one per goroutine.
type parserPool struct {
	ch chan *sitter.Parser
}

func newParserPool(size int) *parserPool {
	if size <= 0 {
		size = 1
	}
	p := &parserPool{ch: make(chan *sitter.Parser, size)}
	for i := 0; i < size; i++ {
		p.ch <- sitter.NewParser()
	}
	return p
}

func (p *parserPool) Get() *sitter.Parser       { return <-p.ch }
func (p *parserPool) Put(parser *sitter.Parser) { p.ch <- parser }
