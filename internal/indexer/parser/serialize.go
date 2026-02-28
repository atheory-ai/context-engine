package parser

import (
	sitter "github.com/smacker/go-tree-sitter"
)

// SyntaxTree mirrors the TypeScript SyntaxTree type in @ce/plugin-sdk.
// This is the JSON structure passed to plugin extract() across the WASM boundary.
// Must stay in sync with the SyntaxTree interface defined in spec-9-indexer.md.
type SyntaxTree struct {
	Root     *SyntaxNode `json:"root"`
	Source   string      `json:"source"`
	Language string      `json:"language"`
}

// SyntaxNode is a single node in the syntax tree.
type SyntaxNode struct {
	Type          string        `json:"type"`
	IsNamed       bool          `json:"isNamed"`
	FieldName     *string       `json:"fieldName"`
	Text          string        `json:"text"`
	StartByte     uint32        `json:"startByte"`
	EndByte       uint32        `json:"endByte"`
	StartPosition Position      `json:"startPosition"`
	EndPosition   Position      `json:"endPosition"`
	Children      []*SyntaxNode `json:"children"`
}

// Position is a row/column location in source (0-indexed).
type Position struct {
	Row    uint32 `json:"row"`
	Column uint32 `json:"column"`
}

// serializeTree converts a tree-sitter parse tree to the SyntaxTree JSON format.
func serializeTree(tree *sitter.Tree, source []byte, language string) *SyntaxTree {
	return &SyntaxTree{
		Root:     serializeNode(tree.RootNode(), source, ""),
		Source:   string(source),
		Language: language,
	}
}

// serializeNode recursively serializes a tree-sitter node.
func serializeNode(node *sitter.Node, source []byte, fieldName string) *SyntaxNode {
	if node == nil {
		return nil
	}

	sn := &SyntaxNode{
		Type:      node.Type(),
		IsNamed:   node.IsNamed(),
		Text:      string(source[node.StartByte():node.EndByte()]),
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

	childCount := int(node.ChildCount())
	if childCount > 0 {
		sn.Children = make([]*SyntaxNode, 0, childCount)
		for i := 0; i < childCount; i++ {
			child := node.Child(i)
			field := node.FieldNameForChild(i)
			sn.Children = append(sn.Children, serializeNode(child, source, field))
		}
	}

	return sn
}
