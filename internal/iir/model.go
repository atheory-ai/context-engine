// Package iir implements the Intermediate Intent Representation: a structured
// description of what code is intended to do, sitting above ASTs and below
// natural language.
//
// Slice 1 proves the deterministic verification loop for a single TypeScript
// function:
//
//	declared intent → source code → extracted intent → comparison → report
//
// No remote models are called and no state is persisted — every stage is
// deterministic so tests and agents can consume stable output.
package iir

// Kind enumerates the IIR node kinds. Only FunctionIntent is supported in
// Slice 1; the type exists so rules and comparisons can target node kinds.
type Kind string

const (
	KindFunctionIntent Kind = "FunctionIntent"
)

// Visibility describes whether a function is part of a module's public API.
type Visibility string

const (
	VisibilityPublic  Visibility = "public"
	VisibilityPrivate Visibility = "private"
)

// TypeUnknown marks a parameter type that could not be determined from source.
// Missing types are represented explicitly rather than dropped so comparison
// can distinguish "unknown" from "absent".
const TypeUnknown = "unknown"

// Origin names the epistemic layer an intent comes from — how much to trust it
// and against what it should be read:
//   - observed: lifted from real source by a plugin. Ground truth about what the
//     code actually does.
//   - declared: a human/agent-authored spec. What the code is supposed to do.
//   - inferred: produced by the shaper from a natural-language description. A
//     model's guess, to be confirmed.
//
// It defaults to declared: a FunctionIntent is a declaration unless a machine
// producer stamps its provenance (the plugin lift → observed, the shaper →
// inferred).
const (
	OriginObserved = "observed"
	OriginDeclared = "declared"
	OriginInferred = "inferred"
)

// FunctionIntent is the minimum IIR node: a semantic description of a single
// function's contract and behavior. It is produced two ways — declared by a
// human/agent (intended) and extracted from source (actual) — then compared.
type FunctionIntent struct {
	Kind     Kind   `json:"kind" yaml:"kind"`
	Name     string `json:"name" yaml:"name"`
	Language string `json:"language" yaml:"language"`

	// Origin is the epistemic layer this intent comes from (see Origin consts).
	// Loaders default an absent origin to "declared".
	Origin string `json:"origin,omitempty" yaml:"origin,omitempty"`

	// Visibility is "public" for exported functions. When intended IIR omits
	// it, loaders default to public because IIR is written to describe API.
	Visibility Visibility `json:"visibility,omitempty" yaml:"visibility,omitempty"`

	Inputs  []Param `json:"inputs" yaml:"inputs"`
	Returns Return  `json:"returns" yaml:"returns"`

	// Behavior is a list of when/then clauses describing expected behavior.
	// Slice 1 treats these as opaque for comparison (count only); richer
	// behavior comparison arrives in a later slice.
	Behavior []BehaviorClause `json:"behavior" yaml:"behavior"`

	// SideEffects names observable effects (e.g. "analytics.track"). An empty
	// non-nil slice means "explicitly declares no side effects". Each effect is a
	// bare name on the wire, or an object with an optional kind/confidence (see
	// SideEffect).
	SideEffects []SideEffect `json:"sideEffects" yaml:"sideEffects"`

	// FailureModes names the expected failure outcomes (e.g. domain error tags).
	// Each is a bare code on the wire, or an object with an optional kind
	// (constructed/sentinel/propagated) and source (see FailureMode).
	FailureModes []FailureMode `json:"failureModes" yaml:"failureModes"`

	// Constraints are free-form durable expectations. They are advisory in
	// Slice 1 and carried through to the report for context.
	Constraints []string `json:"constraints" yaml:"constraints"`
}

// Param is a single function input.
type Param struct {
	Name string `json:"name" yaml:"name"`
	// Type is the annotated type, or TypeUnknown when source omits it.
	Type string `json:"type" yaml:"type"`
}

// Return describes a function's return contract.
type Return struct {
	// Type is the annotated return type. Empty string means the return type
	// was absent in source (distinct from the explicit type "void").
	Type string `json:"type" yaml:"type"`
	// Explicit is true when source declared a return type annotation.
	Explicit bool `json:"explicit" yaml:"-"`
}

// BehaviorClause is a single when/then expectation.
//
// When and Then are always the raw, human-readable condition and consequence
// text (ground truth). WhenExpr and ThenExpr are optional normalized forms of
// those — deterministic AST walks, no model — present only when the condition/
// consequence fits the bounded grammar. Both are additive: an absent normalized
// form means comparison falls back to the raw count-based behavior, losing
// nothing.
type BehaviorClause struct {
	When     string       `json:"when" yaml:"when"`
	Then     string       `json:"then" yaml:"then"`
	WhenExpr *Expr        `json:"whenExpr,omitempty" yaml:"whenExpr,omitempty"`
	ThenExpr *Consequence `json:"thenExpr,omitempty" yaml:"thenExpr,omitempty"`
}

// Consequence action kinds — the normalized "then" of a behavior clause. Throw
// folds Go panic, JS/TS throw, and Python raise into one cross-language notion of
// "raises a failure".
const (
	ConsequenceReturn = "return"
	ConsequenceThrow  = "throw"
	ConsequenceInvoke = "invoke"
)

// Consequence is a normalized behavior consequence: what a clause *does* when its
// condition holds, structured just enough to compare across languages. Op is the
// action (return | throw | invoke). Value is an opaque canonical payload — the
// returned expression, the thrown failure's identity, or the invoked callee — or
// empty when the action carries none (a bare `return`).
type Consequence struct {
	Op    string `json:"op" yaml:"op"`
	Value string `json:"value,omitempty" yaml:"value,omitempty"`
}

// Equal reports whether two consequences describe the same action. The Op must
// match; a Value is compared only when both sides carry one, so a hand-authored
// clause that names the action without its payload still matches source.
func (c *Consequence) Equal(other *Consequence) bool {
	if c == nil || other == nil {
		return c == nil && other == nil
	}
	if c.Op != other.Op {
		return false
	}
	if c.Value != "" && other.Value != "" && c.Value != other.Value {
		return false
	}
	return true
}

// Expr is a normalized expression node: a small, uniform shape that can hold
// comparisons, logical connectives, and leaves without committing to a
// binary-only form. Op names the node ("<", "&&", "!", "path", "lit"); Args are
// operands in source order; Text carries a leaf's payload (a literal value or a
// canonical dotted access path). Operands are deliberately left as opaque path
// strings — this structures expression *shape*, not resolved symbols or types.
type Expr struct {
	Op   string  `json:"op" yaml:"op"`
	Args []*Expr `json:"args,omitempty" yaml:"args,omitempty"`
	Text string  `json:"text,omitempty" yaml:"text,omitempty"`
}

// Equal reports whether two normalized expressions are structurally identical.
// Comparison is order-sensitive (operand order is preserved during
// normalization); commutative canonicalization is intentionally not attempted.
func (e *Expr) Equal(other *Expr) bool {
	if e == nil || other == nil {
		return e == nil && other == nil
	}
	if e.Op != other.Op || e.Text != other.Text || len(e.Args) != len(other.Args) {
		return false
	}
	for i := range e.Args {
		if !e.Args[i].Equal(other.Args[i]) {
			return false
		}
	}
	return true
}

// IsPublic reports whether the function participates in the public API.
func (f *FunctionIntent) IsPublic() bool {
	return f.Visibility == VisibilityPublic
}

// HasFailureModes reports whether any failure modes are declared.
func (f *FunctionIntent) HasFailureModes() bool {
	return len(f.FailureModes) > 0
}
