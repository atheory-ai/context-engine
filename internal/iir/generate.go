package iir

import (
	"fmt"
	"strings"
)

// This file implements deterministic code generation from IIR. The principle
// (per the Slice 6 spec) is that the emitter generates code from structured
// intent — it does not free-write. A model may help shape the IIR upstream, but
// emission itself is a pure function of the FunctionIntent.
//
// The emitter produces a skeleton whose *structure* round-trips: the same name,
// visibility, inputs, return type, branch count, declared side effects, and
// (under the throw strategy) failure modes come back out when the generated
// source is re-extracted. Branch conditions are placeholders — turning declared
// intent prose into real predicates is where a model assists, not the emitter.

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
	if strings.TrimSpace(intent.Name) == "" {
		return "", fmt.Errorf("generate: FunctionIntent has no name")
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

// writeBehavior emits one guard branch per behavior clause. The condition is a
// placeholder (`false`) with the declared intent in comments; failure modes are
// distributed into branch bodies via the active strategy.
func writeBehavior(b *strings.Builder, intent *FunctionIntent, resultStrategy bool) {
	for i, clause := range intent.Behavior {
		if clause.When != "" {
			fmt.Fprintf(b, "%s// when: %s\n", genIndent, clause.When)
		}
		b.WriteString(genIndent + "if (false) {\n")
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
		fmt.Fprintf(b, "%s%s();\n", genIndent, se)
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

// sideEffectImports returns the root objects of member-expression side effects
// (e.g. "analytics" for "analytics.track"), in first-seen order, so the emitter
// can import them and re-extraction attributes the call to an imported client.
func sideEffectImports(effects []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range effects {
		if i := strings.IndexByte(e, '.'); i > 0 {
			root := e[:i]
			if !seen[root] {
				seen[root] = true
				out = append(out, root)
			}
		}
	}
	return out
}
