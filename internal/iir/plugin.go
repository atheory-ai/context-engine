package iir

import (
	"context"
	"strings"
)

// This file defines the IIR plugin surface. Slice 5 is interface-first: there
// is no dynamic runtime, WASM, or remote loading yet. The point is that the
// built-in TypeScript capabilities implement the very interfaces future plugins
// will, so extending IIR later is a matter of registering more implementations —
// not reworking the core.
//
// The interfaces mirror the TypeScript contracts in the Slice 5 spec, adapted to
// idiomatic Go: methods are synchronous and take a context.Context instead of
// returning Promises.

// ExtractionInput is a unit of source handed to an extractor.
type ExtractionInput struct {
	Language string // e.g. "typescript"
	Path     string // optional source path (used for extension-based support)
	Source   []byte
	Target   string // function name to extract, when applicable
}

// ExtractionResult is what an extractor produces.
type ExtractionResult struct {
	Function *FunctionIntent
}

// ComparisonResult holds the matches and mismatches a comparator produces.
type ComparisonResult struct {
	Matches    []Match
	Mismatches []Mismatch
}

// Extractor turns source into IIR. The built-in TypeScript function extractor
// implements this, and future language/framework plugins will too.
type Extractor interface {
	ID() string
	Supports(input ExtractionInput) bool
	Extract(ctx context.Context, input ExtractionInput) (ExtractionResult, error)
}

// Comparator diffs intended IIR against extracted IIR. Built-in and plugin
// comparators share this interface so comparison strategies can be extended.
type Comparator interface {
	ID() string
	Supports(intended, extracted *FunctionIntent) bool
	Compare(intended, extracted *FunctionIntent) ComparisonResult
}

// PluginRulePack associates a rule pack with the plugin that provides it, so
// rule provenance is preserved when packs are aggregated.
type PluginRulePack struct {
	PluginID string
	Pack     RulePack
}

// Plugin is the manifest of IIR contributions from one source (built-in or, in
// the future, external). Interface-first: it carries implementations directly.
type Plugin struct {
	ID           string
	Name         string
	Version      string
	Languages    []string
	Extractors   []Extractor
	Comparators  []Comparator
	Emitters     []Emitter
	TestEmitters []TestEmitter
	RulePacks    []PluginRulePack
}

// --- Built-in implementations ---------------------------------------------

// builtinPluginID is the id under which IIR's built-in capabilities register.
const builtinPluginID = "builtin"

// tsFunctionExtractor is the built-in TypeScript function extractor exposed
// through the Extractor interface (it wraps ExtractFunction).
type tsFunctionExtractor struct{}

func (tsFunctionExtractor) ID() string { return "builtin.typescript.function" }

func (tsFunctionExtractor) Supports(input ExtractionInput) bool {
	if input.Language == "typescript" {
		return true
	}
	// Fall back to extension when language is unspecified.
	return input.Language == "" &&
		(strings.HasSuffix(input.Path, ".ts") || strings.HasSuffix(input.Path, ".tsx"))
}

func (tsFunctionExtractor) Extract(ctx context.Context, input ExtractionInput) (ExtractionResult, error) {
	fn, err := ExtractFunction(ctx, input.Source, input.Target)
	if err != nil {
		return ExtractionResult{}, err
	}
	return ExtractionResult{Function: fn}, nil
}

// functionComparator is the built-in FunctionIntent comparator exposed through
// the Comparator interface (it wraps Compare).
type functionComparator struct{}

func (functionComparator) ID() string { return "builtin.function" }

func (functionComparator) Supports(intended, extracted *FunctionIntent) bool {
	return intended != nil && extracted != nil &&
		intended.Kind == KindFunctionIntent && extracted.Kind == KindFunctionIntent
}

func (functionComparator) Compare(intended, extracted *FunctionIntent) ComparisonResult {
	matches, mismatches := Compare(intended, extracted)
	return ComparisonResult{Matches: matches, Mismatches: mismatches}
}

// Compile-time proof the built-ins satisfy the plugin interfaces.
var (
	_ Extractor  = tsFunctionExtractor{}
	_ Comparator = functionComparator{}
)

// BuiltinExtractor returns the built-in TypeScript function extractor.
func BuiltinExtractor() Extractor { return tsFunctionExtractor{} }

// BuiltinComparator returns the built-in FunctionIntent comparator.
func BuiltinComparator() Comparator { return functionComparator{} }

// BuiltinPlugin describes IIR's built-in capabilities using the same manifest
// shape external plugins will use.
func BuiltinPlugin() Plugin {
	return Plugin{
		ID:           builtinPluginID,
		Name:         "IIR Built-in",
		Version:      "0.1.0",
		Languages:    []string{"typescript"},
		Extractors:   []Extractor{BuiltinExtractor()},
		Comparators:  []Comparator{BuiltinComparator()},
		Emitters:     []Emitter{BuiltinEmitter()},
		TestEmitters: []TestEmitter{BuiltinTestEmitter()},
		RulePacks:    []PluginRulePack{{PluginID: builtinPluginID, Pack: DefaultRulePack()}},
	}
}

// --- Registry --------------------------------------------------------------

// Registry holds registered plugins and resolves capabilities. Slice 5 keeps
// this in-process only; a later slice can back it with dynamic loading without
// changing these lookups. Later registrations take precedence, so a plugin can
// override a built-in for the same input.
type Registry struct {
	plugins []Plugin
}

// NewRegistry returns a registry seeded with the given plugins.
func NewRegistry(plugins ...Plugin) *Registry {
	return &Registry{plugins: append([]Plugin(nil), plugins...)}
}

// DefaultRegistry returns a registry containing only the built-in plugin.
func DefaultRegistry() *Registry { return NewRegistry(BuiltinPlugin()) }

// Register adds a plugin. Its capabilities take precedence over earlier ones.
func (r *Registry) Register(p Plugin) { r.plugins = append(r.plugins, p) }

// Plugins returns the registered plugins in registration order.
func (r *Registry) Plugins() []Plugin { return r.plugins }

// lastMatch returns the last capability for which ok reports true, scanning
// plugins in reverse registration order and each plugin's slice in reverse, so
// later registrations take precedence.
func lastMatch[T any](plugins []Plugin, pick func(Plugin) []T, ok func(T) bool) (T, bool) {
	for i := len(plugins) - 1; i >= 0; i-- {
		items := pick(plugins[i])
		for j := len(items) - 1; j >= 0; j-- {
			if ok(items[j]) {
				return items[j], true
			}
		}
	}
	var zero T
	return zero, false
}

// ExtractorFor returns the last-registered extractor that supports the input.
func (r *Registry) ExtractorFor(input ExtractionInput) (Extractor, bool) {
	return lastMatch(r.plugins,
		func(p Plugin) []Extractor { return p.Extractors },
		func(e Extractor) bool { return e.Supports(input) })
}

// ComparatorFor returns the last-registered comparator that supports the pair.
func (r *Registry) ComparatorFor(intended, extracted *FunctionIntent) (Comparator, bool) {
	return lastMatch(r.plugins,
		func(p Plugin) []Comparator { return p.Comparators },
		func(c Comparator) bool { return c.Supports(intended, extracted) })
}

// EmitterFor returns the last-registered emitter that supports the intent.
func (r *Registry) EmitterFor(intent *FunctionIntent) (Emitter, bool) {
	return lastMatch(r.plugins,
		func(p Plugin) []Emitter { return p.Emitters },
		func(e Emitter) bool { return e.Supports(intent) })
}

// TestEmitterFor returns the last-registered test emitter that supports the
// intent.
func (r *Registry) TestEmitterFor(intent *FunctionIntent) (TestEmitter, bool) {
	return lastMatch(r.plugins,
		func(p Plugin) []TestEmitter { return p.TestEmitters },
		func(e TestEmitter) bool { return e.Supports(intent) })
}

// RulePacks returns every registered rule pack with its owning plugin id, in
// registration order.
func (r *Registry) RulePacks() []PluginRulePack {
	var out []PluginRulePack
	for _, p := range r.plugins {
		out = append(out, p.RulePacks...)
	}
	return out
}
