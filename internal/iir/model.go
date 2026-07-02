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

// FunctionIntent is the minimum IIR node: a semantic description of a single
// function's contract and behavior. It is produced two ways — declared by a
// human/agent (intended) and extracted from source (actual) — then compared.
type FunctionIntent struct {
	Kind     Kind   `json:"kind" yaml:"kind"`
	Name     string `json:"name" yaml:"name"`
	Language string `json:"language" yaml:"language"`

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
	// non-nil slice means "explicitly declares no side effects".
	SideEffects []string `json:"sideEffects" yaml:"sideEffects"`

	// FailureModes names the expected failure outcomes (e.g. domain error tags).
	FailureModes []string `json:"failureModes" yaml:"failureModes"`

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
type BehaviorClause struct {
	When string `json:"when" yaml:"when"`
	Then string `json:"then" yaml:"then"`
}

// IsPublic reports whether the function participates in the public API.
func (f *FunctionIntent) IsPublic() bool {
	return f.Visibility == VisibilityPublic
}

// HasFailureModes reports whether any failure modes are declared.
func (f *FunctionIntent) HasFailureModes() bool {
	return len(f.FailureModes) > 0
}
