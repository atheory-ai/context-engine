package iir

import (
	"context"
	"fmt"
	"sort"
	"strings"
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
	root, err := tsParse(ctx, source)
	if err != nil {
		return nil, fmt.Errorf("parse typescript: %w", err)
	}
	// Parsing returns a tree even for invalid source (with error nodes), so
	// guard against building a garbled FunctionIntent from malformed input.
	if root == nil || hasError(root) {
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
	root, err := tsParse(ctx, source)
	if err != nil {
		return nil, fmt.Errorf("parse typescript: %w", err)
	}
	fns, err := ExtractAllFromNode(root, source)
	if err != nil {
		return nil, err
	}
	out := make([]*FunctionIntent, len(fns))
	for i, f := range fns {
		out[i] = f.Intent
	}
	return out, nil
}

// ExtractedFunction pairs an extracted FunctionIntent with the start byte of its
// declaration node, so a consumer can correlate it to the structural symbol node
// the language plugin emitted for the same function.
type ExtractedFunction struct {
	Intent    *FunctionIntent
	StartByte uint32
}

// ExtractAllFromNode extracts an ExtractedFunction per top-level function from an
// already-parsed tree-sitter root node, so the indexer can reuse the parse it
// already did rather than re-parsing. source is the file bytes the node spans;
// the node must not be closed while this runs.
func ExtractAllFromNode(root *tsNode, source []byte) ([]ExtractedFunction, error) {
	if root == nil {
		return nil, fmt.Errorf("nil root node")
	}
	if hasError(root) {
		return nil, fmt.Errorf("source has syntax errors; cannot extract intent")
	}

	funcs := collectFunctions(root, source)
	imports := collectImports(root, source)
	out := make([]ExtractedFunction, 0, len(funcs))
	for _, fn := range funcs {
		out = append(out, ExtractedFunction{
			Intent:    buildIntent(fn, source, imports),
			StartByte: fn.startByte,
		})
	}
	return out, nil
}

// funcCandidate is a function-like declaration located in the CST.
type funcCandidate struct {
	name       string
	exported   bool
	startByte  uint32  // start byte of the declaration node, for node correlation
	params     *tsNode // formal_parameters
	returnType *tsNode // type_annotation, or nil when absent
	body       *tsNode // statement_block or arrow-function expression
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
func collectFunctions(root *tsNode, src []byte) []funcCandidate {
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

func fromExportStatement(node *tsNode, src []byte) []funcCandidate {
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

func fromFunctionDeclaration(node *tsNode, src []byte, exported bool) []funcCandidate {
	name := nodeFieldText(node, "name", src)
	return []funcCandidate{{
		name:       name,
		exported:   exported,
		startByte:  node.StartByte(),
		params:     node.ChildByFieldName("parameters"),
		returnType: node.ChildByFieldName("return_type"),
		body:       node.ChildByFieldName("body"),
	}}
}

func fromVariableDeclaration(node *tsNode, src []byte, exported bool) []funcCandidate {
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
			name:     nodeFieldText(declr, "name", src),
			exported: exported,
			// Anchor on the declaration statement (the plugin uses the same
			// node for its symbol's start_byte), so correlation matches exactly.
			startByte:  node.StartByte(),
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

func extractParams(params *tsNode, src []byte) []Param {
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
func paramNodes(params *tsNode) []*tsNode {
	if params == nil {
		return nil
	}
	if params.Type() == "formal_parameters" {
		nodes := make([]*tsNode, 0, params.NamedChildCount())
		for i := 0; i < int(params.NamedChildCount()); i++ {
			nodes = append(nodes, params.NamedChild(i))
		}
		return nodes
	}
	return []*tsNode{params}
}

// arrowParams resolves an arrow function's parameters across grammar shapes:
// the `parameters` field (parenthesized), the `parameter` field, or a bare
// leading identifier for the unparenthesized single-parameter form.
func arrowParams(fn *tsNode) *tsNode {
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
func paramName(p *tsNode, src []byte) string {
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
func paramType(p *tsNode, src []byte) string {
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

func extractReturn(returnType *tsNode, src []byte) Return {
	if returnType == nil {
		return Return{Type: "", Explicit: false}
	}
	return Return{Type: typeAnnotationText(returnType, src), Explicit: true}
}

// extractSideEffects walks the body for call expressions that look like
// observable effects: calls on imported clients/services, or calls whose method
// name contains a side-effect verb.
func extractSideEffects(body *tsNode, src []byte, imports map[string]bool) []string {
	seen := map[string]bool{}
	walk(body, func(n *tsNode) {
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
func extractFailureModes(body *tsNode, src []byte) []string {
	seen := map[string]bool{}
	walk(body, func(n *tsNode) {
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
func extractBehavior(body *tsNode, src []byte) []BehaviorClause {
	out := []BehaviorClause{}
	walkWithinFunction(body, func(n *tsNode) {
		if n.Type() != "if_statement" {
			return
		}
		cond := n.ChildByFieldName("condition")
		out = append(out, BehaviorClause{
			When:     conditionText(cond, src),
			Then:     summarizeConsequence(n.ChildByFieldName("consequence"), src),
			WhenExpr: normalizeCondition(cond, src),
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
func walkWithinFunction(node *tsNode, fn func(*tsNode)) {
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
func conditionText(cond *tsNode, src []byte) string {
	if cond == nil {
		return ""
	}
	if cond.Type() == "parenthesized_expression" && cond.NamedChildCount() > 0 {
		return normalizeWhitespace(cond.NamedChild(0).Content(src))
	}
	return normalizeWhitespace(cond.Content(src))
}

// comparisonOps and logicalBinaryOps are the binary operators the v1 normalized
// grammar recognizes. Any other operator yields no structured expression.
var comparisonOps = map[string]bool{
	"<": true, "<=": true, ">": true, ">=": true,
	"==": true, "!=": true, "===": true, "!==": true,
}

var logicalBinaryOps = map[string]bool{"&&": true, "||": true}

// normalizeCondition produces a normalized Expr for a condition node, or nil
// when the condition falls outside the bounded v1 grammar. The grammar covers
// comparison/equality and logical binary operators, logical negation, static
// member/identifier access paths, and literals — the common shape of an `if`
// condition. Anything else (calls, ternaries, arithmetic, optional chaining,
// computed access) returns nil, the signal to fall back to raw-string behavior.
// Operands are left as opaque dotted paths; this structures expression shape,
// not resolved symbols or types.
func normalizeCondition(node *tsNode, src []byte) *Expr {
	if node == nil {
		return nil
	}
	switch node.Type() {
	case "parenthesized_expression":
		if node.NamedChildCount() == 0 {
			return nil
		}
		return normalizeCondition(node.NamedChild(0), src)
	case "binary_expression":
		op := fieldContent(node, "operator", src)
		if !comparisonOps[op] && !logicalBinaryOps[op] {
			return nil
		}
		left := normalizeCondition(node.ChildByFieldName("left"), src)
		right := normalizeCondition(node.ChildByFieldName("right"), src)
		if left == nil || right == nil {
			return nil
		}
		return &Expr{Op: op, Args: []*Expr{left, right}}
	case "unary_expression":
		op := fieldContent(node, "operator", src)
		// A leading `-` on a numeric literal is negative-number notation, not a
		// runtime arithmetic op (unlike binary `+`/`-`, which stay out of
		// grammar), so fold it into the literal. `-x` on a non-literal is still
		// out of grammar.
		if op == "-" {
			if arg := node.ChildByFieldName("argument"); arg != nil && arg.Type() == "number" {
				return &Expr{Op: "lit", Text: "-" + normalizeWhitespace(arg.Content(src))}
			}
			return nil
		}
		if op != "!" {
			return nil
		}
		arg := normalizeCondition(node.ChildByFieldName("argument"), src)
		if arg == nil {
			return nil
		}
		return &Expr{Op: "!", Args: []*Expr{arg}}
	case "identifier", "member_expression":
		if path := memberPath(node, src); path != "" {
			return &Expr{Op: "path", Text: path}
		}
		return nil
	case "number", "string":
		return &Expr{Op: "lit", Text: normalizeWhitespace(node.Content(src))}
	case "true", "false", "null":
		return &Expr{Op: "lit", Text: node.Type()}
	default:
		return nil
	}
}

// memberPath renders an identifier or a static member-access chain as a dotted
// path (e.g. "campaign.minimumDonation.cents"). It returns "" for anything that
// is not a pure static chain — computed access, calls, etc. — signaling the
// operand is outside the v1 grammar.
func memberPath(node *tsNode, src []byte) string {
	if node == nil {
		return ""
	}
	switch node.Type() {
	case "identifier", "property_identifier", "this":
		return node.Content(src)
	case "member_expression":
		obj := memberPath(node.ChildByFieldName("object"), src)
		prop := node.ChildByFieldName("property")
		if obj == "" || prop == nil || prop.Type() != "property_identifier" {
			return ""
		}
		return obj + "." + prop.Content(src)
	default:
		return ""
	}
}

func fieldContent(node *tsNode, field string, src []byte) string {
	c := node.ChildByFieldName(field)
	if c == nil {
		return ""
	}
	return c.Content(src)
}

// summarizeConsequence describes what a branch does: the first return or throw
// it contains, else its first statement.
func summarizeConsequence(cons *tsNode, src []byte) string {
	if cons == nil {
		return ""
	}
	if cons.Type() != "statement_block" {
		return trimStatement(normalizeWhitespace(cons.Content(src)))
	}
	var first *tsNode
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
func calleeParts(callee *tsNode, src []byte) (method, rootObj, full string) {
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

func leftmostIdentifier(node *tsNode, src []byte) string {
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
func collectImports(root *tsNode, src []byte) map[string]bool {
	imports := map[string]bool{}
	for i := 0; i < int(root.NamedChildCount()); i++ {
		node := root.NamedChild(i)
		if node.Type() != "import_statement" {
			continue
		}
		walkWithParent(node, nil, func(n, parent *tsNode) {
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
				if parent != nil && parent.Type() == "import_clause" {
					imports[n.Content(src)] = true
				}
			}
		})
	}
	return imports
}

// --- small CST helpers -----------------------------------------------------

// walk invokes fn for node and every descendant in a deterministic pre-order.
func walk(node *tsNode, fn func(*tsNode)) {
	if node == nil {
		return
	}
	fn(node)
	for i := 0; i < int(node.NamedChildCount()); i++ {
		walk(node.NamedChild(i), fn)
	}
}

// walkWithParent is walk that also passes each node's parent (nil for the root),
// for the few checks that need parent context.
func walkWithParent(node, parent *tsNode, fn func(n, parent *tsNode)) {
	if node == nil {
		return
	}
	fn(node, parent)
	for i := 0; i < int(node.NamedChildCount()); i++ {
		walkWithParent(node.NamedChild(i), node, fn)
	}
}

func nodeFieldText(node *tsNode, field string, src []byte) string {
	if child := node.ChildByFieldName(field); child != nil {
		return child.Content(src)
	}
	return ""
}

// typeAnnotationText returns the type inside a `type_annotation` node, stripping
// the leading colon.
func typeAnnotationText(ann *tsNode, src []byte) string {
	if ann.Type() == "type_annotation" && ann.NamedChildCount() > 0 {
		return strings.TrimSpace(ann.NamedChild(0).Content(src))
	}
	return strings.TrimSpace(strings.TrimPrefix(ann.Content(src), ":"))
}

func firstStringLiteral(node *tsNode, src []byte) string {
	var found string
	walk(node, func(n *tsNode) {
		if found != "" {
			return
		}
		if n.Type() == "string" {
			found = strings.Trim(n.Content(src), "'\"`")
		}
	})
	return found
}

func lastIdentifier(node *tsNode, src []byte) string {
	var id string
	walk(node, func(n *tsNode) {
		if n.Type() == "identifier" {
			id = n.Content(src)
		}
	})
	return id
}

func firstIdentifier(node *tsNode, src []byte) string {
	var id string
	walk(node, func(n *tsNode) {
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
