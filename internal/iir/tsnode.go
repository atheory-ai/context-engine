package iir

import (
	"context"
	"sync"

	"github.com/atheory-ai/context-engine/internal/indexer/wasmparse"
)

// tsNode adapts a wasmparse.SyntaxNode to the small subset of the tree-sitter
// node API the extractor uses, so the extraction logic is backend-agnostic
// (pure-Go WASM tree-sitter instead of CGO). A nil *tsNode models an absent
// node (as the CGO binding returned nil).
type tsNode struct{ n *wasmparse.SyntaxNode }

func wrap(n *wasmparse.SyntaxNode) *tsNode {
	if n == nil {
		return nil
	}
	return &tsNode{n}
}

func (t *tsNode) Type() string { return t.n.Type }

// Content returns the node's source text. The SyntaxNode already carries the
// exact source slice, so the src argument is accepted for API parity but unused.
func (t *tsNode) Content([]byte) string { return t.n.Text }

func (t *tsNode) StartByte() uint32 { return t.n.StartByte }

func (t *tsNode) ChildByFieldName(name string) *tsNode {
	for _, c := range t.n.Children {
		if c.FieldName != nil && *c.FieldName == name {
			return wrap(c)
		}
	}
	return nil
}

func (t *tsNode) NamedChildCount() uint32 {
	var n uint32
	for _, c := range t.n.Children {
		if c.IsNamed {
			n++
		}
	}
	return n
}

func (t *tsNode) NamedChild(i int) *tsNode {
	idx := 0
	for _, c := range t.n.Children {
		if !c.IsNamed {
			continue
		}
		if idx == i {
			return wrap(c)
		}
		idx++
	}
	return nil
}

// hasError reports whether the subtree contains an ERROR or MISSING node, the
// equivalent of tree-sitter's Node.HasError for malformed input.
func hasError(t *tsNode) bool {
	if t == nil {
		return false
	}
	if t.n.Type == "ERROR" {
		return true
	}
	for _, c := range t.n.Children {
		if hasError(&tsNode{c}) {
			return true
		}
	}
	return false
}

// Shared pure-Go tree-sitter parser for the standalone extractor (ce iir verify,
// ce.iir_* host functions). Lazily created; Parse serializes internally.
var (
	wpOnce sync.Once
	wp     *wasmparse.Parser
	wpErr  error
)

func tsParse(ctx context.Context, source []byte) (*tsNode, error) {
	wpOnce.Do(func() { wp, wpErr = wasmparse.New(context.Background()) })
	if wpErr != nil {
		return nil, wpErr
	}
	tree, err := wp.Parse(ctx, "typescript", source)
	if err != nil {
		return nil, err
	}
	if tree == nil {
		return nil, nil
	}
	return wrap(tree.Root), nil
}
