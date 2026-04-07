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

// Parse parses a file and returns the serialized SyntaxTree as JSON bytes.
// Returns nil if no grammar is registered for this file extension —
// the plugin will receive tree: null in that case.
func (p *Parser) Parse(ctx context.Context, filePath string, content []byte) ([]byte, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	grammar := p.grammars.ForExtension(ext)
	if grammar == nil {
		return nil, nil // no grammar — plugin receives tree: null
	}

	parser := p.pool.Get()
	defer p.pool.Put(parser)

	parser.SetLanguage(grammar.Language)

	tree, err := parser.ParseCtx(ctx, nil, content)
	if err != nil {
		return nil, err
	}
	defer tree.Close()

	syntaxTree := serializeTree(tree, content, grammar.Name)
	return json.Marshal(syntaxTree)
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
