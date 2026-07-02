package iir

import (
	"context"
	"fmt"
	"sort"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	ts "github.com/smacker/go-tree-sitter/typescript/typescript"
)

// sideEffectVerbs are substrings that, when present in a called method name,
// mark the call as an observable side effect. The extractor prefers false
// positives over false negatives, per the Slice 4 heuristic.
var sideEffectVerbs = []string{
	"track", "send", "emit", "publish", "save", "create", "update", "delete", "write",
}

// ExtractFunction parses TypeScript source and extracts the FunctionIntent for
// the named function. When targetName is empty or not found, it falls back to
// the first exported function (then the first function) so a name mismatch can
// still be surfaced by comparison rather than lost as a hard error.
//
// Extraction is deterministic: inputs follow source order; side effects and
// failure modes are de-duplicated and sorted.
func ExtractFunction(ctx context.Context, source []byte, targetName string) (*FunctionIntent, error) {
	parser := sitter.NewParser()
	parser.SetLanguage(ts.GetLanguage())

	tree, err := parser.ParseCtx(ctx, nil, source)
	if err != nil {
		return nil, fmt.Errorf("parse typescript: %w", err)
	}
	defer tree.Close()

	root := tree.RootNode()
	funcs := collectFunctions(root, source)
	if len(funcs) == 0 {
		return nil, fmt.Errorf("no function declarations found in source")
	}

	chosen := pickFunction(funcs, targetName)
	imports := collectImports(root, source)
	return buildIntent(chosen, source, imports), nil
}

// funcCandidate is a function-like declaration located in the CST.
type funcCandidate struct {
	name       string
	exported   bool
	params     *sitter.Node // formal_parameters
	returnType *sitter.Node // type_annotation, or nil when absent
	body       *sitter.Node // statement_block or arrow-function expression
}

// pickFunction selects the extraction target: exact name match first, then the
// first exported function, then the first function.
func pickFunction(funcs []funcCandidate, targetName string) funcCandidate {
	if targetName != "" {
		for _, f := range funcs {
			if f.name == targetName {
				return f
			}
		}
	}
	for _, f := range funcs {
		if f.exported {
			return f
		}
	}
	return funcs[0]
}

// collectFunctions gathers top-level function declarations and arrow functions
// bound to exported/const variables.
func collectFunctions(root *sitter.Node, src []byte) []funcCandidate {
	var out []funcCandidate
	for i := 0; i < int(root.NamedChildCount()); i++ {
		child := root.NamedChild(i)
		switch child.Type() {
		case "function_declaration":
			out = append(out, fromFunctionDeclaration(child, src, false)...)
		case "export_statement":
			out = append(out, fromExportStatement(child, src)...)
		case "lexical_declaration", "variable_declaration":
			out = append(out, fromVariableDeclaration(child, src, false)...)
		}
	}
	return out
}

func fromExportStatement(node *sitter.Node, src []byte) []funcCandidate {
	var out []funcCandidate
	for i := 0; i < int(node.NamedChildCount()); i++ {
		inner := node.NamedChild(i)
		switch inner.Type() {
		case "function_declaration":
			out = append(out, fromFunctionDeclaration(inner, src, true)...)
		case "lexical_declaration", "variable_declaration":
			out = append(out, fromVariableDeclaration(inner, src, true)...)
		}
	}
	return out
}

func fromFunctionDeclaration(node *sitter.Node, src []byte, exported bool) []funcCandidate {
	name := nodeFieldText(node, "name", src)
	return []funcCandidate{{
		name:       name,
		exported:   exported,
		params:     node.ChildByFieldName("parameters"),
		returnType: node.ChildByFieldName("return_type"),
		body:       node.ChildByFieldName("body"),
	}}
}

func fromVariableDeclaration(node *sitter.Node, src []byte, exported bool) []funcCandidate {
	var out []funcCandidate
	for i := 0; i < int(node.NamedChildCount()); i++ {
		declr := node.NamedChild(i)
		if declr.Type() != "variable_declarator" {
			continue
		}
		value := declr.ChildByFieldName("value")
		if value == nil || value.Type() != "arrow_function" {
			continue
		}
		out = append(out, funcCandidate{
			name:       nodeFieldText(declr, "name", src),
			exported:   exported,
			params:     value.ChildByFieldName("parameters"),
			returnType: value.ChildByFieldName("return_type"),
			body:       value.ChildByFieldName("body"),
		})
	}
	return out
}

// buildIntent converts a located function into a FunctionIntent.
func buildIntent(fn funcCandidate, src []byte, imports map[string]bool) *FunctionIntent {
	visibility := VisibilityPrivate
	if fn.exported {
		visibility = VisibilityPublic
	}

	intent := &FunctionIntent{
		Kind:         KindFunctionIntent,
		Name:         fn.name,
		Language:     "typescript",
		Visibility:   visibility,
		Inputs:       extractParams(fn.params, src),
		Returns:      extractReturn(fn.returnType, src),
		Behavior:     []BehaviorClause{},
		SideEffects:  extractSideEffects(fn.body, src, imports),
		FailureModes: extractFailureModes(fn.body, src),
		Constraints:  []string{},
	}
	// Preserve the "explicitly declares no side effects" distinction: an
	// extracted function with no effects yields an empty, non-nil slice, and
	// the report shows [] rather than null for a stable shape.
	if intent.SideEffects == nil {
		intent.SideEffects = []string{}
	}
	if intent.FailureModes == nil {
		intent.FailureModes = []string{}
	}
	return intent
}

func extractParams(params *sitter.Node, src []byte) []Param {
	out := []Param{}
	if params == nil {
		return out
	}
	for i := 0; i < int(params.NamedChildCount()); i++ {
		p := params.NamedChild(i)
		switch p.Type() {
		case "required_parameter", "optional_parameter":
			name := nodeFieldText(p, "pattern", src)
			typ := TypeUnknown
			if ann := p.ChildByFieldName("type"); ann != nil {
				typ = typeAnnotationText(ann, src)
			}
			out = append(out, Param{Name: name, Type: typ})
		case "identifier":
			out = append(out, Param{Name: p.Content(src), Type: TypeUnknown})
		}
	}
	return out
}

func extractReturn(returnType *sitter.Node, src []byte) Return {
	if returnType == nil {
		return Return{Type: "", Explicit: false}
	}
	return Return{Type: typeAnnotationText(returnType, src), Explicit: true}
}

// extractSideEffects walks the body for call expressions that look like
// observable effects: calls on imported clients/services, or calls whose method
// name contains a side-effect verb.
func extractSideEffects(body *sitter.Node, src []byte, imports map[string]bool) []string {
	seen := map[string]bool{}
	walk(body, func(n *sitter.Node) {
		if n.Type() != "call_expression" {
			return
		}
		callee := n.ChildByFieldName("function")
		if callee == nil {
			return
		}
		method, rootObj, full := calleeParts(callee, src)
		// A call is a side effect when it targets an imported client/service
		// (`client.method()`) or when the method name contains a side-effect
		// verb. Bare calls to imported helpers (e.g. `ok`, `err`) are not
		// treated as effects — only member calls on imported objects are.
		if imports[rootObj] || matchesSideEffectVerb(method) {
			seen[full] = true
		}
	})
	return sortedKeys(seen)
}

// extractFailureModes captures string-literal arguments of thrown errors as a
// conservative approximation of declared failure outcomes.
func extractFailureModes(body *sitter.Node, src []byte) []string {
	seen := map[string]bool{}
	walk(body, func(n *sitter.Node) {
		if n.Type() != "throw_statement" {
			return
		}
		if lit := firstStringLiteral(n, src); lit != "" {
			seen[lit] = true
		}
	})
	return sortedKeys(seen)
}

// calleeParts returns the called method name, the root object identifier, and
// the full callee text (e.g. method "track", root "analytics", full
// "analytics.track").
func calleeParts(callee *sitter.Node, src []byte) (method, rootObj, full string) {
	full = callee.Content(src)
	switch callee.Type() {
	case "identifier":
		return callee.Content(src), "", full
	case "member_expression":
		prop := callee.ChildByFieldName("property")
		if prop != nil {
			method = prop.Content(src)
		}
		rootObj = leftmostIdentifier(callee.ChildByFieldName("object"), src)
		return method, rootObj, full
	default:
		return "", "", full
	}
}

func leftmostIdentifier(node *sitter.Node, src []byte) string {
	for node != nil {
		switch node.Type() {
		case "identifier":
			return node.Content(src)
		case "member_expression":
			node = node.ChildByFieldName("object")
		default:
			return ""
		}
	}
	return ""
}

func matchesSideEffectVerb(method string) bool {
	lower := strings.ToLower(method)
	for _, v := range sideEffectVerbs {
		if strings.Contains(lower, v) {
			return true
		}
	}
	return false
}

// collectImports returns the set of identifiers introduced by import
// statements: named, default, and namespace imports.
func collectImports(root *sitter.Node, src []byte) map[string]bool {
	imports := map[string]bool{}
	for i := 0; i < int(root.NamedChildCount()); i++ {
		node := root.NamedChild(i)
		if node.Type() != "import_statement" {
			continue
		}
		walk(node, func(n *sitter.Node) {
			switch n.Type() {
			case "import_specifier":
				// Prefer the local alias when present, else the imported name.
				if alias := n.ChildByFieldName("alias"); alias != nil {
					imports[alias.Content(src)] = true
				} else if name := n.ChildByFieldName("name"); name != nil {
					imports[name.Content(src)] = true
				}
			case "namespace_import":
				if id := lastIdentifier(n, src); id != "" {
					imports[id] = true
				}
			case "identifier":
				// default import: `import Foo from "..."`
				if n.Parent() != nil && n.Parent().Type() == "import_clause" {
					imports[n.Content(src)] = true
				}
			}
		})
	}
	return imports
}

// --- small CST helpers -----------------------------------------------------

// walk invokes fn for node and every descendant in a deterministic pre-order.
func walk(node *sitter.Node, fn func(*sitter.Node)) {
	if node == nil {
		return
	}
	fn(node)
	for i := 0; i < int(node.NamedChildCount()); i++ {
		walk(node.NamedChild(i), fn)
	}
}

func nodeFieldText(node *sitter.Node, field string, src []byte) string {
	if child := node.ChildByFieldName(field); child != nil {
		return child.Content(src)
	}
	return ""
}

// typeAnnotationText returns the type inside a `type_annotation` node, stripping
// the leading colon.
func typeAnnotationText(ann *sitter.Node, src []byte) string {
	if ann.Type() == "type_annotation" && ann.NamedChildCount() > 0 {
		return strings.TrimSpace(ann.NamedChild(0).Content(src))
	}
	return strings.TrimSpace(strings.TrimPrefix(ann.Content(src), ":"))
}

func firstStringLiteral(node *sitter.Node, src []byte) string {
	var found string
	walk(node, func(n *sitter.Node) {
		if found != "" {
			return
		}
		if n.Type() == "string" {
			found = strings.Trim(n.Content(src), "'\"`")
		}
	})
	return found
}

func lastIdentifier(node *sitter.Node, src []byte) string {
	var id string
	walk(node, func(n *sitter.Node) {
		if n.Type() == "identifier" {
			id = n.Content(src)
		}
	})
	return id
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
