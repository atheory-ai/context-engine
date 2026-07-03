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
	// ParseCtx returns a tree even for invalid source (with error nodes), so
	// guard against building a garbled FunctionIntent from malformed input.
	if root.HasError() {
		return nil, fmt.Errorf("source has syntax errors; cannot extract intent")
	}

	funcs := collectFunctions(root, source)
	if len(funcs) == 0 {
		return nil, fmt.Errorf("no function declarations found in source")
	}

	chosen := pickFunction(funcs, targetName)
	imports := collectImports(root, source)
	return buildIntent(chosen, source, imports), nil
}

// ExtractAll extracts a FunctionIntent for every top-level function in the
// source, in source order — the entry point for indexing a whole file. Returns
// an empty slice (not an error) for a file with no functions; rejects malformed
// source as ExtractFunction does.
func ExtractAll(ctx context.Context, source []byte) ([]*FunctionIntent, error) {
	parser := sitter.NewParser()
	parser.SetLanguage(ts.GetLanguage())

	tree, err := parser.ParseCtx(ctx, nil, source)
	if err != nil {
		return nil, fmt.Errorf("parse typescript: %w", err)
	}
	defer tree.Close()

	root := tree.RootNode()
	if root.HasError() {
		return nil, fmt.Errorf("source has syntax errors; cannot extract intent")
	}

	funcs := collectFunctions(root, source)
	imports := collectImports(root, source)
	out := make([]*FunctionIntent, 0, len(funcs))
	for _, fn := range funcs {
		out = append(out, buildIntent(fn, source, imports))
	}
	return out, nil
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
			params:     arrowParams(value),
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
		Behavior:     extractBehavior(fn.body, src),
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
	for _, p := range paramNodes(params) {
		switch p.Type() {
		case "required_parameter", "optional_parameter", "rest_parameter":
			out = append(out, Param{Name: paramName(p, src), Type: paramType(p, src)})
		case "identifier":
			out = append(out, Param{Name: p.Content(src), Type: TypeUnknown})
		}
	}
	return out
}

// paramNodes normalizes the different shapes a parameter list can take: a
// `formal_parameters` container, or a single unparenthesized arrow parameter
// (e.g. `x => x`) that appears as a bare node.
func paramNodes(params *sitter.Node) []*sitter.Node {
	if params == nil {
		return nil
	}
	if params.Type() == "formal_parameters" {
		nodes := make([]*sitter.Node, 0, params.NamedChildCount())
		for i := 0; i < int(params.NamedChildCount()); i++ {
			nodes = append(nodes, params.NamedChild(i))
		}
		return nodes
	}
	return []*sitter.Node{params}
}

// arrowParams resolves an arrow function's parameters across grammar shapes:
// the `parameters` field (parenthesized), the `parameter` field, or a bare
// leading identifier for the unparenthesized single-parameter form.
func arrowParams(fn *sitter.Node) *sitter.Node {
	if p := fn.ChildByFieldName("parameters"); p != nil {
		return p
	}
	if p := fn.ChildByFieldName("parameter"); p != nil {
		return p
	}
	if fn.NamedChildCount() > 0 {
		if first := fn.NamedChild(0); first.Type() == "identifier" {
			return first
		}
	}
	return nil
}

// paramName resolves a parameter's name. It prefers the `pattern` field,
// unwrapping a `rest_pattern` (`...args`) to the bound identifier, and falls
// back to the first identifier for node shapes without a pattern field.
func paramName(p *sitter.Node, src []byte) string {
	if pat := p.ChildByFieldName("pattern"); pat != nil {
		if pat.Type() == "rest_pattern" {
			return firstIdentifier(pat, src)
		}
		return pat.Content(src)
	}
	return firstIdentifier(p, src)
}

// paramType resolves a parameter's type annotation, preferring the `type` field
// and falling back to a `type_annotation` child for node types that omit it.
func paramType(p *sitter.Node, src []byte) string {
	if ann := p.ChildByFieldName("type"); ann != nil {
		return typeAnnotationText(ann, src)
	}
	for i := 0; i < int(p.NamedChildCount()); i++ {
		if c := p.NamedChild(i); c.Type() == "type_annotation" {
			return typeAnnotationText(c, src)
		}
	}
	return TypeUnknown
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

// extractBehavior captures simple conditional branches as behavior clauses, one
// per `if` statement in source order. The when-clause is the condition
// expression; the then-clause summarizes the branch's first return/throw.
//
// It is intentionally shallow — a behavior count and human-readable summary,
// not a control-flow model. Two deliberate boundaries: branches inside nested
// closures belong to those functions, not this one, so the walk does not
// descend into nested function scopes; and a bare `else` fallback (no condition
// of its own) is not counted, so a simple if/else yields one clause, not two.
func extractBehavior(body *sitter.Node, src []byte) []BehaviorClause {
	out := []BehaviorClause{}
	walkWithinFunction(body, func(n *sitter.Node) {
		if n.Type() != "if_statement" {
			return
		}
		out = append(out, BehaviorClause{
			When: conditionText(n.ChildByFieldName("condition"), src),
			Then: summarizeConsequence(n.ChildByFieldName("consequence"), src),
		})
	})
	return out
}

// nestedFunctionTypes introduce a new function scope. The behavior walk stops at
// these so an outer function's branch count excludes branches inside closures.
var nestedFunctionTypes = map[string]bool{
	"function_declaration":           true,
	"function_expression":            true,
	"arrow_function":                 true,
	"method_definition":              true,
	"generator_function":             true,
	"generator_function_declaration": true,
}

// walkWithinFunction is a pre-order walk that does not descend into nested
// function scopes, keeping traversal within the current function body.
func walkWithinFunction(node *sitter.Node, fn func(*sitter.Node)) {
	if node == nil {
		return
	}
	fn(node)
	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if nestedFunctionTypes[child.Type()] {
			continue
		}
		walkWithinFunction(child, fn)
	}
}

// conditionText returns the condition expression, stripping the wrapping
// parentheses of an `if (...)`.
func conditionText(cond *sitter.Node, src []byte) string {
	if cond == nil {
		return ""
	}
	if cond.Type() == "parenthesized_expression" && cond.NamedChildCount() > 0 {
		return normalizeWhitespace(cond.NamedChild(0).Content(src))
	}
	return normalizeWhitespace(cond.Content(src))
}

// summarizeConsequence describes what a branch does: the first return or throw
// it contains, else its first statement.
func summarizeConsequence(cons *sitter.Node, src []byte) string {
	if cons == nil {
		return ""
	}
	if cons.Type() != "statement_block" {
		return trimStatement(normalizeWhitespace(cons.Content(src)))
	}
	var first *sitter.Node
	for i := 0; i < int(cons.NamedChildCount()); i++ {
		c := cons.NamedChild(i)
		if first == nil {
			first = c
		}
		if c.Type() == "return_statement" || c.Type() == "throw_statement" {
			return trimStatement(normalizeWhitespace(c.Content(src)))
		}
	}
	if first != nil {
		return trimStatement(normalizeWhitespace(first.Content(src)))
	}
	return ""
}

// normalizeWhitespace collapses runs of whitespace to single spaces so a
// multi-line condition renders as a stable one-line string.
func normalizeWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func trimStatement(s string) string {
	return strings.TrimSuffix(s, ";")
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

func firstIdentifier(node *sitter.Node, src []byte) string {
	var id string
	walk(node, func(n *sitter.Node) {
		if id == "" && n.Type() == "identifier" {
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
