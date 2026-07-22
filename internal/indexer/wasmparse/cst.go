package wasmparse

// SyntaxTree / SyntaxNode / Position are the plugin-boundary JSON shape (matching
// the @ce/plugin-sdk SyntaxTree interface via json tags) so plugin extract()
// calls are backend-agnostic.
type SyntaxTree struct {
	Root     *SyntaxNode `json:"root"`
	Source   string      `json:"source,omitempty"`
	Language string      `json:"language"`
}

type SyntaxNode struct {
	Type          string        `json:"type"`
	IsNamed       bool          `json:"isNamed"`
	FieldName     *string       `json:"fieldName"`
	Text          string        `json:"text,omitempty"`
	StartByte     uint32        `json:"startByte"`
	EndByte       uint32        `json:"endByte"`
	StartPosition Position      `json:"startPosition"`
	EndPosition   Position      `json:"endPosition"`
	Children      []*SyntaxNode `json:"children"`
}

type Position struct {
	Row    uint32 `json:"row"`
	Column uint32 `json:"column"`
}
