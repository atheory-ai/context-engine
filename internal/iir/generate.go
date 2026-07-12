package iir

import (
	"fmt"
	"regexp"
	"strings"
)

// tsIdentifierRE matches a conservative (ASCII) TypeScript identifier.
var tsIdentifierRE = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`)

// validGeneratableName reports whether name is safe to emit as a TypeScript
// identifier (import binding, call target). Extraction only ever yields valid
// identifiers; this guards hand-written IIR from producing broken source.
func validGeneratableName(name string) bool {
	return tsIdentifierRE.MatchString(name)
}

// This file implements deterministic code generation from IIR. The principle
// (per the Slice 6 spec) is that the emitter generates code from structured
// intent — it does not free-write. A model may help shape the IIR upstream, but
// emission itself is a pure function of the FunctionIntent.
//
// The emitter produces a skeleton whose *structure* round-trips: the same name,
// visibility, inputs, return type, branch count, declared side effects, and
// (under the throw strategy) failure modes come back out when the generated
// source is re-extracted. A branch condition is rendered from its normalized
// WhenExpr when one is present, so the guard round-trips on content too; a
// clause with only prose (no structured expression) still falls back to a
// `false` placeholder — turning prose into a predicate is where a model assists.

const genIndent = "  "

// Emitter turns IIR into source. The built-in TypeScript emitter implements
// this; future language/framework emitters will too.
type Emitter interface {
	ID() string
	Supports(intent *FunctionIntent) bool
	Emit(intent *FunctionIntent) (string, error)
}

type tsFunctionEmitter struct{}

func (tsFunctionEmitter) ID() string { return "builtin.typescript.function" }

func (tsFunctionEmitter) Supports(intent *FunctionIntent) bool {
	return intent != nil && intent.Kind == KindFunctionIntent &&
		(intent.Language == "typescript" || intent.Language == "")
}

func (tsFunctionEmitter) Emit(intent *FunctionIntent) (string, error) {
	return GenerateFunction(intent)
}

// BuiltinEmitter returns the built-in TypeScript function emitter.
func BuiltinEmitter() Emitter { return tsFunctionEmitter{} }

// Compile-time proof the built-in emitter satisfies the interface.
var _ Emitter = tsFunctionEmitter{}

// GenerateFunction emits deterministic TypeScript source for a FunctionIntent.
// The failure strategy follows the return type: a Result-like return uses
// `return failure(...)`, otherwise failures are thrown.
func GenerateFunction(intent *FunctionIntent) (string, error) {
	if intent == nil || intent.Kind != KindFunctionIntent {
		return "", fmt.Errorf("generate: unsupported IIR node")
	}
	if !validGeneratableName(intent.Name) {
		return "", fmt.Errorf("generate: %q is not a valid TypeScript identifier", intent.Name)
	}

	resultStrategy := returnLooksLikeResult(intent.Returns.Type)
	var b strings.Builder

	// Imports for member-expression side effects, so re-extraction detects the
	// call on an imported client rather than dropping it.
	imports := sideEffectImports(intent.SideEffects)
	for _, imp := range imports {
		fmt.Fprintf(&b, "import { %s } from \"./%s\";\n", imp, imp)
	}
	if len(imports) > 0 {
		b.WriteByte('\n')
	}

	writeSignature(&b, intent)
	b.WriteString(" {\n")

	// Defensive input-validation hooks are comments, not branches, so they do
	// not inflate the branch count the comparator checks.
	if len(intent.Inputs) > 0 {
		names := make([]string, len(intent.Inputs))
		for i, p := range intent.Inputs {
			names[i] = p.Name
		}
		fmt.Fprintf(&b, "%s// validate inputs: %s\n\n", genIndent, strings.Join(names, ", "))
	}

	writeBehavior(&b, intent, resultStrategy)
	writeUnmatchedFailures(&b, intent, resultStrategy)
	writeSideEffects(&b, intent)
	writeTrailingReturn(&b, intent, resultStrategy)

	b.WriteString("}\n")
	return b.String(), nil
}

func writeSignature(b *strings.Builder, intent *FunctionIntent) {
	if intent.IsPublic() {
		b.WriteString("export ")
	}
	b.WriteString("function ")
	b.WriteString(intent.Name)
	b.WriteByte('(')
	for i, p := range intent.Inputs {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(p.Name)
		if p.Type != "" && p.Type != TypeUnknown {
			b.WriteString(": ")
			b.WriteString(p.Type)
		}
	}
	b.WriteByte(')')
	if intent.Returns.Explicit && intent.Returns.Type != "" {
		b.WriteString(": ")
		b.WriteString(intent.Returns.Type)
	}
}

// writeBehavior emits one guard branch per behavior clause. When the clause
// carries a normalized WhenExpr, the guard is that condition rendered back to
// TypeScript so it re-extracts to the same expression (closing the
// generate→extract→verify loop on behavior content); otherwise it falls back to
// a `false` placeholder with the declared intent in comments. Failure modes are
// distributed into branch bodies via the active strategy.
func writeBehavior(b *strings.Builder, intent *FunctionIntent, resultStrategy bool) {
	for i, clause := range intent.Behavior {
		if clause.When != "" {
			fmt.Fprintf(b, "%s// when: %s\n", genIndent, clause.When)
		}
		cond := "false"
		if rendered, ok := renderTSCondition(clause.WhenExpr); ok {
			cond = rendered
		}
		fmt.Fprintf(b, "%sif (%s) {\n", genIndent, cond)
		if clause.Then != "" {
			fmt.Fprintf(b, "%s%s// then: %s\n", genIndent, genIndent, clause.Then)
		}
		if i < len(intent.FailureModes) {
			fmt.Fprintf(b, "%s%s%s\n", genIndent, genIndent, failureStatement(intent.FailureModes[i], resultStrategy))
		}
		b.WriteString(genIndent + "}\n\n")
	}
}

// writeUnmatchedFailures emits failure modes that had no branch to live in. Only
// the throw strategy re-extracts (the extractor sees thrown string literals), so
// result-strategy extras are left to upstream logic.
func writeUnmatchedFailures(b *strings.Builder, intent *FunctionIntent, resultStrategy bool) {
	if resultStrategy {
		return
	}
	for i := len(intent.Behavior); i < len(intent.FailureModes); i++ {
		fmt.Fprintf(b, "%sthrow new Error(%q);\n\n", genIndent, intent.FailureModes[i])
	}
}

func writeSideEffects(b *strings.Builder, intent *FunctionIntent) {
	for _, se := range intent.SideEffects {
		fmt.Fprintf(b, "%s%s();\n", genIndent, se.Name)
	}
	if len(intent.SideEffects) > 0 {
		b.WriteByte('\n')
	}
}

func writeTrailingReturn(b *strings.Builder, intent *FunctionIntent, resultStrategy bool) {
	if !intent.Returns.Explicit || intent.Returns.Type == "" || intent.Returns.Type == "void" {
		return
	}
	if resultStrategy {
		fmt.Fprintf(b, "%sreturn success();\n", genIndent)
		return
	}
	fmt.Fprintf(b, "%sreturn undefined as unknown as %s;\n", genIndent, intent.Returns.Type)
}

func failureStatement(mode string, resultStrategy bool) string {
	if resultStrategy {
		return fmt.Sprintf("return failure(%q);", mode)
	}
	return fmt.Sprintf("throw new Error(%q);", mode)
}

// numLitRE matches a numeric literal we can safely re-emit verbatim.
var numLitRE = regexp.MustCompile(`^-?\d+(\.\d+)?$`)

// renderTSCondition renders a normalized Expr back into a TypeScript boolean
// expression that re-extracts to the same Expr. It returns ok=false when the
// expression can't be emitted safely (unknown operator, non-identifier path, or
// an unrecognized literal), so the caller falls back to a placeholder guard
// rather than emit malformed source.
func renderTSCondition(e *Expr) (string, bool) {
	if e == nil {
		return "", false
	}
	switch e.Op {
	case "path":
		if !validCondPath(e.Text) {
			return "", false
		}
		return e.Text, true
	case "lit":
		return renderLiteral(e.Text)
	case "!":
		if len(e.Args) != 1 {
			return "", false
		}
		arg, ok := renderOperand(e.Args[0])
		if !ok {
			return "", false
		}
		return "!" + arg, true
	case "==", "!=", "===", "!==", "<", "<=", ">", ">=", "&&", "||":
		if len(e.Args) != 2 {
			return "", false
		}
		l, okL := renderOperand(e.Args[0])
		r, okR := renderOperand(e.Args[1])
		if !okL || !okR {
			return "", false
		}
		// The operator is emitted verbatim: the extractor preserves TypeScript's
		// native forms (=== / !==), so the rendered guard re-extracts to the same
		// Expr for the ops it produces.
		return l + " " + e.Op + " " + r, true
	default:
		return "", false
	}
}

// renderOperand renders a child expression, parenthesizing compound nodes so the
// emitted precedence and grouping match the tree (the extractor unwraps the
// parens on the way back).
func renderOperand(e *Expr) (string, bool) {
	s, ok := renderTSCondition(e)
	if !ok {
		return "", false
	}
	if e.Op != "path" && e.Op != "lit" {
		return "(" + s + ")", true
	}
	return s, true
}

// renderLiteral emits a normalized literal as TypeScript. The null aliases from
// other languages (Go's nil, Python's None) collapse to null; booleans, numbers,
// and already-quoted string literals pass through; anything else is rejected.
func renderLiteral(text string) (string, bool) {
	switch text {
	case "nil", "None", "null":
		return "null", true
	case "true", "false":
		return text, true
	}
	if numLitRE.MatchString(text) {
		return text, true
	}
	if len(text) >= 2 {
		if q := text[0]; (q == '"' || q == '\'' || q == '`') && text[len(text)-1] == q {
			return text, true
		}
	}
	return "", false
}

// validCondPath reports whether a dotted access path is made entirely of safe
// TypeScript identifiers, so it can be emitted as a condition operand.
func validCondPath(path string) bool {
	if path == "" {
		return false
	}
	for _, seg := range strings.Split(path, ".") {
		if !tsIdentifierRE.MatchString(seg) {
			return false
		}
	}
	return true
}

// sideEffectImports returns the root objects of member-expression side effects
// (e.g. "analytics" for "analytics.track"), in first-seen order, so the emitter
// can import them and re-extraction attributes the call to an imported client.
func sideEffectImports(effects []SideEffect) []string {
	seen := map[string]bool{}
	var out []string
	for _, se := range effects {
		if i := strings.IndexByte(se.Name, '.'); i > 0 {
			root := se.Name[:i]
			if !seen[root] {
				seen[root] = true
				out = append(out, root)
			}
		}
	}
	return out
}
